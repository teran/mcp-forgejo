//go:build e2e

// Package e2e runs an end-to-end test that boots a REAL Forgejo instance (via
// the go-docker-testsuite Forgejo wrapper), builds the mcp-forgejo MCP server
// in-process pointing at it, and drives several MCP tool calls against the live
// instance to prove the whole stack is wired correctly.
//
// The test is guarded so it skips gracefully when Docker is unavailable or the
// Forgejo image cannot be started, keeping plain `go test ./...` green on
// machines without Docker.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teran/mcp-forgejo/internal/infrastructure/forgejo"
	"github.com/teran/mcp-forgejo/internal/server"

	forgejoapp "github.com/teran/go-docker-testsuite/applications/forgejo"
)

// Minimal result shapes mirroring the JSON emitted by the MCP server for the
// tools exercised below. Only the fields asserted on are declared.
type (
	repoResult struct {
		Name          string `json:"name"`
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	}

	orgResult struct {
		Username string `json:"username"`
		FullName string `json:"full_name"`
	}

	orgListResult struct {
		Items []orgResult `json:"items"`
	}

	fileResult struct {
		Path      string `json:"path"`
		SHA       string `json:"sha"`
		CommitSHA string `json:"commit_sha"`
	}

	fileEntryResult struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}

	contentsResult struct {
		Items []fileEntryResult `json:"items"`
	}
)

// TestE2EForgejo boots a real Forgejo container, mints a throwaway PAT, builds
// the MCP server in-process against it, and exercises create/read/write/list
// tools followed by cleanup of the created resources.
func TestE2EForgejo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 1. Start a real Forgejo instance; its lifecycle is tied to this test.
	app, err := forgejoapp.NewWithT(t, ctx, forgejoImage)
	if err != nil {
		t.Skipf("skipping e2e: cannot start Forgejo container (is Docker running / image present?): %v", err)
	}

	baseURL := app.MustURL()
	username := app.AdminUsername()
	password := app.AdminPassword()

	// 2. Mint a throwaway PAT for the admin user via the Forgejo REST API.
	//    The PAT is used only inside this test and is never logged.
	pat := createPAT(t, ctx, baseURL, username, password)

	// 3. Build the MCP server in-process, bound to the REAL Forgejo instance.
	s, err := server.Build(forgejo.Config{BaseURL: baseURL, Token: pat})
	if err != nil {
		t.Fatalf("Build server: %v", err)
	}

	// Connect the server and a client over the SDK's in-memory transports (the
	// same transport plumbing stdio uses, without spawning a subprocess).
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-client"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	// 4. Drive MCP tool calls against the live instance.
	// 4a. Read: list the admin user's organizations (currently empty).
	orgs := callJSON[orgListResult](t, cs, "forgejo_org_list", map[string]any{})
	if len(orgs.Items) != 0 {
		t.Fatalf("expected no organizations up front, got %d", len(orgs.Items))
	}

	// 4b. Write: create a repository under the current (admin) user. The MCP
	// tool creates under the current user when owner is omitted (the client
	// then targets POST /api/v1/user/repos rather than /api/v1/orgs/{owner}).
	repoName := fmt.Sprintf("e2e-repo-%d", time.Now().UnixNano())
	repo := callJSON[repoResult](t, cs, "forgejo_repo_create", map[string]any{
		"name":           repoName,
		"auto_init":      true,
		"default_branch": "main",
	})
	if repo.Name != repoName {
		t.Fatalf("created repo name = %q, want %q", repo.Name, repoName)
	}
	if repo.DefaultBranch != "main" {
		t.Fatalf("created repo default_branch = %q, want %q", repo.DefaultBranch, "main")
	}

	// 4c. Read: fetch the repository back and confirm it exists on the server.
	got := callJSON[repoResult](t, cs, "forgejo_repo_get", map[string]any{
		"owner": username, "repo": repoName,
	})
	if got.FullName != username+"/"+repoName {
		t.Fatalf("repo_get full_name = %q, want %q", got.FullName, username+"/"+repoName)
	}

	// 4d. Write: write a file into the repository (creates a commit).
	fileContent := "# e2e\n\nhello from mcp-forgejo e2e\n"
	wf := callJSON[fileResult](t, cs, "forgejo_file_write", map[string]any{
		"owner":   username,
		"repo":    repoName,
		"path":    "README.md",
		"branch":  "main",
		"message": "e2e: write README",
		"content": fileContent,
	})
	if wf.Path != "README.md" {
		t.Fatalf("file_write path = %q, want %q", wf.Path, "README.md")
	}
	if wf.SHA == "" {
		t.Fatal("file_write returned an empty sha")
	}

	// 4e. Read: list the repository root and confirm the written file is there.
	contents := callJSON[contentsResult](t, cs, "forgejo_repo_list_contents", map[string]any{
		"owner": username, "repo": repoName,
	})
	found := false
	for _, e := range contents.Items {
		if e.Name == "README.md" && e.Type == "file" {
			found = true
		}
	}
	if !found {
		t.Fatalf("README.md not found in repo root: %+v", contents.Items)
	}

	// 4f. Write: create an organization.
	orgName := fmt.Sprintf("e2e-org-%d", time.Now().UnixNano())
	org := callJSON[orgResult](t, cs, "forgejo_org_create", map[string]any{
		"username": orgName, "full_name": "E2E Org",
	})
	if org.Username != orgName {
		t.Fatalf("created org username = %q, want %q", org.Username, orgName)
	}

	// 4g. Read: org_list now reflects the created organization.
	orgs2 := callJSON[orgListResult](t, cs, "forgejo_org_list", map[string]any{})
	orgFound := false
	for _, o := range orgs2.Items {
		if o.Username == orgName {
			orgFound = true
		}
	}
	if !orgFound {
		t.Fatalf("created org %q not listed: %+v", orgName, orgs2.Items)
	}

	// 5. Cleanup: delete the repository and organization we created.
	callJSON[any](t, cs, "forgejo_repo_delete", map[string]any{
		"owner": username, "repo": repoName, "confirm": true,
	})
	callJSON[any](t, cs, "forgejo_org_delete", map[string]any{"org": orgName})
}

