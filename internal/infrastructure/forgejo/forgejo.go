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

// CreateRepository implements domain.RepositoryWriteService. An empty Owner
// creates under the current authenticated user; a non-empty Owner creates
// under that organization (createCurrentUserRepo / orgCreateRepo).
func (c *Client) CreateRepository(ctx context.Context, in domain.CreateRepositoryInput) (domain.Repository, error) {
	var path string
	if in.Owner != "" {
		path = fmt.Sprintf("/api/v1/orgs/%s/repos", pathEscape(in.Owner))
	} else {
		path = "/api/v1/user/repos"
	}
	body := createRepositoryRequest{Name: in.Name, Private: in.Private, AutoInit: in.AutoInit}
	var out domain.Repository
	err := c.doJSON(ctx, http.MethodPost, path, body, &out)
	return out, err
}

// CreateFile implements domain.FileWriteOrchestrator (repoCreateFile).
func (c *Client) CreateFile(ctx context.Context, in domain.CreateFileInput) (domain.FileResult, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/contents/%s", pathEscape(in.Owner), pathEscape(in.Repo), pathEscape(in.Path))
	body := fileWriteRequest{
		Message: in.Message,
		Branch:  in.Branch,
		SHA:     "",
		Content: base64.StdEncoding.EncodeToString([]byte(in.Content)),
	}
	var wire fileWriteResponse
	if err := c.doJSON(ctx, http.MethodPost, path, body, &wire); err != nil {
		return domain.FileResult{}, err
	}
	return fileResultFromWire(wire), nil
}

// UpdateFile implements domain.FileWriteOrchestrator (repoUpdateFile). The
// request must carry the blob SHA of the current version so Forgejo can detect
// concurrent modifications.
func (c *Client) UpdateFile(ctx context.Context, in domain.UpdateFileInput) (domain.FileResult, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/contents/%s", pathEscape(in.Owner), pathEscape(in.Repo), pathEscape(in.Path))
	body := fileWriteRequest{
		Message: in.Message,
		Branch:  in.Branch,
		SHA:     in.SHA,
		Content: base64.StdEncoding.EncodeToString([]byte(in.Content)),
	}
	var wire fileWriteResponse
	if err := c.doJSON(ctx, http.MethodPut, path, body, &wire); err != nil {
		return domain.FileResult{}, err
	}
	return fileResultFromWire(wire), nil
}

// ChangeFiles implements domain.FileWriteOrchestrator (repoChangeFiles). It
// applies several file operations in a single commit; each file's content is
// base64-encoded on the wire.
func (c *Client) ChangeFiles(ctx context.Context, in domain.ChangeFilesInput) (domain.ChangeFilesResult, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/contents", pathEscape(in.Owner), pathEscape(in.Repo))
	body := changeFilesRequest{Message: in.Message, Branch: in.Branch}
	for _, f := range in.Files {
		body.Files = append(body.Files, changeFileEntryRequest{
			Path:      f.Path,
			Operation: f.Operation,
			Content:   base64.StdEncoding.EncodeToString([]byte(f.Content)),
		})
	}
	var wire changeFilesResponse
	if err := c.doJSON(ctx, http.MethodPost, path, body, &wire); err != nil {
		return domain.ChangeFilesResult{}, err
	}
	res := domain.ChangeFilesResult{CommitSHA: wire.Commit.SHA}
	for _, f := range wire.Files {
		res.Files = append(res.Files, domain.ChangeFileResult{Path: f.Path, SHA: f.SHA, Status: f.Status})
	}
	return res, nil
}

// CreateBranch implements domain.BranchWriteService (repoCreateBranch). A 409
// conflict is surfaced as KindConflict when the branch already exists.
func (c *Client) CreateBranch(ctx context.Context, owner, repo, newBranch, oldRef string) (domain.Branch, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/branches", pathEscape(owner), pathEscape(repo))
	body := struct {
		NewBranch string `json:"new_branch_name"`
		OldRef    string `json:"old_ref_name"`
	}{NewBranch: newBranch, OldRef: oldRef}
	var wire branchWire
	if err := c.doJSON(ctx, http.MethodPost, path, body, &wire); err != nil {
		return domain.Branch{}, err
	}
	return domain.Branch{Name: wire.Name, Protected: wire.Protected, Default: wire.Default, CommitSHA: wire.Commit.ID}, nil
}

