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
