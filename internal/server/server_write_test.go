package server

// Batch B write/update tools (SPEC 6.2 #14–#25). These tests verify the tools
// are registered, carry the correct annotations (readOnlyHint=false,
// destructiveHint=false, idempotentHint by meaning) and are invocable end to
// end against a mock Forgejo. The input field names below are the JSON schema
// fields @developer must expose on the server input structs.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"example.com/teran/mcp-forgejo/internal/domain"
	"example.com/teran/mcp-forgejo/internal/infrastructure/forgejo"
)

// mockForgejoWrite answers the write endpoints used by Batch B tools. Routing
// is method+path aware because e.g. GET contents (probe) and POST contents
// (create/change-files) hit the same path.
func mockForgejoWrite() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		m := r.Method

		switch {
		case m == http.MethodPost && p == "/api/v1/user/repos":
			_, _ = w.Write([]byte(`{"id":1,"name":"demo","full_name":"alice/demo","private":true,"default_branch":"main"}`))
			return
		case m == http.MethodGet && p == "/api/v1/repos/acme/demo/contents/main.go":
			// Probe: file absent -> WriteFile will create.
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
			return
		case m == http.MethodPost && p == "/api/v1/repos/acme/demo/contents/main.go":
			content := base64.StdEncoding.EncodeToString([]byte("hello"))
			_, _ = w.Write([]byte(`{"content":{"name":"main.go","path":"main.go","sha":"f1","encoding":"base64","content":"` + content + `","size":5},"commit":{"sha":"c1"}}`))
			return
		case m == http.MethodPost && p == "/api/v1/repos/acme/demo/contents":
			_, _ = w.Write([]byte(`{"commit":{"sha":"c9"},"files":[{"path":"a.txt","sha":"fa","status":"created"},{"path":"b.txt","sha":"fb","status":"created"}]}`))
			return
		case m == http.MethodPost && p == "/api/v1/repos/acme/demo/branches":
			_, _ = w.Write([]byte(`{"name":"dev","commit":{"id":"c1"}}`))
			return
		case m == http.MethodPost && p == "/api/v1/repos/acme/demo/issues":
			_, _ = w.Write([]byte(`{"id":1,"number":7,"title":"Bug","body":"detail","state":"open"}`))
			return
		case m == http.MethodPatch && p == "/api/v1/repos/acme/demo/issues/7":
			_, _ = w.Write([]byte(`{"id":1,"number":7,"title":"Bug v2","body":"new","state":"closed"}`))
			return
		case m == http.MethodPost && p == "/api/v1/repos/acme/demo/issues/7/comments":
			_, _ = w.Write([]byte(`{"id":9,"body":"me too","created_at":"2024-01-01"}`))
			return
		case m == http.MethodPost && p == "/api/v1/repos/acme/demo/pulls":
			_, _ = w.Write([]byte(`{"id":1,"number":5,"title":"PR","body":"b","state":"open"}`))
			return
		case m == http.MethodPatch && p == "/api/v1/repos/acme/demo/pulls/5":
			_, _ = w.Write([]byte(`{"id":1,"number":5,"title":"PR v2","body":"new","state":"closed"}`))
			return
		case m == http.MethodGet && p == "/api/v1/repos/acme/demo/pulls/5/merge":
			// Not merged -> MergePullRequest proceeds to merge.
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not merged"}`))
			return
		case m == http.MethodPost && p == "/api/v1/repos/acme/demo/pulls/5/merge":
			w.WriteHeader(http.StatusOK)
			return
		case m == http.MethodPost && p == "/api/v1/repos/acme/demo/pulls/5/reviews":
			_, _ = w.Write([]byte(`{"id":11,"body":"looks good","state":"PENDING"}`))
			return
		case m == http.MethodPost && strings.HasSuffix(p, "/reviews/11"):
			_, _ = w.Write([]byte(`{"id":11,"body":"looks good","state":"APPROVED"}`))
			return
		case m == http.MethodPost && p == "/api/v1/repos/acme/demo/releases":
			_, _ = w.Write([]byte(`{"id":3,"tag_name":"v1.0","name":"V1.0","body":"notes","draft":false,"prerelease":false}`))
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"unexpected "` + m + `" ` + p + `"}`))
		}
	})
}

// setupWrite builds the MCP server backed by mockForgejoWrite and returns a
// connected client session.
func setupWrite(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ts := httptest.NewServer(mockForgejoWrite())
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

func listToolMap(t *testing.T, cs *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools error = %v", err)
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	return byName
}

// writeToolIdempotent maps each Batch B tool to its expected idempotentHint
// (SPEC 6.2): true only for issue_update and pull_update.
var writeToolIdempotent = map[string]bool{
	"forgejo_repo_create":       false,
	"forgejo_file_write":        false,
	"forgejo_file_write_many":   false,
	"forgejo_branch_create":     false,
	"forgejo_issue_create":      false,
	"forgejo_issue_update":      true,
	"forgejo_issue_comment_add": false,
	"forgejo_pull_create":       false,
	"forgejo_pull_update":       true,
	"forgejo_pull_merge":        false,
	"forgejo_pull_review":       false,
	"forgejo_release_create":    false,
}

func TestWriteToolsRegistered(t *testing.T) {
	cs, _ := setup(t) // registration is transport-independent; reuse read mock
	names := listToolMap(t, cs)
	for name := range writeToolIdempotent {
		if _, ok := names[name]; !ok {
			t.Errorf("write tool %q not registered", name)
		}
	}
}

func TestWriteToolsAnnotations(t *testing.T) {
	cs, _ := setup(t)
	byName := listToolMap(t, cs)
	for name, wantIdem := range writeToolIdempotent {
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

func TestRepoCreateTool(t *testing.T) {
	cs := setupWrite(t)
	var repo domain.Repository
	callTool(t, cs, "forgejo_repo_create", map[string]any{"name": "demo", "private": true, "auto_init": true}, &repo)
	if repo.FullName != "alice/demo" || !repo.Private {
		t.Errorf("repo = %+v", repo)
	}
}

func TestFileWriteToolCreates(t *testing.T) {
	cs := setupWrite(t)
	var res domain.FileResult
	callTool(t, cs, "forgejo_file_write", map[string]any{
		"owner": "acme", "repo": "demo", "path": "main.go", "branch": "main", "message": "add", "content": "hello",
	}, &res)
	if res.SHA != "f1" || res.Content != "hello" || res.CommitSHA != "c1" {
		t.Errorf("res = %+v", res)
	}
}

func TestFileWriteManyTool(t *testing.T) {
	cs := setupWrite(t)
	var res domain.ChangeFilesResult
	callTool(t, cs, "forgejo_file_write_many", map[string]any{
		"owner": "acme", "repo": "demo", "branch": "main", "message": "add many",
		"files": []map[string]any{
			{"path": "a.txt", "content": "aaa", "operation": "create"},
			{"path": "b.txt", "content": "bbb", "operation": "create"},
		},
	}, &res)
	if res.CommitSHA != "c9" || len(res.Files) != 2 || res.Files[0].Path != "a.txt" {
		t.Errorf("res = %+v", res)
	}
}

func TestBranchCreateTool(t *testing.T) {
	cs := setupWrite(t)
	var branch domain.Branch
	callTool(t, cs, "forgejo_branch_create", map[string]any{"owner": "acme", "repo": "demo", "new_branch": "dev", "old_ref": "main"}, &branch)
	if branch.Name != "dev" || branch.CommitSHA != "c1" {
		t.Errorf("branch = %+v", branch)
	}
}

func TestIssueCreateTool(t *testing.T) {
	cs := setupWrite(t)
	var issue domain.Issue
	callTool(t, cs, "forgejo_issue_create", map[string]any{
		"owner": "acme", "repo": "demo", "title": "Bug", "body": "detail", "labels": []any{1, 2}, "milestone": 3,
	}, &issue)
	if issue.Number != 7 || issue.Title != "Bug" {
		t.Errorf("issue = %+v", issue)
	}
}

func TestIssueUpdateTool(t *testing.T) {
	cs := setupWrite(t)
	var issue domain.Issue
	callTool(t, cs, "forgejo_issue_update", map[string]any{
		"owner": "acme", "repo": "demo", "index": 7, "title": "Bug v2", "body": "new", "state": "closed",
	}, &issue)
	if issue.State != "closed" || issue.Title != "Bug v2" {
		t.Errorf("issue = %+v", issue)
	}
}

func TestIssueCommentAddTool(t *testing.T) {
	cs := setupWrite(t)
	var comment domain.Comment
	callTool(t, cs, "forgejo_issue_comment_add", map[string]any{"owner": "acme", "repo": "demo", "index": 7, "body": "me too"}, &comment)
	if comment.ID != 9 || comment.Body != "me too" {
		t.Errorf("comment = %+v", comment)
	}
}

func TestPullCreateTool(t *testing.T) {
	cs := setupWrite(t)
	var pr domain.PullRequest
	callTool(t, cs, "forgejo_pull_create", map[string]any{
		"owner": "acme", "repo": "demo", "head": "dev", "base": "main", "title": "PR", "body": "b",
	}, &pr)
	if pr.Number != 5 || pr.Title != "PR" {
		t.Errorf("pr = %+v", pr)
	}
}

func TestPullUpdateTool(t *testing.T) {
	cs := setupWrite(t)
	var pr domain.PullRequest
	callTool(t, cs, "forgejo_pull_update", map[string]any{
		"owner": "acme", "repo": "demo", "index": 5, "title": "PR v2", "body": "new", "state": "closed",
	}, &pr)
	if pr.State != "closed" || pr.Title != "PR v2" {
		t.Errorf("pr = %+v", pr)
	}
}

func TestPullMergeTool(t *testing.T) {
	cs := setupWrite(t)
	var res domain.PullMergeResult
	callTool(t, cs, "forgejo_pull_merge", map[string]any{"owner": "acme", "repo": "demo", "index": 5, "method": "squash"}, &res)
	if !res.Merged || res.AlreadyMerged {
		t.Errorf("res = %+v", res)
	}
}

func TestPullReviewTool(t *testing.T) {
	cs := setupWrite(t)
	var review domain.Review
	callTool(t, cs, "forgejo_pull_review", map[string]any{
		"owner": "acme", "repo": "demo", "index": 5, "body": "looks good", "event": "APPROVED",
	}, &review)
	if review.State != "APPROVED" {
		t.Errorf("review = %+v", review)
	}
}

func TestReleaseCreateTool(t *testing.T) {
	cs := setupWrite(t)
	var release domain.Release
	callTool(t, cs, "forgejo_release_create", map[string]any{
		"owner": "acme", "repo": "demo", "tag": "v1.0", "name": "V1.0", "notes": "notes",
	}, &release)
	if release.TagName != "v1.0" || release.Name != "V1.0" {
		t.Errorf("release = %+v", release)
	}
}

// TestWriteToolErrorSurfacesIsError verifies a 404 on a write tool surfaces an
// error result and never leaks the token (SPEC S2).
func TestWriteToolErrorSurfacesIsError(t *testing.T) {
	cs, _ := setup(t) // read mock returns 404 for unknown write paths
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "forgejo_release_create",
		Arguments: map[string]any{"owner": "ghost", "repo": "missing", "tag": "v1"},
	})
	if err != nil {
		t.Fatalf("CallTool error = %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for write tool on 404")
	}
	msg := textOf(t, res)
	if msg == "" {
		t.Fatal("expected error text in content")
	}
	if strings.Contains(msg, testToken) {
		t.Errorf("token leaked in error: %q", msg)
	}
}

// TestWriteToolJSONSchemaPinsFields ensures each write tool exposes the input
// fields @developer must add, so the schema matches the documented contract.
func TestWriteToolJSONSchemaPinsFields(t *testing.T) {
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

	check("forgejo_repo_create", []string{"name", "private", "auto_init", "owner"})
	check("forgejo_file_write", []string{"owner", "repo", "path", "message", "content", "branch"})
	check("forgejo_branch_create", []string{"new_branch", "old_ref"})
	check("forgejo_issue_create", []string{"title", "body", "labels", "milestone"})
	check("forgejo_issue_update", []string{"index", "state", "title"})
	check("forgejo_issue_comment_add", []string{"index", "body"})
	check("forgejo_pull_create", []string{"head", "base", "title"})
	check("forgejo_pull_update", []string{"index", "state"})
	check("forgejo_pull_merge", []string{"index", "method"})
	check("forgejo_pull_review", []string{"index", "event"})
	check("forgejo_release_create", []string{"tag", "name", "notes"})
}
