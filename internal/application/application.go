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
