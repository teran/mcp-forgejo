// Package server wires the Forgejo client, the application use cases and the
// MCP SDK into a tool registry, and provides the transport drivers (stdio and
// HTTP/SSE). It is the adapter that binds the other layers together.
package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"example.com/teran/mcp-forgejo/internal/application"
	"example.com/teran/mcp-forgejo/internal/domain"
	"example.com/teran/mcp-forgejo/internal/infrastructure/forgejo"
)

// Implementation metadata advertised during MCP initialization.
const (
	implName    = "mcp-forgejo"
	implVersion = "0.1.0"
)

// Build constructs an *mcp.Server with all registered tools, bound to a
// Forgejo client built from cfg (the client owns its resty HTTP transport).
func Build(cfg forgejo.Config) (*mcp.Server, error) {
	client := forgejo.New(cfg)
	s := mcp.NewServer(&mcp.Implementation{Name: implName, Version: implVersion}, &mcp.ServerOptions{})
	registerTools(s, client)
	return s, nil
}

// registerTools registers every tool, grouped read -> write -> delete (S3).
// Stage 1 implements the read subset; further tools can be added here without
// reworking the registry.
func registerTools(s *mcp.Server, client *forgejo.Client) {
	registerRepoTools(s, client)
	registerOrgTools(s, client)
	registerIssueTools(s, client)
	registerBatchAReadTools(s, client)
}

// readAnnotations builds the annotation set for read-only tools (SPEC 6.0).
func readAnnotations(title string) *mcp.ToolAnnotations {
	destructive := false
	openWorld := false
	return &mcp.ToolAnnotations{
		Title:           title,
		ReadOnlyHint:    true,
		DestructiveHint: &destructive,
		IdempotentHint:  true,
		OpenWorldHint:   &openWorld,
	}
}

// registerRepoTools registers the repository/content read tools.
func registerRepoTools(s *mcp.Server, client *forgejo.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_repo_get",
		Title:       "Get repository",
		Description: "Returns details of a repository by owner+name. Read-only; never modifies.",
		Annotations: readAnnotations("Get repository"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in repoGetIn) (*mcp.CallToolResult, domain.Repository, error) {
		repo, err := application.GetRepository(ctx, client, in.Owner, in.Repo)
		return nil, repo, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_repo_list_contents",
		Title:       "List directory contents",
		Description: "Lists the entries (type/sha/size) of a directory at path+ref. Read-only.",
		Annotations: readAnnotations("List directory contents"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in repoListContentsIn) (*mcp.CallToolResult, []domain.FileEntry, error) {
		entries, err := application.ListContents(ctx, client, in.Owner, in.Repo, in.Path, in.Ref, in.Page, in.Limit)
		return nil, entries, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_file_get",
		Title:       "Read file",
		Description: "Returns the UTF-8 text content of a file at path+ref. If the blob is not valid UTF-8, returns a binary flag instead of decoding. Read-only.",
		Annotations: readAnnotations("Read file"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in fileGetIn) (*mcp.CallToolResult, domain.File, error) {
		file, err := application.GetFile(ctx, client, in.Owner, in.Repo, in.Path, in.Ref)
		return nil, file, err
	})
}

// registerOrgTools registers the organization read tools.
func registerOrgTools(s *mcp.Server, client *forgejo.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_org_list",
		Title:       "List my organizations",
		Description: "Lists the organizations the current user belongs to. Read-only.",
		Annotations: readAnnotations("List my organizations"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ orgListIn) (*mcp.CallToolResult, []domain.Organization, error) {
		orgs, err := application.ListOrganizations(ctx, client)
		return nil, orgs, err
	})
}

// registerIssueTools registers the issue read tools.
func registerIssueTools(s *mcp.Server, client *forgejo.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_issue_get",
		Title:       "Get issue + comments",
		Description: "Returns an issue and its comments in a single call. Read-only.",
		Annotations: readAnnotations("Get issue + comments"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in issueGetIn) (*mcp.CallToolResult, domain.IssueWithComments, error) {
		result, err := application.GetIssue(ctx, client, in.Owner, in.Repo, in.Index)
		return nil, result, err
	})
}

