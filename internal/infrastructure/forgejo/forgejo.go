// Package forgejo implements the Forgejo REST client. It depends only on the
// domain interfaces and implements them by issuing HTTP requests to the remote
// Forgejo instance, attaching the PAT as an Authorization header and mapping
// HTTP status codes onto the domain error taxonomy.
package forgejo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"example.com/teran/mcp-forgejo/internal/domain"
)

// Config carries the values the client needs to talk to Forgejo. It is a
// small, transport-agnostic struct local to this package so the client does
// not depend on internal/config.
type Config struct {
	BaseURL string
	Token   string
}

// Client is a minimal Forgejo REST client implementing the domain services.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// New constructs a Client. If httpClient is nil, http.DefaultClient is used.
func New(cfg Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		token:      cfg.Token,
		httpClient: httpClient,
	}
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

// do performs a request, sets the auth header, and decodes the JSON response
// into out (if out is non-nil). Non-2xx responses are mapped onto the domain
// error taxonomy; the token is redacted from any surfaced message.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, http.NoBody)
	if err != nil {
		return domain.NewForgejoError(domain.KindTransient, domain.Redact(fmt.Sprintf("build request: %v", err), c.token))
	}
	req.Header.Set("Authorization", "token "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.NewForgejoError(domain.KindTransient, domain.Redact(fmt.Sprintf("request failed: %v", err), c.token))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return mapStatusError(resp.StatusCode, string(body), c.token)
	}

	if out == nil {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.NewForgejoError(domain.KindTransient, "failed to read response body")
	}
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
