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

// Items wraps a slice of results so that a list tool's structuredContent is a
// JSON *object* ({ "items": [...] }) rather than a bare array. MCP clients
// require structuredContent to be a record, so list tools must not return a
// top-level array (regression for "expected record, received array").
type Items[T any] struct {
	Items []T `json:"items"`
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

// Tag is a git tag of a repository.
type Tag struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	SHA        string `json:"sha"`
	Message    string `json:"message"`
	TarballURL string `json:"tarball_url"`
	ZipballURL string `json:"zipball_url"`
}

// CreateTagInput carries the fields needed to create a git tag.
type CreateTagInput struct {
	Name    string `json:"tag_name"`
	Target  string `json:"target,omitempty"`
	Message string `json:"message,omitempty"`
}

// ForkRepositoryInput carries the fields of a repository fork. Empty fields
// are omitted from the request body so Forgejo applies its defaults.
type ForkRepositoryInput struct {
	Organization  string `json:"organization,omitempty"`
	Name          string `json:"name,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
}

// UpdateRepositoryInput carries the fields of an in-place repository edit.
// Private is a pointer so an omitted value leaves the visibility untouched.
type UpdateRepositoryInput struct {
	Description   string `json:"description,omitempty"`
	Website       string `json:"website,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
	Private       *bool  `json:"private,omitempty"`
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

// ReleaseAsset is a file attached to a release.
type ReleaseAsset struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	DownloadURL        string `json:"download_url"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Label is a Forgejo issue label.
type Label struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// Milestone is a Forgejo milestone.
type Milestone struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	State        string `json:"state"`
	OpenIssues   int    `json:"open_issues"`
	ClosedIssues int    `json:"closed_issues"`
	DueOn        string `json:"due_on"`
}

// CreateMilestoneInput carries the fields needed to create a milestone.
type CreateMilestoneInput struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	DueOn       string `json:"due_on,omitempty"`
}

// UpdateMilestoneInput carries the fields needed to edit a milestone.
type UpdateMilestoneInput struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	State       string `json:"state,omitempty"`
	DueOn       string `json:"due_on,omitempty"`
}

// CreateLabelInput carries the fields needed to create a label.
type CreateLabelInput struct {
	Name        string `json:"name"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

// UpdateLabelInput carries the fields needed to edit a label.
type UpdateLabelInput struct {
	Name        string `json:"name,omitempty"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
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

// CreateOrganizationInput carries the fields needed to create an organization.
type CreateOrganizationInput struct {
	Username    string `json:"username"`
	Description string `json:"description,omitempty"`
	FullName    string `json:"full_name,omitempty"`
}

// OrganizationWriteService creates organizations.
type OrganizationWriteService interface {
	// CreateOrganization creates a new organization.
	CreateOrganization(ctx context.Context, in CreateOrganizationInput) (Organization, error)
}

// OrganizationDeleteService permanently deletes an organization.
type OrganizationDeleteService interface {
	// DeleteOrganization deletes an organization by its username.
	DeleteOrganization(ctx context.Context, org string) error
}

// IssueService is implemented by the Forgejo REST client.
type IssueService interface {
	// GetIssue returns a single issue by its index number.
	GetIssue(ctx context.Context, owner, repo string, index int64) (Issue, error)
	// ListComments returns the comments of a single issue.
	ListComments(ctx context.Context, owner, repo string, index int64) ([]Comment, error)
}

// Diff is the unified text diff between two refs (basehead) or for a single
// pull request. The body is plain text, never JSON.
type Diff struct {
	BaseHead string `json:"basehead"`
	Text     string `json:"text"`
}

// Commit is a flattened view of a Forgejo commit. The wire format nests the
// message/author inside "commit"; the domain type keeps them flat.
type Commit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}

// Branch is a repository branch with the SHA of the commit it points to.
type Branch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
	Default   bool   `json:"default"`
	CommitSHA string `json:"commit_sha"`
}

// PullRequest is a pull request. The wire format carries extra fields (head,
// base, merged) that are not part of the domain shape.
type PullRequest struct {
	ID        int64  `json:"id"`
	Number    int64  `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	State     string `json:"state"`
	User      User   `json:"user"`
	HTMLURL   string `json:"html_url"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// PullFile is a single file changed by a pull request.
type PullFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int64  `json:"additions"`
	Deletions int64  `json:"deletions"`
	Changes   int64  `json:"changes"`
}

// Check is a single status check on a commit.
type Check struct {
	Context     string `json:"context"`
	State       string `json:"state"`
	TargetURL   string `json:"target_url"`
	Description string `json:"description"`
}

// PullRequestDetail bundles a pull request together with its changed files and
// combined checks, so a single tool call returns the complete result (SPEC
// 6.1 #12 / M5).
type PullRequestDetail struct {
	PullRequest PullRequest `json:"pull_request"`
	Files       []PullFile  `json:"files"`
	Checks      []Check     `json:"checks"`
}

// Release is a release of a repository.
type Release struct {
	ID         int64  `json:"id"`
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	CreatedAt  string `json:"created_at"`
}

// SearchService searches the Forgejo instance for repositories.
type SearchService interface {
	// SearchRepos searches repositories by q/topic/sort/order and an optional
	// private filter (nil leaves the visibility unconstrained).
	SearchRepos(ctx context.Context, q, topic, sort, order string, private *bool) ([]Repository, error)
}

// DiffService returns unified text diffs for a compare range or a pull request.
type DiffService interface {
	// GetDiff returns the text diff between two refs (basehead).
	GetDiff(ctx context.Context, owner, repo, basehead string) (Diff, error)
	// GetPullDiff returns the text diff of a single pull request.
	GetPullDiff(ctx context.Context, owner, repo string, index int64) (Diff, error)
}

// CommitService lists commits of a repository.
type CommitService interface {
	// ListCommits lists commits of a branch/ref with pagination.
	ListCommits(ctx context.Context, owner, repo, branch string, page, limit int) ([]Commit, error)
}

// BranchService lists branches of a repository.
type BranchService interface {
	// ListBranches lists the branches of a repository.
	ListBranches(ctx context.Context, owner, repo string) ([]Branch, error)
}

// IssueListService lists issues of a repository.
type IssueListService interface {
	// ListIssues lists issues filtered by state with pagination.
	ListIssues(ctx context.Context, owner, repo, state string, page, limit int) ([]Issue, error)
}

// PullRequestService lists and retrieves pull requests.
type PullRequestService interface {
	// ListPullRequests lists pull requests filtered by state with pagination.
	ListPullRequests(ctx context.Context, owner, repo, state string, page, limit int) ([]PullRequest, error)
	// GetPullRequest returns a PR together with its changed files and checks.
	GetPullRequest(ctx context.Context, owner, repo string, index int64) (PullRequestDetail, error)
}

// ReleaseService lists and retrieves releases.
type ReleaseService interface {
	// ListReleases lists the releases of a repository with pagination.
	ListReleases(ctx context.Context, owner, repo string, page, limit int) ([]Release, error)
	// GetLatestRelease returns the latest non-draft release of a repository.
	GetLatestRelease(ctx context.Context, owner, repo string) (Release, error)
}

// CreateRepositoryInput carries the fields needed to create a repository.
// An empty Owner creates under the current authenticated user; a non-empty
// Owner creates under that organization.
type CreateRepositoryInput struct {
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	Private       bool   `json:"private"`
	AutoInit      bool   `json:"auto_init"`
	License       string `json:"license,omitempty"`
	Gitignore     string `json:"gitignore,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
	Readme        string `json:"readme,omitempty"`
}

// RepositoryWriteService creates repositories.
type RepositoryWriteService interface {
	// CreateRepository creates a new repository.
	CreateRepository(ctx context.Context, in CreateRepositoryInput) (Repository, error)
}

// WriteFileInput carries the fields of a single-file write that either
// creates the file (if absent) or updates it (if present).
type WriteFileInput struct {
	Owner   string `json:"owner"`
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Message string `json:"message"`
	Content string `json:"content"`
}

// CreateFileInput is the wire contract for creating a new file.
type CreateFileInput struct {
	Owner   string `json:"owner"`
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Message string `json:"message"`
	Content string `json:"content"`
}

// UpdateFileInput is the wire contract for updating an existing file. SHA must
// be the blob SHA of the current version so the update is conflict-checked.
type UpdateFileInput struct {
	Owner   string `json:"owner"`
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Message string `json:"message"`
	SHA     string `json:"sha"`
	Content string `json:"content"`
}

// FileResult is the outcome of a create/update file operation.
type FileResult struct {
	Path      string `json:"path"`
	SHA       string `json:"sha"`
	Content   string `json:"content"`
	CommitSHA string `json:"commit_sha"`
}

// ChangeFileEntry describes a single file operation within a multi-file commit.
type ChangeFileEntry struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Operation string `json:"operation"`
}

