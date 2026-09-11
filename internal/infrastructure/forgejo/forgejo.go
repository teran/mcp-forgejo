// Package forgejo implements the Forgejo REST client. It depends only on the
// domain interfaces and implements them by issuing HTTP requests to the remote
// Forgejo instance, attaching the PAT as an Authorization header and mapping
// HTTP status codes onto the domain error taxonomy.
//
// Outbound HTTP is performed through resty.dev/v3 (SPEC G9). The transport is
// intentionally kept thin: no retry loop runs inside the client so transient
// failures surface to the caller immediately and the MCP layer decides how to
// report them. Timeout and header policy are set explicitly here rather than
// inherited from package defaults.
package forgejo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/teran/mcp-forgejo/internal/domain"
	"resty.dev/v3"
)

// requestTimeout is the explicit per-request timeout applied to every call.
// Forgejo read endpoints are expected to answer well within this bound.
const requestTimeout = 30 * time.Second

// Config carries the values the client needs to talk to Forgejo. It is a
// small, transport-agnostic struct local to this package so the client does
// not depend on internal/config.
type Config struct {
	BaseURL string
	Token   string
}

// Client is a minimal Forgejo REST client implementing the domain services.
type Client struct {
	resty *resty.Client
	token string
}

// New constructs a Client using resty's default HTTP transport, pointed at
// cfg.BaseURL with the PAT attached as an Authorization header.
func New(cfg Config) *Client {
	return newClient(cfg, nil)
}

// NewWithClient constructs a Client around an explicitly supplied *http.Client.
// Tests inject the httptest server's client (or a transport that returns
// synthetic errors) through this seam.
func NewWithClient(cfg Config, hc *http.Client) *Client {
	return newClient(cfg, hc)
}

// newClient is the shared constructor. When hc is nil resty's default
// transport is used; otherwise the supplied client (and its transport) is used
// verbatim so tests stay hermetic and offline.
func newClient(cfg Config, hc *http.Client) *Client {
	var rc *resty.Client
	if hc == nil {
		rc = resty.New()
	} else {
		rc = resty.NewWithClient(hc)
	}

	// Explicit transport policy (SPEC G9 / N23): a bounded per-request timeout
	// and no in-client retry. Retries are the caller's decision; here a slow or
	// failed upstream surfaces as a KindTransient error.
	rc.SetBaseURL(strings.TrimRight(cfg.BaseURL, "/"))
	rc.SetHeader("Authorization", "token "+cfg.Token)
	rc.SetTimeout(requestTimeout)
	rc.SetRetryCount(0)

	return &Client{resty: rc, token: cfg.Token}
}

