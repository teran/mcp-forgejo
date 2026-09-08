package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"git.homelab.teran.dev/teran/mcp-forgejo/internal/domain"
	"git.homelab.teran.dev/teran/mcp-forgejo/internal/infrastructure/forgejo"
)

const testToken = "srv-secret-pat"

// mockForgejo returns a handler serving canned Forgejo responses keyed by path.
func mockForgejo() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/acme/demo":
			_, _ = w.Write([]byte(`{"id":7,"name":"demo","full_name":"acme/demo","private":true,"default_branch":"master","description":"a repo"}`))
		case "/api/v1/repos/acme/demo/contents":
			_, _ = w.Write([]byte(`[{"name":"src","type":"dir","size":0,"sha":"d1"},{"name":"main.go","type":"file","size":13,"sha":"f1"}]`))
		case "/api/v1/repos/acme/demo/contents/main.go":
			content := base64.StdEncoding.EncodeToString([]byte("package main"))
			_, _ = w.Write([]byte(`{"name":"main.go","sha":"sha1","size":11,"type":"file","encoding":"base64","content":"` + content + `"}`))
		case "/api/v1/repos/acme/demo/issues/5":
			_, _ = w.Write([]byte(`{"id":1,"number":5,"title":"Fix bug","body":"detail","state":"open","comments":1}`))
		case "/api/v1/repos/acme/demo/issues/5/comments":
			_, _ = w.Write([]byte(`[{"id":2,"body":"first comment"}]`))
		case "/api/v1/user/orgs":
			_, _ = w.Write([]byte(`[{"id":1,"username":"acme","full_name":"ACME"}]`))
		case "/api/v1/repos/ghost/missing":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"unexpected path "` + r.URL.Path + `"}`))
		}
	})
}

// setup builds the mcp server backed by a mock Forgejo and returns a connected
// client session.
func setup(t *testing.T) (*mcp.ClientSession, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(mockForgejo())
	t.Cleanup(ts.Close)

	s, err := Build(forgejo.Config{BaseURL: ts.URL, Token: testToken}, ts.Client())
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
	return cs, ts
}

// callTool calls a tool and decodes the JSON text content into out.
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) error = %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool(%s) returned error: %v", name, textOf(t, res))
	}
	if out != nil && len(res.Content) > 0 {
		if tc, ok := res.Content[0].(*mcp.TextContent); ok {
			if err := json.Unmarshal([]byte(tc.Text), out); err != nil {
				t.Fatalf("decode result of %s: %v (text=%s)", name, err, tc.Text)
			}
		} else {
			t.Fatalf("expected TextContent for %s, got %T", name, res.Content[0])
		}
	}
	return res
}

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

func TestListTools(t *testing.T) {
	cs, _ := setup(t)
	res, err := cs.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools error = %v", err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	want := []string{"forgejo_repo_get", "forgejo_repo_list_contents", "forgejo_file_get", "forgejo_org_list", "forgejo_issue_get"}
	for _, n := range want {
		if !names[n] {
			t.Errorf("tool %q not registered", n)
		}
	}
}

func TestReadToolsAnnotations(t *testing.T) {
	cs, _ := setup(t)
	res, err := cs.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools error = %v", err)
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	readTools := []string{"forgejo_repo_get", "forgejo_repo_list_contents", "forgejo_file_get", "forgejo_org_list", "forgejo_issue_get"}
	for _, n := range readTools {
		tool, ok := byName[n]
		if !ok {
			t.Errorf("tool %q not registered", n)
			continue
		}
		a := tool.Annotations
		if a == nil {
			t.Errorf("tool %q: annotations missing", n)
			continue
		}
		if a.Title == "" {
			t.Errorf("tool %q: annotations.title empty", n)
		}
		if !a.ReadOnlyHint {
			t.Errorf("tool %q: readOnlyHint != true", n)
		}
		if !a.IdempotentHint {
			t.Errorf("tool %q: idempotentHint != true", n)
		}
		if a.DestructiveHint == nil || *a.DestructiveHint {
			t.Errorf("tool %q: destructiveHint != false", n)
		}
		if a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("tool %q: openWorldHint != false", n)
		}
	}
}

func TestRepoGetTool(t *testing.T) {
	cs, _ := setup(t)
	var repo domain.Repository
	callTool(t, cs, "forgejo_repo_get", map[string]any{"owner": "acme", "repo": "demo"}, &repo)
	if repo.FullName != "acme/demo" || !repo.Private || repo.DefaultBranch != "master" {
		t.Errorf("repo = %+v", repo)
	}
}

func TestRepoListContentsTool(t *testing.T) {
	cs, _ := setup(t)
	var entries []domain.FileEntry
	callTool(t, cs, "forgejo_repo_list_contents", map[string]any{"owner": "acme", "repo": "demo", "path": ""}, &entries)
	if len(entries) != 2 || entries[0].Name != "src" || entries[1].Type != "file" {
		t.Errorf("entries = %+v", entries)
	}
}

func TestFileGetTool(t *testing.T) {
	cs, _ := setup(t)
	var f domain.File
	callTool(t, cs, "forgejo_file_get", map[string]any{"owner": "acme", "repo": "demo", "path": "main.go"}, &f)
	if f.Binary || f.Content != "package main" || f.SHA != "sha1" {
		t.Errorf("file = %+v", f)
	}
}

func TestOrgListTool(t *testing.T) {
	cs, _ := setup(t)
	var orgs []domain.Organization
	callTool(t, cs, "forgejo_org_list", map[string]any{}, &orgs)
	if len(orgs) != 1 || orgs[0].Username != "acme" {
		t.Errorf("orgs = %+v", orgs)
	}
}

func TestIssueGetTool(t *testing.T) {
	cs, _ := setup(t)
	var got domain.IssueWithComments
	callTool(t, cs, "forgejo_issue_get", map[string]any{"owner": "acme", "repo": "demo", "index": 5}, &got)
	if got.Issue.Number != 5 || got.Issue.Title != "Fix bug" {
		t.Errorf("issue = %+v", got.Issue)
	}
	if len(got.Comments) != 1 || got.Comments[0].Body != "first comment" {
		t.Errorf("comments = %+v", got.Comments)
	}
}

func TestToolErrorSurfacesIsError(t *testing.T) {
	cs, _ := setup(t)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "forgejo_repo_get",
		Arguments: map[string]any{"owner": "ghost", "repo": "missing"},
	})
	if err != nil {
		t.Fatalf("CallTool error = %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for 404")
	}
	msg := textOf(t, res)
	if msg == "" {
		t.Fatal("expected error text in content")
	}
	if contains(msg, testToken) {
		t.Errorf("token leaked in error: %q", msg)
	}
}

func TestHTTPHandlerNonNil(t *testing.T) {
	s, err := Build(forgejo.Config{BaseURL: "https://x", Token: "t"}, http.DefaultClient)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if NewHTTPHandler(s) == nil {
		t.Error("NewHTTPHandler returned nil")
	}
}

func TestBuildWithNilHTTPClient(t *testing.T) {
	if _, err := Build(forgejo.Config{BaseURL: "https://x", Token: "t"}, nil); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