// CreateIssue implements domain.IssueWriteService (issueCreateIssue).
func (c *Client) CreateIssue(ctx context.Context, in domain.CreateIssueInput) (domain.Issue, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues", pathEscape(in.Owner), pathEscape(in.Repo))
	body := createIssueRequest{Title: in.Title, Body: in.Body, Labels: in.Labels, Milestone: in.Milestone}
	var out domain.Issue
	err := c.doJSON(ctx, http.MethodPost, path, body, &out)
	return out, err
}

// UpdateIssue implements domain.IssueWriteService (issueEditIssue).
func (c *Client) UpdateIssue(ctx context.Context, in domain.UpdateIssueInput) (domain.Issue, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%s", pathEscape(in.Owner), pathEscape(in.Repo), strconv.FormatInt(in.Index, 10))
	body := updateIssueRequest{Title: in.Title, Body: in.Body, State: in.State}
	var out domain.Issue
	err := c.doJSON(ctx, http.MethodPatch, path, body, &out)
	return out, err
}

// CreateIssueComment implements domain.IssueWriteService (issueCreateComment).
func (c *Client) CreateIssueComment(ctx context.Context, owner, repo string, index int64, body string) (domain.Comment, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%s/comments", pathEscape(owner), pathEscape(repo), strconv.FormatInt(index, 10))
	req := struct {
		Body string `json:"body"`
	}{Body: body}
	var out domain.Comment
	err := c.doJSON(ctx, http.MethodPost, path, req, &out)
	return out, err
}

// CreatePullRequest implements domain.PullRequestWriteService
// (repoCreatePullRequest).
func (c *Client) CreatePullRequest(ctx context.Context, in domain.CreatePullRequestInput) (domain.PullRequest, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls", pathEscape(in.Owner), pathEscape(in.Repo))
	body := createPullRequestRequest{Title: in.Title, Body: in.Body, Head: in.Head, Base: in.Base}
	var out domain.PullRequest
	err := c.doJSON(ctx, http.MethodPost, path, body, &out)
	return out, err
}

// UpdatePullRequest implements domain.PullRequestWriteService
// (repoEditPullRequest).
func (c *Client) UpdatePullRequest(ctx context.Context, in domain.UpdatePullRequestInput) (domain.PullRequest, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%s", pathEscape(in.Owner), pathEscape(in.Repo), strconv.FormatInt(in.Index, 10))
	body := updateIssueRequest{Title: in.Title, Body: in.Body, State: in.State}
	var out domain.PullRequest
	err := c.doJSON(ctx, http.MethodPatch, path, body, &out)
	return out, err
}

// IsPullRequestMerged implements domain.PullRequestWriteService
// (repoPullRequestIsMerged). Forgejo returns 204 when merged and 404 when not;
// any other status maps onto the error taxonomy.
func (c *Client) IsPullRequestMerged(ctx context.Context, owner, repo string, index int64) (bool, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%s/merge", pathEscape(owner), pathEscape(repo), strconv.FormatInt(index, 10))
	req := c.resty.R().SetContext(ctx).SetResponseBodyUnlimitedReads(true)
	resp, err := req.Execute(http.MethodGet, path)
	if err != nil {
		return false, domain.NewForgejoError(domain.KindTransient, domain.Redact(fmt.Sprintf("request failed: %v", err), c.token))
	}
	switch resp.StatusCode() {
	case http.StatusNoContent:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		if resp.StatusCode() >= 200 && resp.StatusCode() < 300 {
			return true, nil
		}
		return false, mapStatusError(resp.StatusCode(), string(resp.Bytes()), c.token)
	}
}

// MergePullRequest implements domain.PullRequestWriteService
// (repoMergePullRequest). The merge method is passed through verbatim.
func (c *Client) MergePullRequest(ctx context.Context, owner, repo string, index int64, method string) error {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%s/merge", pathEscape(owner), pathEscape(repo), strconv.FormatInt(index, 10))
	body := struct {
		Do string `json:"Do"`
	}{Do: method}
	return c.doJSON(ctx, http.MethodPost, path, body, nil)
}

// CreatePullReview implements domain.PullRequestWriteService
// (repoCreatePullReview). It creates a pending review; the caller later submits
// it with the returned review ID.
func (c *Client) CreatePullReview(ctx context.Context, owner, repo string, index int64, body string) (domain.Review, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%s/reviews", pathEscape(owner), pathEscape(repo), strconv.FormatInt(index, 10))
	req := struct {
		Event string `json:"event"`
		Body  string `json:"body"`
	}{Event: "PENDING", Body: body}
	var out domain.Review
	err := c.doJSON(ctx, http.MethodPost, path, req, &out)
	return out, err
}