// ChangeFilesInput carries the fields of a multi-file (single-commit) write.
type ChangeFilesInput struct {
	Owner   string            `json:"owner"`
	Repo    string            `json:"repo"`
	Branch  string            `json:"branch"`
	Message string            `json:"message"`
	Files   []ChangeFileEntry `json:"files"`
}

// ChangeFileResult is a single file's outcome in a multi-file write.
type ChangeFileResult struct {
	Path   string `json:"path"`
	SHA    string `json:"sha"`
	Status string `json:"status"`
}

// ChangeFilesResult is the outcome of a multi-file write.
type ChangeFilesResult struct {
	CommitSHA string             `json:"commit_sha"`
	Files     []ChangeFileResult `json:"files"`
}

// FileWriteOrchestrator writes files to a repository. It is the write-side
// counterpart of RepositoryService: it probes an existing file and dispatches
// to create or update.
type FileWriteOrchestrator interface {
	// GetFile returns the content of a single file at path+ref.
	GetFile(ctx context.Context, owner, repo, path, ref string) (File, error)
	// CreateFile creates a new file.
	CreateFile(ctx context.Context, in CreateFileInput) (FileResult, error)
	// UpdateFile updates an existing file.
	UpdateFile(ctx context.Context, in UpdateFileInput) (FileResult, error)
	// ChangeFiles applies multiple file operations in one commit.
	ChangeFiles(ctx context.Context, in ChangeFilesInput) (ChangeFilesResult, error)
}