// registerBatchAReadTools registers the Batch A read tools (SPEC 6.1 #1, #6,
// #7, #8, #9, #11, #12, #13): repository search, diff, commits, branches,
// issues, pull requests and releases.
func registerBatchAReadTools(s *mcp.Server, client *forgejo.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_repo_search",
		Title:       "Search repositories",
		Description: "Search the Forgejo instance by q, topic, sort, order and an optional private filter. Returns matching repositories. Read-only.",
		Annotations: readAnnotations("Search repositories"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in repoSearchIn) (*mcp.CallToolResult, []domain.Repository, error) {
		repos, err := application.SearchRepos(ctx, client, in.Q, in.Topic, in.Sort, in.Order, in.Private)
		return nil, repos, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_diff_get",
		Title:       "Get diff",
		Description: "Returns a unified diff between two refs (basehead, e.g. main..dev) or for a pull request (pr). Read-only.",
		Annotations: readAnnotations("Get diff"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in diffGetIn) (*mcp.CallToolResult, domain.Diff, error) {
		diff, err := application.GetDiff(ctx, client, in.Owner, in.Repo, in.Basehead, in.PR)
		return nil, diff, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_commit_list",
		Title:       "List commits",
		Description: "Lists commits of a repository branch with pagination. Read-only.",
		Annotations: readAnnotations("List commits"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in commitListIn) (*mcp.CallToolResult, []domain.Commit, error) {
		commits, err := application.ListCommits(ctx, client, in.Owner, in.Repo, in.Branch, in.Page, in.Limit)
		return nil, commits, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_branch_list",
		Title:       "List branches",
		Description: "Lists the branches of a repository. Read-only.",
		Annotations: readAnnotations("List branches"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in branchListIn) (*mcp.CallToolResult, []domain.Branch, error) {
		branches, err := application.ListBranches(ctx, client, in.Owner, in.Repo)
		return nil, branches, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_issue_list",
		Title:       "List issues",
		Description: "Lists issues filtered by state (open/closed/all) with pagination. Read-only.",
		Annotations: readAnnotations("List issues"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in issueListIn) (*mcp.CallToolResult, []domain.Issue, error) {
		issues, err := application.ListIssues(ctx, client, in.Owner, in.Repo, in.State, in.Page, in.Limit)
		return nil, issues, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_pull_list",
		Title:       "List pull requests",
		Description: "Lists pull requests filtered by state (open/closed/all) with pagination. Read-only.",
		Annotations: readAnnotations("List pull requests"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pullListIn) (*mcp.CallToolResult, []domain.PullRequest, error) {
		prs, err := application.ListPullRequests(ctx, client, in.Owner, in.Repo, in.State, in.Page, in.Limit)
		return nil, prs, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_pull_get",
		Title:       "Get pull request + files + checks",
		Description: "Returns a pull request together with its changed files and combined checks status in a single call. Read-only.",
		Annotations: readAnnotations("Get pull request + files + checks"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pullGetIn) (*mcp.CallToolResult, domain.PullRequestDetail, error) {
		detail, err := application.GetPullRequest(ctx, client, in.Owner, in.Repo, in.Number)
		return nil, detail, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_release_list",
		Title:       "List releases",
		Description: "Lists the releases of a repository, or fetches the latest release when latest is true. Read-only.",
		Annotations: readAnnotations("List releases"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in releaseListIn) (*mcp.CallToolResult, []domain.Release, error) {
		releases, err := application.ListReleases(ctx, client, in.Owner, in.Repo, in.Latest, in.Page, in.Limit)
		return nil, releases, err
	})
}

// Input schemas. The 'jsonschema' tags provide property descriptions; the SDK
// generates the JSON Schema automatically from these structs.

type repoGetIn struct {
	Owner string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
}

type repoListContentsIn struct {
	Owner string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
	Path  string `json:"path,omitempty" jsonschema:"Directory path in the repo; empty lists the repo root"`
	Ref   string `json:"ref,omitempty" jsonschema:"Branch/tag/sha; defaults to the default branch"`
	Page  int    `json:"page,omitempty" jsonschema:"Page number (1-based); omitted when 0"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum number of entries per page; omitted when 0"`
}

type fileGetIn struct {
	Owner string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
	Path  string `json:"path" jsonschema:"File path in the repo"`
	Ref   string `json:"ref,omitempty" jsonschema:"Branch/tag/sha; defaults to the default branch"`
}

type orgListIn struct{}

type issueGetIn struct {
	Owner string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
	Index int64  `json:"index" jsonschema:"Issue index number"`
}

type repoSearchIn struct {
	Q       string `json:"q" jsonschema:"Search query"`
	Topic   string `json:"topic,omitempty" jsonschema:"Filter by topic"`
	Sort    string `json:"sort,omitempty" jsonschema:"Sort field (e.g. stars, updated)"`
	Order   string `json:"order,omitempty" jsonschema:"Sort order (asc/desc)"`
	Private *bool  `json:"private,omitempty" jsonschema:"Restrict to private repositories; omitted means any visibility"`
}

type diffGetIn struct {
	Owner    string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo     string `json:"repo" jsonschema:"Repository name"`
	Basehead string `json:"basehead,omitempty" jsonschema:"Base..head refs to compare (e.g. main..dev)"`
	PR       int64  `json:"pr,omitempty" jsonschema:"Pull request number to fetch its diff; used when basehead is empty"`
}

type commitListIn struct {
	Owner  string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo   string `json:"repo" jsonschema:"Repository name"`
	Branch string `json:"branch,omitempty" jsonschema:"Branch/ref to list commits for; defaults to the default branch"`
	Page   int    `json:"page,omitempty" jsonschema:"Page number (1-based); omitted when 0"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum number of commits per page; omitted when 0"`
}

type branchListIn struct {
	Owner string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
}

type issueListIn struct {
	Owner string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
	State string `json:"state,omitempty" jsonschema:"Issue state filter (open/closed/all)"`
	Page  int    `json:"page,omitempty" jsonschema:"Page number (1-based); omitted when 0"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum number of issues per page; omitted when 0"`
}

type pullListIn struct {
	Owner string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
	State string `json:"state,omitempty" jsonschema:"Pull request state filter (open/closed/all)"`
	Page  int    `json:"page,omitempty" jsonschema:"Page number (1-based); omitted when 0"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum number of pull requests per page; omitted when 0"`
}

type pullGetIn struct {
	Owner  string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo   string `json:"repo" jsonschema:"Repository name"`
	Number int64  `json:"number" jsonschema:"Pull request number"`
}

type releaseListIn struct {
	Owner  string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo   string `json:"repo" jsonschema:"Repository name"`
	Latest bool   `json:"latest,omitempty" jsonschema:"Fetch only the latest release"`
	Page   int    `json:"page,omitempty" jsonschema:"Page number (1-based); omitted when 0"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum number of releases per page; omitted when 0"`
}
