package server

// forgejo_repo_migrate (SPEC 6.2, repoMigrate): migrates a remote git
// repository into Forgejo. It is a WRITE/UPDATE tool that is NOT idempotent
// (readOnlyHint=false, destructiveHint=false, idempotentHint=false). These
// tests verify the tool is registered, carries the correct annotations, exposes
// the right JSON-schema fields and is invocable end to end against a mock
// Forgejo. The input field names below are the JSON-schema fields @developer
// must expose on the server input struct (repoMigrateIn).

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

// mockForgejoMigrate answers the migrate endpoint. It records the source PAT so
// the test can assert it never leaks into the tool result.
func mockForgejoMigrate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/migrate" {
			_, _ = w.Write([]byte(`{"id":12,"name":"migrated","full_name":"alice/migrated","private":true,"default_branch":"main"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"unexpected "` + r.Method + `" ` + r.URL.Path + `"}`))
	})
}

// setupRepoMigrate builds the MCP server backed by mockForgejoMigrate and
// returns a connected client session.
func setupRepoMigrate(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ts := httptest.NewServer(mockForgejoMigrate())
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

func TestRepoMigrateToolRegistered(t *testing.T) {
	cs, _ := setup(t) // registration is transport-independent
	names := listToolMap(t, cs)
	if _, ok := names["forgejo_repo_migrate"]; !ok {
		t.Errorf("write tool %q not registered", "forgejo_repo_migrate")
	}
}

func TestRepoMigrateAnnotations(t *testing.T) {
	cs, _ := setup(t)
	byName := listToolMap(t, cs)
	tool, ok := byName["forgejo_repo_migrate"]
	if !ok {
		t.Fatalf("tool %q not registered", "forgejo_repo_migrate")
	}
	a := tool.Annotations
	if a == nil {
		t.Fatal("annotations missing")
	}
	if a.Title == "" {
		t.Errorf("annotations.title empty")
	}
	// Write/update tool: readOnlyHint=false, destructiveHint=false,
	// openWorldHint=false, idempotentHint=false (migrate is not idempotent).
	if a.ReadOnlyHint {
		t.Errorf("repo_migrate: readOnlyHint != false")
	}
	if a.DestructiveHint == nil || *a.DestructiveHint {
		t.Errorf("repo_migrate: destructiveHint != false")
	}
	if a.IdempotentHint {
		t.Errorf("repo_migrate: idempotentHint != false")
	}
	if a.OpenWorldHint == nil || *a.OpenWorldHint {
		t.Errorf("repo_migrate: openWorldHint != false")
	}
}

func TestRepoMigrateTool(t *testing.T) {
	cs := setupRepoMigrate(t)
	var repo domain.Repository
	callTool(t, cs, "forgejo_repo_migrate", map[string]any{
		"clone_addr": "https://github.com/acme/upstream.git",
		"repo_name":  "migrated",
		"repo_owner": "alice",
		"service":    "github",
		"private":    true,
	}, &repo)
	if repo.Name != "migrated" || repo.FullName != "alice/migrated" || !repo.Private {
		t.Errorf("repo = %+v", repo)
	}
}

// The success result is a Repository; the source auth_token must never appear
// in the tool output (S02).
func TestRepoMigrateAuthTokenNotInResult(t *testing.T) {
	cs := setupRepoMigrate(t)
	res := callTool(t, cs, "forgejo_repo_migrate", map[string]any{
		"clone_addr":    "https://github.com/acme/upstream.git",
		"repo_name":     "migrated",
		"auth_token":    "ghp_srv_secret",
		"auth_username": "alice",
	}, &domain.Repository{})
	if strings.Contains(textOf(t, res), "ghp_srv_secret") {
		t.Errorf("auth_token leaked in tool result: %q", textOf(t, res))
	}
}

func TestRepoMigrateToolErrorSurfacesIsError(t *testing.T) {
	cs, _ := setup(t) // read mock returns 404 for the migrate path
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "forgejo_repo_migrate",
		Arguments: map[string]any{
			"clone_addr": "https://github.com/acme/upstream.git",
			"repo_name":  "migrated",
			"auth_token": "ghp_srv_secret",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(repo_migrate) error = %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError for 404")
	}
	msg := textOf(t, res)
	if strings.Contains(msg, testToken) || strings.Contains(msg, "ghp_srv_secret") {
		t.Errorf("secret leaked in error: %q", msg)
	}
}

// =============================================================================
// JSON schema field pins
// =============================================================================

func TestRepoMigrateToolJSONSchemaPinsFields(t *testing.T) {
	cs, _ := setup(t)
	byName := listToolMap(t, cs)
	tool, ok := byName["forgejo_repo_migrate"]
	if !ok {
		t.Fatalf("tool %q not registered", "forgejo_repo_migrate")
	}
	schemaJSON, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	for _, f := range []string{
		"clone_addr", "repo_name", "repo_owner", "service", "auth_username", "auth_token",
		"mirror", "mirror_interval", "private", "description", "issues", "labels",
		"milestones", "pull_requests", "releases", "wiki", "lfs", "lfs_endpoint",
	} {
		if !strings.Contains(string(schemaJSON), `"`+f+`"`) {
			t.Errorf("tool %q schema missing field %q (schema=%s)", "forgejo_repo_migrate", f, schemaJSON)
		}
	}
}

// =============================================================================
// log redaction (S02): the auth_token argument must never reach a log
// =============================================================================

// redactArgs matches sensitive argument keys case-insensitively. The migrate
// tool's source PAT arrives under auth_token, which must be added to
// sensitiveArgsKeys so it is redacted before logging.
func TestRedactArgsMigrateAuthToken(t *testing.T) {
	raw := json.RawMessage(`{"clone_addr":"https://github.com/acme/upstream.git","repo_name":"migrated","auth_token":"ghp_log_secret","auth_username":"alice"}`)
	got := redactArgs(raw).(map[string]any)
	if got["auth_token"] != "[REDACTED]" {
		t.Errorf("auth_token not redacted: %v", got["auth_token"])
	}
	if got["clone_addr"] != "https://github.com/acme/upstream.git" || got["repo_name"] != "migrated" {
		t.Errorf("non-sensitive migrate args redacted by mistake: %v", got)
	}
	out, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal redacted args: %v", err)
	}
	if strings.Contains(string(out), "ghp_log_secret") {
		t.Errorf("auth_token leaked in redacted args: %s", out)
	}
}
