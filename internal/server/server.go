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
