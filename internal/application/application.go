// Package application implements the use cases (tool handlers) of mcp-forgejo.
// It depends only on the pure domain interfaces; it has no knowledge of the
// transport, the MCP SDK, or the Forgejo HTTP client.
package application

import (
	"context"

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
