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
	registerWriteTools(s, client)
	registerDeleteTools(s, client)
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

// writeAnnotations builds the annotation set for write/update tools
// (SPEC 6.0 / 6.2): readOnlyHint=false, destructiveHint=false,
// openWorldHint=false. idempotentHint is true only for tools whose repeated
// invocation with identical args has no extra effect (issue_update,
// pull_update).
func writeAnnotations(title string, idempotent bool) *mcp.ToolAnnotations {
	destructive := false
	openWorld := false
	return &mcp.ToolAnnotations{
		Title:           title,
		ReadOnlyHint:    false,
		DestructiveHint: &destructive,
		IdempotentHint:  idempotent,
		OpenWorldHint:   &openWorld,
	}
}

// deleteAnnotations builds the annotation set for destructive delete tools
// (SPEC 6.0 / 6.3): readOnlyHint=false, destructiveHint=TRUE,
// openWorldHint=false, idempotentHint=false (repeating a delete is never a
// no-op).
func deleteAnnotations(title string) *mcp.ToolAnnotations {
	destructive := true
	openWorld := false
	return &mcp.ToolAnnotations{
		Title:           title,
		ReadOnlyHint:    false,
		DestructiveHint: &destructive,
		IdempotentHint:  false,
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

// registerWriteTools registers the Batch B write/update tools (SPEC 6.2
// #14–#25). They are grouped read -> write -> delete (S3).
func registerWriteTools(s *mcp.Server, client *forgejo.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_repo_create",
		Title:       "Create repository",
		Description: "Create a repository (optionally under an organization). Creating a name that already exists conflicts; not idempotent.",
		Annotations: writeAnnotations("Create repository", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in repoCreateIn) (*mcp.CallToolResult, domain.Repository, error) {
		repo, err := application.CreateRepository(ctx, client, domain.CreateRepositoryInput{
			Owner: in.Owner, Name: in.Name, Private: in.Private, AutoInit: in.AutoInit,
		})
		return nil, repo, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_file_write",
		Title:       "Write/update file",
		Description: "Write text content to a file at path+branch in a single commit. Creates the file when absent, updates it when present. Not idempotent: every call records a new commit.",
		Annotations: writeAnnotations("Write/update file", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in fileWriteIn) (*mcp.CallToolResult, domain.FileResult, error) {
		res, err := application.WriteFile(ctx, client, domain.WriteFileInput{
			Owner: in.Owner, Repo: in.Repo, Path: in.Path, Branch: in.Branch, Message: in.Message, Content: in.Content,
		})
		return nil, res, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_file_write_many",
		Title:       "Write multiple files",
		Description: "Create/update/delete several files in one commit at a branch with a commit message.",
		Annotations: writeAnnotations("Write multiple files", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in fileWriteManyIn) (*mcp.CallToolResult, domain.ChangeFilesResult, error) {
		files := make([]domain.ChangeFileEntry, 0, len(in.Files))
		for _, f := range in.Files {
			files = append(files, domain.ChangeFileEntry{Path: f.Path, Content: f.Content, Operation: f.Operation})
		}
		res, err := application.WriteManyFiles(ctx, client, domain.ChangeFilesInput{
			Owner: in.Owner, Repo: in.Repo, Branch: in.Branch, Message: in.Message, Files: files,
		})
		return nil, res, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_branch_create",
		Title:       "Create branch",
		Description: "Create a branch from an existing ref. Creating an existing branch conflicts; not idempotent.",
		Annotations: writeAnnotations("Create branch", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in branchCreateIn) (*mcp.CallToolResult, domain.Branch, error) {
		branch, err := application.CreateBranch(ctx, client, in.Owner, in.Repo, in.NewBranch, in.OldRef)
		return nil, branch, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_issue_create",
		Title:       "Create issue",
		Description: "Create an issue with a title, body, labels and milestone.",
		Annotations: writeAnnotations("Create issue", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in issueCreateIn) (*mcp.CallToolResult, domain.Issue, error) {
		issue, err := application.CreateIssue(ctx, client, domain.CreateIssueInput{
			Owner: in.Owner, Repo: in.Repo, Title: in.Title, Body: in.Body, Labels: in.Labels, Milestone: in.Milestone,
		})
		return nil, issue, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_issue_update",
		Title:       "Update/close/reopen issue",
		Description: "Edit an existing issue's title/body or set its state to open/closed. Repeating the same edit is idempotent.",
		Annotations: writeAnnotations("Update/close/reopen issue", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in issueUpdateIn) (*mcp.CallToolResult, domain.Issue, error) {
		issue, err := application.UpdateIssue(ctx, client, domain.UpdateIssueInput{
			Owner: in.Owner, Repo: in.Repo, Index: in.Index, Title: in.Title, Body: in.Body, State: in.State,
		})
		return nil, issue, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_issue_comment_add",
		Title:       "Add issue comment",
		Description: "Append a comment to an issue. Each call adds a new comment; not idempotent.",
		Annotations: writeAnnotations("Add issue comment", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in issueCommentAddIn) (*mcp.CallToolResult, domain.Comment, error) {
		comment, err := application.AddIssueComment(ctx, client, in.Owner, in.Repo, in.Index, in.Body)
		return nil, comment, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_pull_create",
		Title:       "Create pull request",
		Description: "Open a pull request from a head branch to a base branch with a title/body.",
		Annotations: writeAnnotations("Create pull request", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pullCreateIn) (*mcp.CallToolResult, domain.PullRequest, error) {
		pr, err := application.CreatePullRequest(ctx, client, domain.CreatePullRequestInput{
			Owner: in.Owner, Repo: in.Repo, Title: in.Title, Body: in.Body, Head: in.Head, Base: in.Base,
		})
		return nil, pr, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_pull_update",
		Title:       "Update/close/reopen pull request",
		Description: "Edit an existing pull request's title/body or set its state to open/closed. Repeating the same edit is idempotent.",
		Annotations: writeAnnotations("Update/close/reopen pull request", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pullUpdateIn) (*mcp.CallToolResult, domain.PullRequest, error) {
		pr, err := application.UpdatePullRequest(ctx, client, domain.UpdatePullRequestInput{
			Owner: in.Owner, Repo: in.Repo, Index: in.Index, Title: in.Title, Body: in.Body, State: in.State,
		})
		return nil, pr, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_pull_merge",
		Title:       "Merge pull request",
		Description: "Merge a pull request with a merge/squash/rebase method and report whether it was merged or already merged.",
		Annotations: writeAnnotations("Merge pull request", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pullMergeIn) (*mcp.CallToolResult, domain.PullMergeResult, error) {
		res, err := application.MergePullRequest(ctx, client, in.Owner, in.Repo, in.Index, in.Method)
		return nil, res, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_pull_review",
		Title:       "Review pull request",
		Description: "Create and submit a pull request review (approve/comment/request_changes) in one call.",
		Annotations: writeAnnotations("Review pull request", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pullReviewIn) (*mcp.CallToolResult, domain.Review, error) {
		review, err := application.ReviewPullRequest(ctx, client, in.Owner, in.Repo, in.Index, in.Body, in.Event)
		return nil, review, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_release_create",
		Title:       "Create release",
		Description: "Create a release for an existing tag with a title and notes.",
		Annotations: writeAnnotations("Create release", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in releaseCreateIn) (*mcp.CallToolResult, domain.Release, error) {
		release, err := application.CreateRelease(ctx, client, domain.CreateReleaseInput{
			Owner: in.Owner, Repo: in.Repo, Tag: in.Tag, Name: in.Name, Notes: in.Notes,
		})
		return nil, release, err
	})
}

type repoCreateIn struct {
	Owner    string `json:"owner,omitempty" jsonschema:"Repository owner/namespace; empty creates under the current user"`
	Name     string `json:"name" jsonschema:"Repository name"`
	Private  bool   `json:"private,omitempty" jsonschema:"Create a private repository"`
	AutoInit bool   `json:"auto_init,omitempty" jsonschema:"Initialize the repository with a README"`
}

type fileWriteIn struct {
	Owner   string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo    string `json:"repo" jsonschema:"Repository name"`
	Path    string `json:"path" jsonschema:"File path in the repository"`
	Branch  string `json:"branch,omitempty" jsonschema:"Branch to write to; defaults to the default branch"`
	Message string `json:"message" jsonschema:"Commit message"`
	Content string `json:"content" jsonschema:"File text content"`
}

type fileWriteManyIn struct {
	Owner   string        `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo    string        `json:"repo" jsonschema:"Repository name"`
	Branch  string        `json:"branch,omitempty" jsonschema:"Branch to write to; defaults to the default branch"`
	Message string        `json:"message" jsonschema:"Commit message"`
	Files   []fileEntryIn `json:"files" jsonschema:"File operations to apply in one commit"`
}

type fileEntryIn struct {
	Path      string `json:"path" jsonschema:"File path"`
	Content   string `json:"content,omitempty" jsonschema:"File content (text)"`
	Operation string `json:"operation" jsonschema:"Operation: create/update/delete"`
}

type branchCreateIn struct {
	Owner     string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo      string `json:"repo" jsonschema:"Repository name"`
	NewBranch string `json:"new_branch" jsonschema:"New branch name"`
	OldRef    string `json:"old_ref,omitempty" jsonschema:"Source branch/tag/sha; defaults to the default branch"`
}

type issueCreateIn struct {
	Owner     string  `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo      string  `json:"repo" jsonschema:"Repository name"`
	Title     string  `json:"title" jsonschema:"Issue title"`
	Body      string  `json:"body,omitempty" jsonschema:"Issue body"`
	Labels    []int64 `json:"labels,omitempty" jsonschema:"Label IDs to apply"`
	Milestone int64   `json:"milestone,omitempty" jsonschema:"Milestone ID"`
}

type issueUpdateIn struct {
	Owner string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
	Index int64  `json:"index" jsonschema:"Issue index number"`
	Title string `json:"title,omitempty" jsonschema:"New title"`
	Body  string `json:"body,omitempty" jsonschema:"New body"`
	State string `json:"state,omitempty" jsonschema:"New state (open/closed)"`
}

type issueCommentAddIn struct {
	Owner string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
	Index int64  `json:"index" jsonschema:"Issue index number"`
	Body  string `json:"body" jsonschema:"Comment text"`
}

type pullCreateIn struct {
	Owner string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
	Head  string `json:"head" jsonschema:"Head branch"`
	Base  string `json:"base" jsonschema:"Base branch"`
	Title string `json:"title" jsonschema:"Pull request title"`
	Body  string `json:"body,omitempty" jsonschema:"Pull request body"`
}

type pullUpdateIn struct {
	Owner string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
	Index int64  `json:"index" jsonschema:"Pull request number"`
	Title string `json:"title,omitempty" jsonschema:"New title"`
	Body  string `json:"body,omitempty" jsonschema:"New body"`
	State string `json:"state,omitempty" jsonschema:"New state (open/closed)"`
}

type pullMergeIn struct {
	Owner  string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo   string `json:"repo" jsonschema:"Repository name"`
	Index  int64  `json:"index" jsonschema:"Pull request number"`
	Method string `json:"method" jsonschema:"Merge method (merge/squash/rebase)"`
}

type pullReviewIn struct {
	Owner string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
	Index int64  `json:"index" jsonschema:"Pull request number"`
	Body  string `json:"body,omitempty" jsonschema:"Review body"`
	Event string `json:"event" jsonschema:"Review event (approve/comment/request_changes)"`
}

type releaseCreateIn struct {
	Owner string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
	Tag   string `json:"tag" jsonschema:"Tag name"`
	Name  string `json:"name,omitempty" jsonschema:"Release title"`
	Notes string `json:"notes,omitempty" jsonschema:"Release notes"`
}

// registerDeleteTools registers the Batch C delete tools (SPEC 6.3 #26–#31).
// They are registered last (read -> write -> delete, S3). Each is destructive
// and therefore requires confirmation, surfaced in the description.
func registerDeleteTools(s *mcp.Server, client *forgejo.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_file_delete",
		Title:       "Delete file",
		Description: "Delete a file at path+branch with a commit message. Destructive — confirm before use.",
		Annotations: deleteAnnotations("Delete file"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in fileDeleteIn) (*mcp.CallToolResult, domain.FileResult, error) {
		res, err := application.DeleteFile(ctx, client, domain.DeleteFileInput{
			Owner: in.Owner, Repo: in.Repo, Path: in.Path, Branch: in.Branch, Message: in.Message,
		})
		return nil, res, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_branch_delete",
		Title:       "Delete branch",
		Description: "Delete a branch. Destructive — confirm before use; will not delete the default branch.",
		Annotations: deleteAnnotations("Delete branch"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in branchDeleteIn) (*mcp.CallToolResult, any, error) {
		err := application.DeleteBranch(ctx, client, in.Owner, in.Repo, in.Branch)
		return nil, nil, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_issue_delete",
		Title:       "Delete issue",
		Description: "Permanently delete an issue. Destructive — confirm before use.",
		Annotations: deleteAnnotations("Delete issue"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in issueDeleteIn) (*mcp.CallToolResult, any, error) {
		err := application.DeleteIssue(ctx, client, in.Owner, in.Repo, in.Index)
		return nil, nil, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_comment_delete",
		Title:       "Delete comment",
		Description: "Delete an issue/PR comment. Destructive — confirm before use.",
		Annotations: deleteAnnotations("Delete comment"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in commentDeleteIn) (*mcp.CallToolResult, any, error) {
		err := application.DeleteComment(ctx, client, in.Owner, in.Repo, in.CommentID)
		return nil, nil, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_release_delete",
		Title:       "Delete release",
		Description: "Delete a release (tag remains). Destructive — confirm before use.",
		Annotations: deleteAnnotations("Delete release"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in releaseDeleteIn) (*mcp.CallToolResult, any, error) {
		err := application.DeleteRelease(ctx, client, in.Owner, in.Repo, in.ID)
		return nil, nil, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "forgejo_repo_delete",
		Title:       "Delete repository",
		Description: "Permanently delete a repository. Highly destructive — requires explicit confirmation before invoking.",
		Annotations: deleteAnnotations("Delete repository"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in repoDeleteIn) (*mcp.CallToolResult, any, error) {
		err := application.DeleteRepository(ctx, client, in.Owner, in.Repo, in.Confirm)
		return nil, nil, err
	})
}

type fileDeleteIn struct {
	Owner   string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo    string `json:"repo" jsonschema:"Repository name"`
	Path    string `json:"path" jsonschema:"File path in the repository"`
	Branch  string `json:"branch,omitempty" jsonschema:"Branch to delete from; defaults to the default branch"`
	Message string `json:"message" jsonschema:"Commit message"`
}

type branchDeleteIn struct {
	Owner  string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo   string `json:"repo" jsonschema:"Repository name"`
	Branch string `json:"branch" jsonschema:"Branch name to delete"`
}

type issueDeleteIn struct {
	Owner string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
	Index int64  `json:"index" jsonschema:"Issue index number"`
}

type commentDeleteIn struct {
	Owner     string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo      string `json:"repo" jsonschema:"Repository name"`
	CommentID int64  `json:"comment_id" jsonschema:"Comment ID"`
}

type releaseDeleteIn struct {
	Owner string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
	ID    int64  `json:"id" jsonschema:"Release ID"`
}

type repoDeleteIn struct {
	Owner   string `json:"owner" jsonschema:"Repository owner/namespace"`
	Repo    string `json:"repo" jsonschema:"Repository name"`
	Confirm bool   `json:"confirm" jsonschema:"Explicit confirmation; required to permanently delete the repository"`
}
