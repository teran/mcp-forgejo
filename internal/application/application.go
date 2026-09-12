// Package application implements the use cases (tool handlers) of mcp-forgejo.
// It depends only on the pure domain interfaces; it has no knowledge of the
// transport, the MCP SDK, or the Forgejo HTTP client.
package application

import (
	"context"
	"errors"
	"strings"

	"example.com/teran/mcp-forgejo/internal/domain"
)

// GetRepository returns a single repository by owner+name.
func GetRepository(ctx context.Context, svc domain.RepositoryService, owner, repo string) (domain.Repository, error) {
	return svc.GetRepository(ctx, owner, repo)
}

// ListContents lists the entries of a directory at path+ref.
func ListContents(ctx context.Context, svc domain.RepositoryService, owner, repo, path, ref string, page, limit int) ([]domain.FileEntry, error) {
	return svc.ListContents(ctx, owner, repo, path, ref, page, limit)
}

// GetFile returns the content of a single file at path+ref.
func GetFile(ctx context.Context, svc domain.RepositoryService, owner, repo, path, ref string) (domain.File, error) {
	return svc.GetFile(ctx, owner, repo, path, ref)
}

// ListOrganizations returns the organizations the current user belongs to.
func ListOrganizations(ctx context.Context, svc domain.OrganizationService) ([]domain.Organization, error) {
	return svc.ListOrganizations(ctx)
}

// GetIssue returns an issue together with its comments in a single call.
func GetIssue(ctx context.Context, svc domain.IssueService, owner, repo string, index int64) (domain.IssueWithComments, error) {
	issue, err := svc.GetIssue(ctx, owner, repo, index)
	if err != nil {
		return domain.IssueWithComments{}, err
	}
	comments, err := svc.ListComments(ctx, owner, repo, index)
	if err != nil {
		return domain.IssueWithComments{}, err
	}
	return domain.IssueWithComments{Issue: issue, Comments: comments}, nil
}

// SearchRepos searches the Forgejo instance for repositories.
func SearchRepos(ctx context.Context, svc domain.SearchService, q, topic, sort, order string, private *bool) ([]domain.Repository, error) {
	return svc.SearchRepos(ctx, q, topic, sort, order, private)
}

// GetDiff returns a unified diff for a compare range (basehead) or for a
// single pull request (when prIndex > 0 and basehead is empty).
func GetDiff(ctx context.Context, svc domain.DiffService, owner, repo, basehead string, prIndex int64) (domain.Diff, error) {
	if basehead != "" {
		return svc.GetDiff(ctx, owner, repo, basehead)
	}
	return svc.GetPullDiff(ctx, owner, repo, prIndex)
}

// ListCommits lists commits of a repository branch with pagination.
func ListCommits(ctx context.Context, svc domain.CommitService, owner, repo, branch string, page, limit int) ([]domain.Commit, error) {
	return svc.ListCommits(ctx, owner, repo, branch, page, limit)
}

// ListBranches lists the branches of a repository.
func ListBranches(ctx context.Context, svc domain.BranchService, owner, repo string) ([]domain.Branch, error) {
	return svc.ListBranches(ctx, owner, repo)
}

// ListIssues lists issues filtered by state with pagination.
func ListIssues(ctx context.Context, svc domain.IssueListService, owner, repo, state string, page, limit int) ([]domain.Issue, error) {
	return svc.ListIssues(ctx, owner, repo, state, page, limit)
}

// ListPullRequests lists pull requests filtered by state with pagination.
func ListPullRequests(ctx context.Context, svc domain.PullRequestService, owner, repo, state string, page, limit int) ([]domain.PullRequest, error) {
	return svc.ListPullRequests(ctx, owner, repo, state, page, limit)
}

// GetPullRequest returns a pull request together with its files and checks.
func GetPullRequest(ctx context.Context, svc domain.PullRequestService, owner, repo string, number int64) (domain.PullRequestDetail, error) {
	return svc.GetPullRequest(ctx, owner, repo, number)
}

// ListReleases lists the releases of a repository, or fetches the latest
// release when latest is true (returned as a single-element slice).
func ListReleases(ctx context.Context, svc domain.ReleaseService, owner, repo string, latest bool, page, limit int) ([]domain.Release, error) {
	if latest {
		rel, err := svc.GetLatestRelease(ctx, owner, repo)
		if err != nil {
			return nil, err
		}
		return []domain.Release{rel}, nil
	}
	return svc.ListReleases(ctx, owner, repo, page, limit)
}