// callJSON invokes the named tool and decodes its JSON text content into T.
// It fails the test on protocol or tool-level errors.
func callJSON[T any](t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) T {
	t.Helper()
	var zero T

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) error = %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool(%s) returned error: %s", name, textOf(res))
	}
	if len(res.Content) == 0 {
		// delete tools return empty content; nothing to decode.
		return zero
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("CallTool(%s): expected TextContent, got %T", name, res.Content[0])
	}
	// Callers that do not want the result pass T = any and may ignore it, but
	// they still get here only if there is content to decode.
	if err := json.Unmarshal([]byte(tc.Text), &zero); err != nil {
		t.Fatalf("CallTool(%s): decode result: %v (text=%s)", name, err, truncate(tc.Text))
	}
	return zero
}

// TestE2ETokenMinting verifies the PAT-minting path in isolation against a live
// instance: the returned token must authenticate a Forgejo API call.
func TestE2ETokenMinting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	app, err := forgejoapp.NewWithT(t, ctx, forgejoImage)
	if err != nil {
		t.Skipf("skipping e2e: cannot start Forgejo container (is Docker running / image present?): %v", err)
	}

	baseURL := app.MustURL()
	pat := createPAT(t, ctx, baseURL, app.AdminUsername(), app.AdminPassword())

	// The PAT must authenticate: GET /api/v1/user with `Authorization: token`.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/user", nil)
	if err != nil {
		t.Fatalf("new user request: %v", err)
	}
	req.Header.Set("Authorization", "token "+pat)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("auth check: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PAT authentication: status %d, want 200", resp.StatusCode)
	}
}