// BranchWriteService creates branches.
type BranchWriteService interface {
	// CreateBranch creates a new branch from an existing ref.
	CreateBranch(ctx context.Context, owner, repo, newBranch, oldRef string) (Branch, error)
}

// CreateIssueInput carries the fields needed to create an issue.
type CreateIssueInput struct {
	Owner     string  `json:"owner"`
	Repo      string  `json:"repo"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	Labels    []int64 `json:"labels"`
	Milestone int64   `json:"milestone"`
}

// UpdateIssueInput carries the fields needed to edit/close/reopen an issue.
type UpdateIssueInput struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Index int64  `json:"index"`
	Title string `json:"title"`
	Body  string `json:"body"`
	State string `json:"state"`
}

// IssueWriteService creates, updates and comments on issues.
type IssueWriteService interface {
	// CreateIssue creates a new issue.
	CreateIssue(ctx context.Context, in CreateIssueInput) (Issue, error)
	// UpdateIssue edits an existing issue (title/body/state).
	UpdateIssue(ctx context.Context, in UpdateIssueInput) (Issue, error)
	// CreateIssueComment appends a comment to an issue.
	CreateIssueComment(ctx context.Context, owner, repo string, index int64, body string) (Comment, error)
}

// CreatePullRequestInput carries the fields needed to open a pull request.
type CreatePullRequestInput struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
}

// UpdatePullRequestInput carries the fields needed to edit/close/reopen a PR.
type UpdatePullRequestInput struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Index int64  `json:"index"`
	Title string `json:"title"`
	Body  string `json:"body"`
	State string `json:"state"`
}

// Review is a pull request review.
type Review struct {
	ID    int64  `json:"id"`
	Body  string `json:"body"`
	State string `json:"state"`
}

// PullMergeResult reports the outcome of a merge request, distinguishing a
// fresh merge from a pull request that was already merged.
type PullMergeResult struct {
	Merged        bool `json:"merged"`
	AlreadyMerged bool `json:"already_merged"`
}

// PullRequestWriteService creates, updates, merges and reviews pull requests.
type PullRequestWriteService interface {
	// CreatePullRequest opens a new pull request.
	CreatePullRequest(ctx context.Context, in CreatePullRequestInput) (PullRequest, error)
	// UpdatePullRequest edits an existing pull request (title/body/state).
	UpdatePullRequest(ctx context.Context, in UpdatePullRequestInput) (PullRequest, error)
	// IsPullRequestMerged reports whether a pull request has been merged.
	IsPullRequestMerged(ctx context.Context, owner, repo string, index int64) (bool, error)
	// MergePullRequest merges a pull request with the given method.
	MergePullRequest(ctx context.Context, owner, repo string, index int64, method string) error
	// CreatePullReview creates a pending pull request review.
	CreatePullReview(ctx context.Context, owner, repo string, index int64, body string) (Review, error)
	// SubmitPullReview submits an existing review with a final event.
	SubmitPullReview(ctx context.Context, owner, repo string, index int64, reviewID int64, event string) (Review, error)
}

// CreateReleaseInput carries the fields needed to create a release.
type CreateReleaseInput struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Tag   string `json:"tag"`
	Name  string `json:"name"`
	Notes string `json:"notes"`
}

// ReleaseWriteService creates releases.
type ReleaseWriteService interface {
	// CreateRelease creates a release for an existing tag.
	CreateRelease(ctx context.Context, in CreateReleaseInput) (Release, error)
}

// DeleteFileInput carries the fields needed to delete a single file. SHA must
// be the blob SHA of the current version so the delete is conflict-checked.
type DeleteFileInput struct {
	Owner   string `json:"owner"`
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Message string `json:"message"`
	SHA     string `json:"sha"`
}

// FileDeleteService deletes a file from a repository. It first probes the
// file (to obtain its current SHA) and then deletes it.
type FileDeleteService interface {
	// GetFile returns the content of a single file at path+ref.
	GetFile(ctx context.Context, owner, repo, path, ref string) (File, error)
	// DeleteFile deletes a file at path+branch carrying the current blob SHA.
	DeleteFile(ctx context.Context, in DeleteFileInput) (FileResult, error)
}

// BranchDeleteService deletes a branch from a repository. It reads the
// repository first so it can refuse to delete the default branch.
type BranchDeleteService interface {
	// GetRepository returns details of a single repository.
	GetRepository(ctx context.Context, owner, repo string) (Repository, error)
	// DeleteBranch deletes a branch.
	DeleteBranch(ctx context.Context, owner, repo, branch string) error
}

// IssueDeleteService permanently deletes an issue.
type IssueDeleteService interface {
	// DeleteIssue deletes an issue by its index number.
	DeleteIssue(ctx context.Context, owner, repo string, index int64) error
}

// CommentDeleteService deletes an issue/pull request comment.
type CommentDeleteService interface {
	// DeleteComment deletes a comment by its ID.
	DeleteComment(ctx context.Context, owner, repo string, commentID int64) error
}

// ReleaseDeleteService deletes a release (the tag remains).
type ReleaseDeleteService interface {
	// DeleteRelease deletes a release by its ID.
	DeleteRelease(ctx context.Context, owner, repo string, id int64) error
}

// RepositoryDeleteService permanently deletes a repository.
type RepositoryDeleteService interface {
	// DeleteRepository deletes a repository by owner+name.
	DeleteRepository(ctx context.Context, owner, repo string) error
}

// TagService lists the git tags of a repository.
type TagService interface {
	// ListTags lists the tags of a repository.
	ListTags(ctx context.Context, owner, repo string) ([]Tag, error)
}

// TagWriteService creates git tags.
type TagWriteService interface {
	// CreateTag creates a tag pointing at target (a ref) with an optional message.
	CreateTag(ctx context.Context, owner, repo string, in CreateTagInput) (Tag, error)
}

// TagDeleteService deletes a git tag.
type TagDeleteService interface {
	// DeleteTag deletes a tag by its name.
	DeleteTag(ctx context.Context, owner, repo, tag string) error
}

// RepositoryForkService forks a repository.
type RepositoryForkService interface {
	// ForkRepository forks a repository into an organization or user namespace.
	ForkRepository(ctx context.Context, owner, repo string, in ForkRepositoryInput) (Repository, error)
}

// RepositoryUpdateService edits a repository in place. Repeating the same edit
// is idempotent.
type RepositoryUpdateService interface {
	// UpdateRepository edits a repository (description/website/default_branch/private).
	UpdateRepository(ctx context.Context, owner, repo string, in UpdateRepositoryInput) (Repository, error)
}

// MilestoneService lists the milestones of a repository.
type MilestoneService interface {
	// ListMilestones lists the milestones of a repository.
	ListMilestones(ctx context.Context, owner, repo string) ([]Milestone, error)
}

// MilestoneWriteService creates and edits milestones.
type MilestoneWriteService interface {
	// CreateMilestone creates a new milestone.
	CreateMilestone(ctx context.Context, owner, repo string, in CreateMilestoneInput) (Milestone, error)
	// UpdateMilestone edits an existing milestone. Repeating the same edit is idempotent.
	UpdateMilestone(ctx context.Context, owner, repo string, id int64, in UpdateMilestoneInput) (Milestone, error)
}

// MilestoneDeleteService deletes a milestone.
type MilestoneDeleteService interface {
	// DeleteMilestone deletes a milestone by its ID.
	DeleteMilestone(ctx context.Context, owner, repo string, id int64) error
}

// LabelService lists the labels of a repository.
type LabelService interface {
	// ListLabels lists the labels of a repository.
	ListLabels(ctx context.Context, owner, repo string) ([]Label, error)
}

// LabelWriteService creates and edits labels.
type LabelWriteService interface {
	// CreateLabel creates a new label.
	CreateLabel(ctx context.Context, owner, repo string, in CreateLabelInput) (Label, error)
	// UpdateLabel edits an existing label. Repeating the same edit is idempotent.
	UpdateLabel(ctx context.Context, owner, repo string, id int64, in UpdateLabelInput) (Label, error)
}

// LabelDeleteService deletes a label.
type LabelDeleteService interface {
	// DeleteLabel deletes a label by its ID.
	DeleteLabel(ctx context.Context, owner, repo string, id int64) error
}

// IssueLabelsService sets the exact set of labels on an issue.
type IssueLabelsService interface {
	// SetIssueLabels replaces the label set of an issue. Repeating the same set
	// is idempotent.
	SetIssueLabels(ctx context.Context, owner, repo string, index int64, labelIDs []int64) error
}

// UserService retrieves a single user. An empty username returns the current
// authenticated user.
type UserService interface {
	// GetUser returns a user by username, or the current user when username is
	// empty.
	GetUser(ctx context.Context, username string) (User, error)
}

// UserSearchService searches users by a query string.
type UserSearchService interface {
	// SearchUsers searches users by query q.
	SearchUsers(ctx context.Context, q string) ([]User, error)
}

// ReleaseAssetService uploads a file attachment to a release.
type ReleaseAssetService interface {
	// UploadReleaseAsset uploads content as a named attachment to a release.
	UploadReleaseAsset(ctx context.Context, owner, repo string, releaseID int64, filename string, content []byte) (ReleaseAsset, error)
}

// BranchReadService reads a single branch of a repository. It is kept separate
// from BranchService (which lists branches) so a caller can depend on exactly
// the read it needs.
type BranchReadService interface {
	// GetBranch returns a single branch by name.
	GetBranch(ctx context.Context, owner, repo, branch string) (Branch, error)
}