// validationError builds a KindValidation domain error. It is returned when a
// use-case input fails validation before any service call is made.
func validationError(msg string) error {
	return domain.NewForgejoError(domain.KindValidation, msg)
}

// CreateRepository creates a repository. An empty name is rejected before the
// service is invoked (SPEC 6.2 #14).
func CreateRepository(ctx context.Context, svc domain.RepositoryWriteService, in domain.CreateRepositoryInput) (domain.Repository, error) {
	if strings.TrimSpace(in.Name) == "" {
		return domain.Repository{}, validationError("repository name is required")
	}
	return svc.CreateRepository(ctx, in)
}

// WriteFile writes a single file: it probes for the file and creates it when
// absent or updates it (carrying the existing blob SHA) when present. A probe
// error that is not a 404 is propagated without any write. Not idempotent —
// every call records a new commit (SPEC 6.2 #15).
func WriteFile(ctx context.Context, svc domain.FileWriteOrchestrator, in domain.WriteFileInput) (domain.FileResult, error) {
	if strings.TrimSpace(in.Path) == "" {
		return domain.FileResult{}, validationError("file path is required")
	}
	existing, err := svc.GetFile(ctx, in.Owner, in.Repo, in.Path, in.Branch)
	if err != nil {
		var fe *domain.ForgejoError
		if errors.As(err, &fe) && fe.Kind == domain.KindNotFound {
			return svc.CreateFile(ctx, domain.CreateFileInput(in))
		}
		return domain.FileResult{}, err
	}
	return svc.UpdateFile(ctx, domain.UpdateFileInput{
		Owner: in.Owner, Repo: in.Repo, Path: in.Path, Branch: in.Branch, Message: in.Message,
		SHA: existing.SHA, Content: in.Content,
	})
}

// WriteManyFiles applies several file operations in a single commit
// (SPEC 6.2 #16).
func WriteManyFiles(ctx context.Context, svc domain.FileWriteOrchestrator, in domain.ChangeFilesInput) (domain.ChangeFilesResult, error) {
	return svc.ChangeFiles(ctx, in)
}

// CreateBranch creates a new branch from an existing ref (SPEC 6.2 #17).
func CreateBranch(ctx context.Context, svc domain.BranchWriteService, owner, repo, newBranch, oldRef string) (domain.Branch, error) {
	if strings.TrimSpace(newBranch) == "" {
		return domain.Branch{}, validationError("branch name is required")
	}
	return svc.CreateBranch(ctx, owner, repo, newBranch, oldRef)
}

// CreateIssue creates an issue (SPEC 6.2 #18).
func CreateIssue(ctx context.Context, svc domain.IssueWriteService, in domain.CreateIssueInput) (domain.Issue, error) {
	if strings.TrimSpace(in.Title) == "" {
		return domain.Issue{}, validationError("issue title is required")
	}
	return svc.CreateIssue(ctx, in)
}

// UpdateIssue edits an existing issue (title/body/state). Repeating the same
// edit is idempotent (SPEC 6.2 #19).
func UpdateIssue(ctx context.Context, svc domain.IssueWriteService, in domain.UpdateIssueInput) (domain.Issue, error) {
	return svc.UpdateIssue(ctx, in)
}

// AddIssueComment appends a comment to an issue. Each call adds a new comment,
// so it is not idempotent (SPEC 6.2 #20).
func AddIssueComment(ctx context.Context, svc domain.IssueWriteService, owner, repo string, index int64, body string) (domain.Comment, error) {
	if strings.TrimSpace(body) == "" {
		return domain.Comment{}, validationError("comment body is required")
	}
	return svc.CreateIssueComment(ctx, owner, repo, index, body)
}

// CreatePullRequest opens a pull request (SPEC 6.2 #21).
func CreatePullRequest(ctx context.Context, svc domain.PullRequestWriteService, in domain.CreatePullRequestInput) (domain.PullRequest, error) {
	if strings.TrimSpace(in.Title) == "" {
		return domain.PullRequest{}, validationError("pull request title is required")
	}
	return svc.CreatePullRequest(ctx, in)
}

// UpdatePullRequest edits an existing pull request. Repeating the same edit is
// idempotent (SPEC 6.2 #22).
func UpdatePullRequest(ctx context.Context, svc domain.PullRequestWriteService, in domain.UpdatePullRequestInput) (domain.PullRequest, error) {
	return svc.UpdatePullRequest(ctx, in)
}