// SubmitPullReview implements domain.PullRequestWriteService
// (repoSubmitPullReview).
func (c *Client) SubmitPullReview(ctx context.Context, owner, repo string, index, reviewID int64, event string) (domain.Review, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%s/reviews/%s", pathEscape(owner), pathEscape(repo), strconv.FormatInt(index, 10), strconv.FormatInt(reviewID, 10))
	req := struct {
		Event string `json:"event"`
	}{Event: event}
	var out domain.Review
	err := c.doJSON(ctx, http.MethodPost, path, req, &out)
	return out, err
}

// CreateRelease implements domain.ReleaseWriteService (repoCreateRelease).
func (c *Client) CreateRelease(ctx context.Context, in domain.CreateReleaseInput) (domain.Release, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/releases", pathEscape(in.Owner), pathEscape(in.Repo))
	body := createReleaseRequest{TagName: in.Tag, Name: in.Name, Body: in.Notes}
	var out domain.Release
	err := c.doJSON(ctx, http.MethodPost, path, body, &out)
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

// createRepositoryRequest is the body of repoCreateFile / orgCreateRepo. The
// owner goes in the URL path, never the body.
type createRepositoryRequest struct {
	Name     string `json:"name"`
	Private  bool   `json:"private"`
	AutoInit bool   `json:"auto_init"`
}

// fileWriteRequest is the shared body of repoCreateFile and repoUpdateFile.
// Content is base64-encoded by the caller; SHA is set only for updates.
type fileWriteRequest struct {
	Message string `json:"message"`
	Branch  string `json:"branch,omitempty"`
	SHA     string `json:"sha,omitempty"`
	Content string `json:"content"`
}

// fileWriteResponse is the wire shape of a repoCreateFile / repoUpdateFile
// response: the resulting contents entry plus the commit it created.
type fileWriteResponse struct {
	Content contentResponse `json:"content"`
	Commit  struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// fileResultFromWire maps a fileWriteResponse into a domain.FileResult,
// decoding the base64 content back to text.
func fileResultFromWire(w fileWriteResponse) domain.FileResult {
	res := domain.FileResult{Path: w.Content.Path, SHA: w.Content.SHA, CommitSHA: w.Commit.SHA}
	if w.Content.Encoding == "base64" {
		if data, err := base64.StdEncoding.DecodeString(w.Content.Content); err == nil {
			res.Content = string(data)
		}
	} else {
		res.Content = w.Content.Content
	}
	return res
}

// changeFilesRequest is the body of repoChangeFiles.
type changeFilesRequest struct {
	Message string                   `json:"message"`
	Branch  string                   `json:"branch,omitempty"`
	Files   []changeFileEntryRequest `json:"files"`
}

// changeFileEntryRequest is a single file operation within repoChangeFiles.
type changeFileEntryRequest struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
	Content   string `json:"content"`
}

// changeFilesResponse is the wire shape of a repoChangeFiles response.
type changeFilesResponse struct {
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
	Files []struct {
		Path   string `json:"path"`
		SHA    string `json:"sha"`
		Status string `json:"status"`
	} `json:"files"`
}

// createIssueRequest is the body of issueCreateIssue.
type createIssueRequest struct {
	Title     string  `json:"title"`
	Body      string  `json:"body,omitempty"`
	Labels    []int64 `json:"labels,omitempty"`
	Milestone int64   `json:"milestone,omitempty"`
}

// updateIssueRequest is the body of issueEditIssue and repoEditPullRequest.
type updateIssueRequest struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
	State string `json:"state,omitempty"`
}

// createPullRequestRequest is the body of repoCreatePullRequest.
type createPullRequestRequest struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Head  string `json:"head"`
	Base  string `json:"base"`
}

// createReleaseRequest is the body of repoCreateRelease.
type createReleaseRequest struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name,omitempty"`
	Body    string `json:"body,omitempty"`
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

// doJSON performs a request with an optional JSON body and decodes the JSON
// response into out (if out is non-nil). Failures map onto the domain error
// taxonomy exactly like do, and the PAT is always redacted from any surfaced
// message (S2). The body is marshalled by resty, so a nil body sends no
// payload. Content-bearing fields (e.g. file content) are base64-encoded by
// the caller before being placed in body, per the Forgejo wire contract.
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	req := c.resty.R().
		SetContext(ctx).
		SetResponseBodyUnlimitedReads(true)
	if body != nil {
		req = req.SetBody(body)
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
	if err := json.Unmarshal(resp.Bytes(), out); err != nil {
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
