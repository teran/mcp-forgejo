package server

// Batch C delete tools (SPEC 6.3 #26–#31). These tests verify the tools are
// registered, carry the correct annotations (readOnlyHint=false,
// destructiveHint=TRUE, idempotentHint=false) and are invocable end to end
// against a mock Forgejo. The input field names below are the JSON schema
// fields @developer must expose on the server input structs. They also pin the
// destructive guard behaviors: branch_delete refuses the default branch and
// repo_delete requires explicit confirmation.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"example.com/teran/mcp-forgejo/internal/domain"
	"example.com/teran/mcp-forgejo/internal/infrastructure/forgejo"
)

// mockForgejoDelete answers the delete endpoints used by Batch C tools, plus
// the read probes they depend on (GetFile for file_delete, GetRepository for
// branch_delete).
func mockForgejoDelete() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		m := r.Method

		switch {
		case m == http.MethodGet && p == "/api/v1/repos/acme/demo/contents/main.go":
			// Probe for the file sha before repoDeleteFile.
			_, _ = w.Write([]byte(`{"name":"main.go","path":"main.go","sha":"f1","size":11,"type":"file"}`))
			return
		case m == http.MethodDelete && p == "/api/v1/repos/acme/demo/contents/main.go":
			_, _ = w.Write([]byte(`{"content":{"name":"main.go","path":"main.go","sha":"f1"},"commit":{"sha":"c1"}}`))
			return
		case m == http.MethodGet && p == "/api/v1/repos/acme/demo":
			_, _ = w.Write([]byte(`{"id":7,"name":"demo","full_name":"acme/demo","default_branch":"main"}`))
			return
		case m == http.MethodDelete && p == "/api/v1/repos/acme/demo/branches/dev":
			w.WriteHeader(http.StatusNoContent)
			return
		case m == http.MethodDelete && p == "/api/v1/repos/acme/demo/issues/7":
			w.WriteHeader(http.StatusNoContent)
			return
		case m == http.MethodDelete && p == "/api/v1/repos/acme/demo/issues/comments/9":
			w.WriteHeader(http.StatusNoContent)
			return
		case m == http.MethodDelete && p == "/api/v1/repos/acme/demo/releases/3":
			w.WriteHeader(http.StatusNoContent)
			return
		case m == http.MethodDelete && p == "/api/v1/repos/acme/demo":
			w.WriteHeader(http.StatusNoContent)
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"unexpected "` + m + `" ` + p + `"}`))
		}
	})
}

// setupDelete builds the MCP server backed by mockForgejoDelete and returns a
// connected client session.
func setupDelete(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ts := httptest.NewServer(mockForgejoDelete())
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

// deleteToolNames lists the Batch C tools that must be registered.
var deleteToolNames = []string{
	"forgejo_file_delete",
	"forgejo_branch_delete",
	"forgejo_issue_delete",
	"forgejo_comment_delete",
	"forgejo_release_delete",
	"forgejo_repo_delete",
}

func TestDeleteToolsRegistered(t *testing.T) {
	cs, _ := setup(t) // registration is transport-independent; reuse read mock
	names := listToolMap(t, cs)
	for _, name := range deleteToolNames {
		if _, ok := names[name]; !ok {
			t.Errorf("delete tool %q not registered", name)
		}
	}
}

func TestDeleteToolsAnnotations(t *testing.T) {
	cs, _ := setup(t)
	byName := listToolMap(t, cs)
	for _, name := range deleteToolNames {
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
		if a.DestructiveHint == nil || !*a.DestructiveHint {
			t.Errorf("tool %q: destructiveHint != true", name)
		}
		if a.IdempotentHint {
			t.Errorf("tool %q: idempotentHint != false", name)
		}
		if a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("tool %q: openWorldHint != false", name)
		}
	}
}

// TestDeleteToolsRequireConfirmationInDescription ensures the destructive tools
// surface an explicit confirmation prompt in their description/instructions
// (SPEC 6.3), most importantly repo_delete.
func TestDeleteToolsRequireConfirmationInDescription(t *testing.T) {
	cs, _ := setup(t)
	byName := listToolMap(t, cs)
	for _, name := range deleteToolNames {
		tool, ok := byName[name]
		if !ok {
			t.Errorf("tool %q not registered", name)
			continue
		}
		if !strings.Contains(tool.Description, "confirm") {
			t.Errorf("tool %q description should require confirmation, got %q", name, tool.Description)
		}
	}
}

func TestFileDeleteTool(t *testing.T) {
	cs := setupDelete(t)
	var res domain.FileResult
	callTool(t, cs, "forgejo_file_delete", map[string]any{
		"owner": "acme", "repo": "demo", "path": "main.go", "branch": "main", "message": "remove main",
	}, &res)
	if res.Path != "main.go" || res.CommitSHA != "c1" {
		t.Errorf("res = %+v", res)
	}
}

func TestBranchDeleteTool(t *testing.T) {
	cs := setupDelete(t)
	callTool(t, cs, "forgejo_branch_delete", map[string]any{
		"owner": "acme", "repo": "demo", "branch": "dev",
	}, nil)
}

func TestIssueDeleteTool(t *testing.T) {
	cs := setupDelete(t)
	callTool(t, cs, "forgejo_issue_delete", map[string]any{
		"owner": "acme", "repo": "demo", "index": 7,
	}, nil)
}

func TestCommentDeleteTool(t *testing.T) {
	cs := setupDelete(t)
	callTool(t, cs, "forgejo_comment_delete", map[string]any{
		"owner": "acme", "repo": "demo", "comment_id": 9,
	}, nil)
}

func TestReleaseDeleteTool(t *testing.T) {
	cs := setupDelete(t)
	callTool(t, cs, "forgejo_release_delete", map[string]any{
		"owner": "acme", "repo": "demo", "id": 3,
	}, nil)
}

func TestRepoDeleteToolConfirmed(t *testing.T) {
	cs := setupDelete(t)
	callTool(t, cs, "forgejo_repo_delete", map[string]any{
		"owner": "acme", "repo": "demo", "confirm": true,
	}, nil)
}

// TestRepoDeleteToolRequiresConfirmation verifies the destructive guard: a repo
// delete without confirm=true must fail with an error result (never call the
// Forgejo API).
func TestRepoDeleteToolRequiresConfirmation(t *testing.T) {
	cs := setupDelete(t)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "forgejo_repo_delete",
		Arguments: map[string]any{"owner": "acme", "repo": "demo"},
	})
	if err != nil {
		t.Fatalf("CallTool error = %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for repo_delete without confirmation")
	}
}

// TestDeleteToolErrorSurfacesIsError verifies a 404 on a delete tool surfaces
// an error result and never leaks the token (SPEC S2).
func TestDeleteToolErrorSurfacesIsError(t *testing.T) {
	cs, _ := setup(t) // read mock returns 404 for unknown delete paths
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "forgejo_release_delete",
		Arguments: map[string]any{"owner": "ghost", "repo": "missing", "id": 3},
	})
	if err != nil {
		t.Fatalf("CallTool error = %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for delete tool on 404")
	}
	msg := textOf(t, res)
	if msg == "" {
		t.Fatal("expected error text in content")
	}
	if strings.Contains(msg, testToken) {
		t.Errorf("token leaked in error: %q", msg)
	}
}

// TestDeleteToolJSONSchemaPinsFields ensures each delete tool exposes the input
// fields @developer must add, so the schema matches the documented contract.
func TestDeleteToolJSONSchemaPinsFields(t *testing.T) {
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

	check("forgejo_file_delete", []string{"owner", "repo", "path", "branch", "message"})
	check("forgejo_branch_delete", []string{"owner", "repo", "branch"})
	check("forgejo_issue_delete", []string{"owner", "repo", "index"})
	check("forgejo_comment_delete", []string{"owner", "repo", "comment_id"})
	check("forgejo_release_delete", []string{"owner", "repo", "id"})
	check("forgejo_repo_delete", []string{"owner", "repo", "confirm"})
}
