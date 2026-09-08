// Package domain holds the pure entities, error taxonomy and service
// interfaces of mcp-forgejo. It has no internal dependencies: it is the
// stable core that the application and infrastructure layers build upon.
package domain

import (
	"context"
	"strings"
)

// ErrorKind classifies Forgejo API failures so that layers above can map them
// to MCP error codes and user-friendly messages without leaking credentials.
type ErrorKind string

const (
	// KindUnknown is the default for unexpected failures.
	KindUnknown ErrorKind = "unknown"
	// KindNotFound maps to Forgejo 404.
	KindNotFound ErrorKind = "not_found"
	// KindUnauthorized maps to Forgejo 401.
	KindUnauthorized ErrorKind = "unauthorized"
	// KindForbidden maps to Forgejo 403.
	KindForbidden ErrorKind = "forbidden"
	// KindValidation maps to Forgejo 422.
	KindValidation ErrorKind = "validation"
	// KindConflict maps to Forgejo 409.
	KindConflict ErrorKind = "conflict"
	// KindArchived maps to Forgejo 423.
	KindArchived ErrorKind = "archived"
	// KindTransient maps to network/5xx failures, safe to retry.
	KindTransient ErrorKind = "transient"
)

// ForgejoError is the typed error produced by the infrastructure layer. It
// carries a classification for MCP error-code mapping and a redacted message
// that never contains credentials.
type ForgejoError struct {
	Kind    ErrorKind
	Message string
}

// Error implements the error interface.
func (e *ForgejoError) Error() string { return e.Message }

// NewForgejoError builds a typed Forgejo error with a plain message. Callers
// format the message themselves (via fmt.Sprintf) so the message never embeds
// credentials without going through domain.Redact.
func NewForgejoError(kind ErrorKind, message string) error {
	return &ForgejoError{Kind: kind, Message: message}
}

// Redact returns s with every occurrence of secret replaced by "[REDACTED]".
// It is a defensive data-hygiene helper (S2): even though the PAT is only ever
// sent in an Authorization header, any string that could embed credentials is
// scrubbed before it is surfaced in an error or log.
func Redact(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "[REDACTED]")
}

// Owner is a repository owner (user or organization).
type Owner struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	FullName  string `json:"full_name"`
	AvatarURL string `json:"avatar_url"`
}

// Repository is a Forgejo repository.
type Repository struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Owner         Owner  `json:"owner"`
	Private       bool   `json:"private"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
	SSHURL        string `json:"ssh_url"`
	Archived      bool   `json:"archived"`
	Empty         bool   `json:"empty"`
	Size          int64  `json:"size"`
	Language      string `json:"language"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// FileEntry is a single entry (file or directory) in a repository listing.
type FileEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	Size        int64  `json:"size"`
	SHA         string `json:"sha"`
	DownloadURL string `json:"download_url"`
}

// File is the content of a single repository file. If the underlying blob is
// not valid UTF-8 (e.g. an image or archive) then Binary is true and Content is
// empty — corrupted text is never returned.
type File struct {
	Content  string `json:"content"`
	Binary   bool   `json:"binary"`
	SHA      string `json:"sha"`
	Encoding string `json:"encoding"`
	Size     int64  `json:"size"`
}

// Organization is a Forgejo organization the current user belongs to.
type Organization struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Website     string `json:"website"`
	Location    string `json:"location"`
	AvatarURL   string `json:"avatar_url"`
}

// User is a Forgejo user attached to an issue or comment.
type User struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	FullName  string `json:"full_name"`
	AvatarURL string `json:"avatar_url"`
}

// Label is a Forgejo issue label.
type Label struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Issue is a Forgejo issue.
type Issue struct {
	ID        int64   `json:"id"`
	Number    int64   `json:"number"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	State     string  `json:"state"`
	User      User    `json:"user"`
	Labels    []Label `json:"labels"`
	Comments  int64   `json:"comments"`
	HTMLURL   string  `json:"html_url"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// Comment is a comment on a Forgejo issue or pull request.
type Comment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	User      User   `json:"user"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// IssueWithComments bundles an issue together with its comments, so a single
// tool call returns the complete result (SPEC 6.1 #10).
type IssueWithComments struct {
	Issue    Issue     `json:"issue"`
	Comments []Comment `json:"comments"`
}

// RepositoryService is implemented by the Forgejo REST client.
type RepositoryService interface {
	// GetRepository returns details of a single repository.
	GetRepository(ctx context.Context, owner, repo string) (Repository, error)
	// ListContents lists the entries of a directory at path+ref.
	ListContents(ctx context.Context, owner, repo, path, ref string, page, limit int) ([]FileEntry, error)
	// GetFile returns the (possibly binary) content of a single file at path+ref.
	GetFile(ctx context.Context, owner, repo, path, ref string) (File, error)
}

// OrganizationService is implemented by the Forgejo REST client.
type OrganizationService interface {
	// ListOrganizations returns the organizations the current user belongs to.
	ListOrganizations(ctx context.Context) ([]Organization, error)
}

// IssueService is implemented by the Forgejo REST client.
type IssueService interface {
	// GetIssue returns a single issue by its index number.
	GetIssue(ctx context.Context, owner, repo string, index int64) (Issue, error)
	// ListComments returns the comments of a single issue.
	ListComments(ctx context.Context, owner, repo string, index int64) ([]Comment, error)
}
