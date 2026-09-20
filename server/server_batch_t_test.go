package server

// Batch T tools (SPEC §6): forgejo_tag_list, forgejo_tag_create,
// forgejo_tag_delete, forgejo_repo_fork, forgejo_repo_update. These tests
// verify the tools are registered, carry the correct annotations
// (tag_list read, tag_create/fork write non-idempotent, repo_update write
// idempotent, tag_delete destructive) and are invocable end to end against a
// mock Forgejo. The input field names below are the JSON schema fields
// @developer must expose on the server input structs.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teran/mcp-forgejo/domain"
	"github.com/teran/mcp-forgejo/infrastructure/forgejo"
)

// mockForgejoBatchT answers the Batch T endpoints.
func mockForgejoBatchT() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		m := r.Method

		switch {
		case m == http.MethodGet && p == "/api/v1/repos/acme/demo/tags":
			_, _ = w.Write([]byte(`[
				{"name":"v1.0","id":"abc123","sha":"abc123","message":"release 1","tarball_url":"https://x/t/v1.0.tar.gz","zipball_url":"https://x/z/v1.0.zip"},
				{"name":"v2.0","id":"def456","sha":"def456","message":"release 2","tarball_url":"https://x/t/v2.0.tar.gz","zipball_url":"https://x/z/v2.0.zip"}
			]`))
			return
		case m == http.MethodPost && p == "/api/v1/repos/acme/demo/tags":
			_, _ = w.Write([]byte(`{"name":"v3.0","id":"aaa111","sha":"aaa111","message":"release 3","tarball_url":"https://x/t/v3.0.tar.gz","zipball_url":"https://x/z/v3.0.zip"}`))
			return
		case m == http.MethodDelete && p == "/api/v1/repos/acme/demo/tags/v1.0":
			w.WriteHeader(http.StatusNoContent)
			return
		case m == http.MethodPost && p == "/api/v1/repos/acme/demo/forks":
			_, _ = w.Write([]byte(`{"id":9,"name":"demo-fork","full_name":"acme/demo-fork","default_branch":"main"}`))
			return
		case m == http.MethodPatch && p == "/api/v1/repos/acme/demo":
			_, _ = w.Write([]byte(`{"id":7,"name":"demo","full_name":"acme/demo","description":"new desc","website":"https://example.com","default_branch":"dev","private":false}`))
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"unexpected "` + m + `" ` + p + `"}`))
		}
	})
}

// setupBatchT builds the MCP server backed by mockForgejoBatchT and returns a
// connected client session.
func setupBatchT(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ts := httptest.NewServer(mockForgejoBatchT())
	t.Cleanup(ts.Close)

	s, err := Build(forgejo.Config{BaseURL: ts.URL, Token: testToken})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// batchTWriteIdempotent maps each Batch T write tool to its expected
// idempotentHint: true only for repo_update (repeating the same edit is a
// no-op); tag_create and repo_fork are not idempotent.
var batchTWriteIdempotent = map[string]bool{
	"forgejo_tag_create":  false,
	"forgejo_repo_fork":   false,
	"forgejo_repo_update": true,
}

func TestBatchTReadToolsRegistered(t *testing.T) {
	cs, _ := setup(t) // registration is transport-independent
	names := listToolMap(t, cs)
	if _, ok := names["forgejo_tag_list"]; !ok {
		t.Errorf("read tool %q not registered", "forgejo_tag_list")
	}
}

func TestBatchTWriteToolsRegistered(t *testing.T) {
	cs, _ := setup(t)
	names := listToolMap(t, cs)
	for name := range batchTWriteIdempotent {
		if _, ok := names[name]; !ok {
			t.Errorf("write tool %q not registered", name)
		}
	}
	if _, ok := names["forgejo_tag_delete"]; !ok {
		t.Errorf("delete tool %q not registered", "forgejo_tag_delete")
	}
}

func TestBatchTReadAnnotations(t *testing.T) {
	cs, _ := setup(t)
	byName := listToolMap(t, cs)
	tool, ok := byName["forgejo_tag_list"]
	if !ok {
		t.Fatalf("tool %q not registered", "forgejo_tag_list")
	}
	a := tool.Annotations
	if a == nil {
		t.Fatal("annotations missing")
	}
	if a.Title == "" {
		t.Errorf("annotations.title empty")
	}
	if !a.ReadOnlyHint || !a.IdempotentHint {
		t.Errorf("tag_list: readOnlyHint=%v idempotentHint=%v, want true/true", a.ReadOnlyHint, a.IdempotentHint)
	}
	if a.DestructiveHint == nil || *a.DestructiveHint {
		t.Errorf("tag_list: destructiveHint != false")
	}
	if a.OpenWorldHint == nil || *a.OpenWorldHint {
		t.Errorf("tag_list: openWorldHint != false")
	}
}

func TestBatchTWriteAnnotations(t *testing.T) {
	cs, _ := setup(t)
	byName := listToolMap(t, cs)
	for name, wantIdem := range batchTWriteIdempotent {
		tool, ok := byName[name]
		if !ok {
			t.Errorf("tool %q not registered", name)
			continue
		}
		a := tool.Annotations
		if a == nil {
			t.Errorf("tool %q: annotations missing", name)
			continue
		}
		if a.Title == "" {
			t.Errorf("tool %q: annotations.title empty", name)
		}
		if a.ReadOnlyHint {
			t.Errorf("tool %q: readOnlyHint != false", name)
		}
		if a.DestructiveHint == nil || *a.DestructiveHint {
			t.Errorf("tool %q: destructiveHint != false", name)
		}
		if a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("tool %q: openWorldHint != false", name)
		}
		if a.IdempotentHint != wantIdem {
			t.Errorf("tool %q: idempotentHint = %v, want %v", name, a.IdempotentHint, wantIdem)
		}
	}
}

func TestBatchTDeleteAnnotations(t *testing.T) {
	cs, _ := setup(t)
	byName := listToolMap(t, cs)
	tool, ok := byName["forgejo_tag_delete"]
	if !ok {
		t.Fatalf("tool %q not registered", "forgejo_tag_delete")
	}
	a := tool.Annotations
	if a == nil {
		t.Fatal("annotations missing")
	}
	if a.Title == "" {
		t.Errorf("annotations.title empty")
	}
	if a.ReadOnlyHint {
		t.Errorf("tag_delete: readOnlyHint != false")
	}
	if a.DestructiveHint == nil || !*a.DestructiveHint {
		t.Errorf("tag_delete: destructiveHint != true")
	}
	if a.IdempotentHint {
		t.Errorf("tag_delete: idempotentHint != false")
	}
	if a.OpenWorldHint == nil || *a.OpenWorldHint {
		t.Errorf("tag_delete: openWorldHint != false")
	}
	// Destructive tools surface an explicit confirmation prompt in the
	// description (SPEC 6.3).
	if !strings.Contains(tool.Description, "confirm") {
		t.Errorf("tag_delete description should require confirmation, got %q", tool.Description)
	}
}

// =============================================================================
// End-to-end invocation
// =============================================================================

func TestTagListTool(t *testing.T) {
	cs := setupBatchT(t)
	var tags domain.Items[domain.Tag]
	callTool(t, cs, "forgejo_tag_list", map[string]any{"owner": "acme", "repo": "demo"}, &tags)
	if len(tags.Items) != 2 || tags.Items[0].Name != "v1.0" || tags.Items[0].SHA != "abc123" ||
		tags.Items[1].Name != "v2.0" {
		t.Errorf("tags = %+v", tags)
	}
}

func TestTagCreateTool(t *testing.T) {
	cs := setupBatchT(t)
	var tag domain.Tag
	callTool(t, cs, "forgejo_tag_create", map[string]any{
		"owner": "acme", "repo": "demo", "name": "v3.0", "target": "main", "message": "release 3",
	}, &tag)
	if tag.Name != "v3.0" || tag.SHA != "aaa111" {
		t.Errorf("tag = %+v", tag)
	}
}

func TestTagDeleteTool(t *testing.T) {
	cs := setupBatchT(t)
	callTool(t, cs, "forgejo_tag_delete", map[string]any{
		"owner": "acme", "repo": "demo", "tag": "v1.0",
	}, nil)
}

func TestRepoForkTool(t *testing.T) {
	cs := setupBatchT(t)
	var repo domain.Repository
	callTool(t, cs, "forgejo_repo_fork", map[string]any{
		"owner": "acme", "repo": "demo", "organization": "team", "name": "demo-fork", "default_branch": "main",
	}, &repo)
	if repo.FullName != "acme/demo-fork" || repo.DefaultBranch != "main" {
		t.Errorf("repo = %+v", repo)
	}
}

func TestRepoUpdateTool(t *testing.T) {
	cs := setupBatchT(t)
	var repo domain.Repository
	callTool(t, cs, "forgejo_repo_update", map[string]any{
		"owner": "acme", "repo": "demo", "description": "new desc", "website": "https://example.com", "default_branch": "dev",
	}, &repo)
	if repo.Description != "new desc" || repo.DefaultBranch != "dev" {
		t.Errorf("repo = %+v", repo)
	}
}

// =============================================================================
// Error surfacing + token redaction (SPEC S2)
// =============================================================================

func TestBatchTToolErrorSurfacesIsError(t *testing.T) {
	cs, _ := setup(t) // read mock returns 404 for unknown Batch T paths
	for _, name := range []string{"forgejo_tag_list", "forgejo_tag_create", "forgejo_tag_delete", "forgejo_repo_fork", "forgejo_repo_update"} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      name,
			Arguments: map[string]any{"owner": "ghost", "repo": "missing"},
		})
		if err != nil {
			t.Fatalf("CallTool(%s) error = %v", name, err)
		}
		if !res.IsError {
			t.Errorf("%s: expected IsError for 404", name)
		}
		msg := textOf(t, res)
		if strings.Contains(msg, testToken) {
			t.Errorf("%s: token leaked in error: %q", name, msg)
		}
	}
}

// =============================================================================
// List object contract: tag_list returns a JSON object ({items:[...]})
// =============================================================================

func TestTagListReturnsObjectContract(t *testing.T) {
	cs := setupBatchT(t)
	res := callToolRaw(t, cs, "forgejo_tag_list", map[string]any{"owner": "acme", "repo": "demo"})
	items := listStructuredItems(t, res)
	if len(items) != 2 {
		t.Errorf("items length = %d, want 2", len(items))
	}
}

// =============================================================================
// JSON schema field pins
// =============================================================================

func TestBatchTToolJSONSchemaPinsFields(t *testing.T) {
	cs, _ := setup(t)
	byName := listToolMap(t, cs)

	check := func(toolName string, wantFields []string) {
		t.Helper()
		tool, ok := byName[toolName]
		if !ok {
			t.Errorf("tool %q not registered", toolName)
			return
		}
		schemaJSON, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal schema for %s: %v", toolName, err)
		}
		for _, f := range wantFields {
			if !strings.Contains(string(schemaJSON), `"`+f+`"`) {
				t.Errorf("tool %q schema missing field %q (schema=%s)", toolName, f, schemaJSON)
			}
		}
	}

	check("forgejo_tag_list", []string{"owner", "repo"})
	check("forgejo_tag_create", []string{"owner", "repo", "name", "target", "message"})
	check("forgejo_tag_delete", []string{"owner", "repo", "tag"})
	check("forgejo_repo_fork", []string{"owner", "repo", "organization", "name", "default_branch"})
	check("forgejo_repo_update", []string{"owner", "repo", "description", "website", "default_branch", "private"})
}