// GetRepository implements domain.RepositoryService.
func (c *Client) GetRepository(ctx context.Context, owner, repo string) (domain.Repository, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s", pathEscape(owner), pathEscape(repo))
	var out domain.Repository
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// ListContents implements domain.RepositoryService.
func (c *Client) ListContents(ctx context.Context, owner, repo, path, ref string, page, limit int) ([]domain.FileEntry, error) {
	p := fmt.Sprintf("/api/v1/repos/%s/%s/contents", pathEscape(owner), pathEscape(repo))
	if path != "" {
		p += "/" + pathEscape(path)
	}
	q := url.Values{}
	if ref != "" {
		q.Set("ref", ref)
	}
	setPagination(q, page, limit)
	var out []domain.FileEntry
	err := c.do(ctx, http.MethodGet, p, q, &out)
	return out, err
}

// GetFile implements domain.RepositoryService.
func (c *Client) GetFile(ctx context.Context, owner, repo, path, ref string) (domain.File, error) {
	p := fmt.Sprintf("/api/v1/repos/%s/%s/contents/%s", pathEscape(owner), pathEscape(repo), pathEscape(path))
	q := url.Values{}
	if ref != "" {
		q.Set("ref", ref)
	}
	var raw contentResponse
	err := c.do(ctx, http.MethodGet, p, q, &raw)
	if err != nil {
		return domain.File{}, err
	}
	return decodeFile(raw), nil
}

// ListOrganizations implements domain.OrganizationService.
func (c *Client) ListOrganizations(ctx context.Context) ([]domain.Organization, error) {
	var out []domain.Organization
	err := c.do(ctx, http.MethodGet, "/api/v1/user/orgs", nil, &out)
	return out, err
}

// GetIssue implements domain.IssueService.
func (c *Client) GetIssue(ctx context.Context, owner, repo string, index int64) (domain.Issue, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%s", pathEscape(owner), pathEscape(repo), strconv.FormatInt(index, 10))
	var out domain.Issue
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// ListComments implements domain.IssueService.
func (c *Client) ListComments(ctx context.Context, owner, repo string, index int64) ([]domain.Comment, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%s/comments", pathEscape(owner), pathEscape(repo), strconv.FormatInt(index, 10))
	var out []domain.Comment
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// SearchRepos implements domain.SearchService. The Forgejo search endpoint
// wraps the result list in a {"ok":true,"data":[...]} envelope, so the client
// unwraps ".data" before returning.
func (c *Client) SearchRepos(ctx context.Context, q, topic, sort, order string, private *bool) ([]domain.Repository, error) {
	qp := url.Values{}
	if q != "" {
		qp.Set("q", q)
	}
	if topic != "" {
		qp.Set("topic", topic)
	}
	if sort != "" {
		qp.Set("sort", sort)
	}
	if order != "" {
		qp.Set("order", order)
	}
	if private != nil {
		qp.Set("private", strconv.FormatBool(*private))
	}
	var wrapped repoSearchResponse
	if err := c.do(ctx, http.MethodGet, "/api/v1/repos/search", qp, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Data, nil
}

// GetDiff implements domain.DiffService. The compare endpoint returns a plain
// text unified diff, so the body is read through the text path, not JSON.
func (c *Client) GetDiff(ctx context.Context, owner, repo, basehead string) (domain.Diff, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/compare/%s", pathEscape(owner), pathEscape(repo), pathEscape(basehead))
	text, err := c.doText(ctx, http.MethodGet, path, nil)
	if err != nil {
		return domain.Diff{}, err
	}
	return domain.Diff{BaseHead: basehead, Text: text}, nil
}

// GetPullDiff implements domain.DiffService. The pull request diff endpoint
// carries a ".diff" suffix and returns plain text.
func (c *Client) GetPullDiff(ctx context.Context, owner, repo string, index int64) (domain.Diff, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%s.diff", pathEscape(owner), pathEscape(repo), strconv.FormatInt(index, 10))
	text, err := c.doText(ctx, http.MethodGet, path, nil)
	if err != nil {
		return domain.Diff{}, err
	}
	return domain.Diff{BaseHead: "", Text: text}, nil
}

// ListCommits implements domain.CommitService. Forgejo nests the message and
// author inside a "commit" object, so the wire shape is decoded first and then
// flattened into domain.Commit.
func (c *Client) ListCommits(ctx context.Context, owner, repo, branch string, page, limit int) ([]domain.Commit, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/commits", pathEscape(owner), pathEscape(repo))
	qp := url.Values{}
	if branch != "" {
		qp.Set("sha", branch)
	}
	setPagination(qp, page, limit)
	var wire []commitWire
	if err := c.do(ctx, http.MethodGet, path, qp, &wire); err != nil {
		return nil, err
	}
	out := make([]domain.Commit, 0, len(wire))
	for _, w := range wire {
		out = append(out, domain.Commit{
			SHA:     w.SHA,
			Message: w.Commit.Message,
			Author:  w.Commit.Author.Name,
			Date:    w.Commit.Author.Date,
		})
	}
	return out, nil
}

// ListBranches implements domain.BranchService. The commit the branch points
// to is nested under "commit.id", flattened into CommitSHA.
func (c *Client) ListBranches(ctx context.Context, owner, repo string) ([]domain.Branch, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/branches", pathEscape(owner), pathEscape(repo))
	var wire []branchWire
	if err := c.do(ctx, http.MethodGet, path, nil, &wire); err != nil {
		return nil, err
	}
	out := make([]domain.Branch, 0, len(wire))
	for _, w := range wire {
		out = append(out, domain.Branch{
			Name:      w.Name,
			Protected: w.Protected,
			Default:   w.Default,
			CommitSHA: w.Commit.ID,
		})
	}
	return out, nil
}

// ListIssues implements domain.IssueListService.
func (c *Client) ListIssues(ctx context.Context, owner, repo, state string, page, limit int) ([]domain.Issue, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues", pathEscape(owner), pathEscape(repo))
	qp := url.Values{}
	if state != "" {
		qp.Set("state", state)
	}
	setPagination(qp, page, limit)
	var out []domain.Issue
	err := c.do(ctx, http.MethodGet, path, qp, &out)
	return out, err
}

// ListPullRequests implements domain.PullRequestService.
func (c *Client) ListPullRequests(ctx context.Context, owner, repo, state string, page, limit int) ([]domain.PullRequest, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls", pathEscape(owner), pathEscape(repo))
	qp := url.Values{}
	if state != "" {
		qp.Set("state", state)
	}
	setPagination(qp, page, limit)
	var out []domain.PullRequest
	err := c.do(ctx, http.MethodGet, path, qp, &out)
	return out, err
}

// GetPullRequest implements domain.PullRequestService. A single call composes
// three HTTP requests in order (SPEC 6.1 #12 / M5): the pull request, its
// changed files, and the combined commit status keyed by head.sha.
func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, index int64) (domain.PullRequestDetail, error) {
	prPath := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%s", pathEscape(owner), pathEscape(repo), strconv.FormatInt(index, 10))
	var prWire pullRequestWire
	if err := c.do(ctx, http.MethodGet, prPath, nil, &prWire); err != nil {
		return domain.PullRequestDetail{}, err
	}

	filesPath := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%s/files", pathEscape(owner), pathEscape(repo), strconv.FormatInt(index, 10))
	var files []domain.PullFile
	if err := c.do(ctx, http.MethodGet, filesPath, nil, &files); err != nil {
		return domain.PullRequestDetail{}, err
	}

	statusPath := fmt.Sprintf("/api/v1/repos/%s/%s/commits/%s/status", pathEscape(owner), pathEscape(repo), pathEscape(prWire.Head.SHA))
	var status combinedStatusWire
	if err := c.do(ctx, http.MethodGet, statusPath, nil, &status); err != nil {
		return domain.PullRequestDetail{}, err
	}

	checks := make([]domain.Check, 0, len(status.Statuses))
	for _, s := range status.Statuses {
		checks = append(checks, domain.Check{
			Context:     s.Context,
			State:       s.State,
			TargetURL:   s.TargetURL,
			Description: s.Description,
		})
	}

	return domain.PullRequestDetail{PullRequest: prWire.PullRequest, Files: files, Checks: checks}, nil
}

// ListReleases implements domain.ReleaseService.
func (c *Client) ListReleases(ctx context.Context, owner, repo string, page, limit int) ([]domain.Release, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/releases", pathEscape(owner), pathEscape(repo))
	qp := url.Values{}
	setPagination(qp, page, limit)
	var out []domain.Release
	err := c.do(ctx, http.MethodGet, path, qp, &out)
	return out, err
}

// GetLatestRelease implements domain.ReleaseService.
func (c *Client) GetLatestRelease(ctx context.Context, owner, repo string) (domain.Release, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/releases/latest", pathEscape(owner), pathEscape(repo))
	var out domain.Release
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// repoSearchResponse is the wire envelope of the Forgejo repo search endpoint.
type repoSearchResponse struct {
	OK   bool                `json:"ok"`
	Data []domain.Repository `json:"data"`
}

// commitWire is the nested wire shape of a Forgejo commit list item.
type commitWire struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name string `json:"name"`
			Date string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

// branchWire is the nested wire shape of a Forgejo branch list item.
type branchWire struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
	Default   bool   `json:"default"`
	Commit    struct {
		ID string `json:"id"`
	} `json:"commit"`
}

// pullRequestWire embeds the domain PullRequest (fields map directly) and
// additionally captures head.sha, which the combined-status call needs.
type pullRequestWire struct {
	domain.PullRequest
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

// combinedStatusWire is the wire shape of the Forgejo combined status endpoint.
type combinedStatusWire struct {
	State    string `json:"state"`
	Statuses []struct {
		Context     string `json:"context"`
		State       string `json:"state"`
		TargetURL   string `json:"target_url"`
		Description string `json:"description"`
	} `json:"statuses"`
}

// contentResponse is the wire shape of a single contents entry returned by the
// Forgejo repoGetContents endpoint.
type contentResponse struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	SHA      string `json:"sha"`
	Size     int64  `json:"size"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// decodeFile converts a raw contents response into a domain.File, base64
// decoding the content and detecting non-UTF-8 (binary) blobs. JSON string
// decoding already normalizes invalid UTF-8 for the non-base64 path, so the
// binary check only applies to base64-decoded bytes.
func decodeFile(raw contentResponse) domain.File {
	if raw.Encoding == "base64" {
		data, err := base64.StdEncoding.DecodeString(raw.Content)
		if err != nil {
			// A malformed base64 blob is treated as binary rather than
			// returning corrupt text.
			return domain.File{Binary: true, SHA: raw.SHA, Encoding: raw.Encoding, Size: raw.Size}
		}
		if !utf8.Valid(data) {
			return domain.File{Binary: true, SHA: raw.SHA, Encoding: raw.Encoding, Size: raw.Size}
		}
		return domain.File{Content: string(data), SHA: raw.SHA, Encoding: raw.Encoding, Size: raw.Size}
	}
	return domain.File{Content: raw.Content, SHA: raw.SHA, Encoding: raw.Encoding, Size: raw.Size}
}

// setPagination adds page and limit query params when they are non-zero.
func setPagination(q url.Values, page, limit int) {
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
}

// pathEscape percent-encodes a single path segment.
func pathEscape(s string) string {
	return url.PathEscape(s)
}

// do performs a request through resty, decodes the JSON response into out (if
// out is non-nil) and maps failures onto the domain error taxonomy. The
// response body is read eagerly so that a body-read failure is reported as a
// KindTransient error rather than being silently swallowed. The PAT is always
// redacted from any surfaced message (S2).
func (c *Client) do(ctx context.Context, method, path string, query url.Values, out any) error {
	req := c.resty.R().
		SetContext(ctx).
		SetResponseBodyUnlimitedReads(true)
	if len(query) > 0 {
		req = req.SetQueryParamsFromValues(query)
	}

	resp, err := req.Execute(method, path)
	if err != nil {
		return domain.NewForgejoError(domain.KindTransient, domain.Redact(fmt.Sprintf("request failed: %v", err), c.token))
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return mapStatusError(resp.StatusCode(), string(resp.Bytes()), c.token)
	}

	if out == nil {
		return nil
	}
	body := resp.Bytes()
	if err := json.Unmarshal(body, out); err != nil {
		return domain.NewForgejoError(domain.KindValidation, domain.Redact(fmt.Sprintf("failed to decode Forgejo response: %v", err), c.token))
	}
	return nil
}

// doText performs a request and returns the raw response body as a string
// (for endpoints that return plain text, e.g. diffs) instead of decoding JSON.
// Failures map onto the domain error taxonomy exactly like do.
func (c *Client) doText(ctx context.Context, method, path string, query url.Values) (string, error) {
	req := c.resty.R().
		SetContext(ctx).
		SetResponseBodyUnlimitedReads(true)
	if len(query) > 0 {
		req = req.SetQueryParamsFromValues(query)
	}

	resp, err := req.Execute(method, path)
	if err != nil {
		return "", domain.NewForgejoError(domain.KindTransient, domain.Redact(fmt.Sprintf("request failed: %v", err), c.token))
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return "", mapStatusError(resp.StatusCode(), string(resp.Bytes()), c.token)
	}
	return string(resp.Bytes()), nil
}

// kindForStatus maps a Forgejo HTTP status onto the domain error taxonomy.
func kindForStatus(status int) domain.ErrorKind {
	switch status {
	case http.StatusUnauthorized:
		return domain.KindUnauthorized
	case http.StatusForbidden:
		return domain.KindForbidden
	case http.StatusNotFound:
		return domain.KindNotFound
	case http.StatusUnprocessableEntity:
		return domain.KindValidation
	case http.StatusConflict:
		return domain.KindConflict
	case http.StatusLocked:
		return domain.KindArchived
	default:
		if status >= 500 {
			return domain.KindTransient
		}
		return domain.KindUnknown
	}
}

// mapStatusError builds a typed domain error for a non-2xx HTTP status,
// preferring a human-readable fallback when the response body is empty.
func mapStatusError(status int, body, token string) error {
	kind := kindForStatus(status)
	msg := domain.Redact(strings.TrimSpace(body), token)
	if msg == "" {
		msg = fallbackMessage(kind, status)
	}
	return domain.NewForgejoError(kind, msg)
}

// fallbackMessage returns a safe default message when Forgejo returned no body.
func fallbackMessage(kind domain.ErrorKind, status int) string {
	switch kind {
	case domain.KindNotFound:
		return "Forgejo resource not found"
	case domain.KindUnauthorized:
		return "Forgejo authorization failed"
	case domain.KindForbidden:
		return "Forgejo access forbidden"
	case domain.KindConflict:
		return "Forgejo conflict"
	case domain.KindArchived:
		return "Forgejo repository is archived"
	default:
		return fmt.Sprintf("Forgejo request failed with status %d", status)
	}
}