// MergePullRequest merges a pull request, first checking whether it has already
// been merged. An already-merged PR is reported as such without a further merge
// call (SPEC 6.2 #23).
func MergePullRequest(ctx context.Context, svc domain.PullRequestWriteService, owner, repo string, index int64, method string) (domain.PullMergeResult, error) {
	switch method {
	case "merge", "squash", "rebase":
	default:
		return domain.PullMergeResult{}, validationError("invalid merge method")
	}
	merged, err := svc.IsPullRequestMerged(ctx, owner, repo, index)
	if err != nil {
		return domain.PullMergeResult{}, err
	}
	if merged {
		return domain.PullMergeResult{Merged: false, AlreadyMerged: true}, nil
	}
	if err := svc.MergePullRequest(ctx, owner, repo, index, method); err != nil {
		return domain.PullMergeResult{}, err
	}
	return domain.PullMergeResult{Merged: true, AlreadyMerged: false}, nil
}

// ReviewPullRequest creates and then submits a pull request review in a single
// call; the submit references the review ID produced by the create
// (SPEC 6.2 #24).
func ReviewPullRequest(ctx context.Context, svc domain.PullRequestWriteService, owner, repo string, index int64, body, event string) (domain.Review, error) {
	switch strings.ToLower(event) {
	case "approve", "approved", "comment", "request_changes", "requestchanges":
	default:
		return domain.Review{}, validationError("invalid review event")
	}
	review, err := svc.CreatePullReview(ctx, owner, repo, index, body)
	if err != nil {
		return domain.Review{}, err
	}
	return svc.SubmitPullReview(ctx, owner, repo, index, review.ID, event)
}

// CreateRelease creates a release for an existing tag (SPEC 6.2 #25).
func CreateRelease(ctx context.Context, svc domain.ReleaseWriteService, in domain.CreateReleaseInput) (domain.Release, error) {
	if strings.TrimSpace(in.Tag) == "" {
		return domain.Release{}, validationError("release tag is required")
	}
	return svc.CreateRelease(ctx, in)
}

// DeleteFile deletes a file at path+branch. It first probes the file to obtain
// its current blob SHA; a failed probe is propagated without any delete, so a
// missing or unreadable file is never silently deleted (SPEC 6.3 #26).
func DeleteFile(ctx context.Context, svc domain.FileDeleteService, in domain.DeleteFileInput) (domain.FileResult, error) {
	if strings.TrimSpace(in.Path) == "" {
		return domain.FileResult{}, validationError("file path is required")
	}
	existing, err := svc.GetFile(ctx, in.Owner, in.Repo, in.Path, in.Branch)
	if err != nil {
		return domain.FileResult{}, err
	}
	in.SHA = existing.SHA
	return svc.DeleteFile(ctx, in)
}

// DeleteBranch deletes a branch. The repository is read first so the default
// branch can never be deleted (SPEC 6.3 #27).
func DeleteBranch(ctx context.Context, svc domain.BranchDeleteService, owner, repo, branch string) error {
	if strings.TrimSpace(branch) == "" {
		return validationError("branch name is required")
	}
	r, err := svc.GetRepository(ctx, owner, repo)
	if err != nil {
		return err
	}
	if r.DefaultBranch == branch {
		return validationError("cannot delete the default branch")
	}
	return svc.DeleteBranch(ctx, owner, repo, branch)
}

// DeleteIssue permanently deletes an issue by its index (SPEC 6.3 #28).
func DeleteIssue(ctx context.Context, svc domain.IssueDeleteService, owner, repo string, index int64) error {
	if index <= 0 {
		return validationError("issue index must be positive")
	}
	return svc.DeleteIssue(ctx, owner, repo, index)
}

// DeleteComment deletes an issue/pull request comment by its ID (SPEC 6.3 #29).
func DeleteComment(ctx context.Context, svc domain.CommentDeleteService, owner, repo string, commentID int64) error {
	if commentID <= 0 {
		return validationError("comment id must be positive")
	}
	return svc.DeleteComment(ctx, owner, repo, commentID)
}

// DeleteRelease deletes a release by its ID; the tag remains (SPEC 6.3 #30).
func DeleteRelease(ctx context.Context, svc domain.ReleaseDeleteService, owner, repo string, id int64) error {
	if id <= 0 {
		return validationError("release id must be positive")
	}
	return svc.DeleteRelease(ctx, owner, repo, id)
}

// DeleteRepository permanently deletes a repository. It requires explicit
// confirmation; without it the deletion is refused before any service call
// (SPEC 6.3 #31).
func DeleteRepository(ctx context.Context, svc domain.RepositoryDeleteService, owner, repo string, confirm bool) error {
	if !confirm {
		return validationError("permanent repository deletion requires explicit confirmation")
	}
	return svc.DeleteRepository(ctx, owner, repo)
}
