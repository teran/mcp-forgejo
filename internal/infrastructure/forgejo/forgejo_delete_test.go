package forgejo

// Batch C delete tools (SPEC 6.3 #26–#31). Test expectations for @developer:
// endpoints, HTTP methods, request bodies, wire JSON and the domain
// types/methods they must add. Read the header of forgejo_test.go for the
// shared helpers (newTestServer, testToken) and forgejo_write_test.go for
// decodeBody, which these tests build on.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"example.com/teran/mcp-forgejo/internal/domain"
)

// =============================================================================
// #26 forgejo_file_delete — repoDeleteFile
// Forgejo: DELETE /repos/{owner}/{repo}/contents/{filepath}
//          body deleteFileOptions {sha (required), branch, message}
//          response 200 FileDeleteResponse {content, commit}
// =============================================================================

func TestDeleteFile(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"content":{"name":"main.go","path":"main.go","sha":"f1"},"commit":{"sha":"c1"}}`))
	})
	c, _ := newTestServer(t, handler)

	res, err := c.DeleteFile(context.Background(), domain.DeleteFileInput{
		Owner: "acme", Repo: "demo", Path: "main.go", Branch: "main", Message: "remove", SHA: "f1",
	})
	if err != nil {
		t.Fatalf("DeleteFile() error = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/contents/main.go" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/contents/main.go", gotPath)
	}
	if gotBody["sha"] != "f1" || gotBody["message"] != "remove" || gotBody["branch"] != "main" {
		t.Errorf("body = %+v, want sha/message/branch", gotBody)
	}
	if res.Path != "main.go" || res.SHA != "f1" || res.CommitSHA != "c1" {
		t.Errorf("res = %+v", res)
	}
}

func TestDeleteFileNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"no such file"}`))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.DeleteFile(context.Background(), domain.DeleteFileInput{Owner: "o", Repo: "r", Path: "x", SHA: "f1"})
	if err == nil {
		t.Fatal("expected error for 404")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindNotFound {
		t.Errorf("expected not_found, got %v", err)
	}
}

func TestDeleteFileTransientRedactsToken(t *testing.T) {
	body := `{"message":"boom ` + testToken + `"}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(body))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.DeleteFile(context.Background(), domain.DeleteFileInput{Owner: "o", Repo: "r", Path: "x", SHA: "f1"})
	if err == nil {
		t.Fatal("expected error for 5xx")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindTransient {
		t.Errorf("expected transient, got %v", err)
	}
	if strings.Contains(err.Error(), testToken) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("token not redacted: %q", err.Error())
	}
}

// =============================================================================
// #27 forgejo_branch_delete — repoDeleteBranch
// Forgejo: DELETE /repos/{owner}/{repo}/branches/{branch} → 204
// =============================================================================

func TestDeleteBranch(t *testing.T) {
	var gotMethod, gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	c, _ := newTestServer(t, handler)

	if err := c.DeleteBranch(context.Background(), "acme", "demo", "dev"); err != nil {
		t.Fatalf("DeleteBranch() error = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/branches/dev" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/branches/dev", gotPath)
	}
}

func TestDeleteBranchNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"branch not found"}`))
	})
	c, _ := newTestServer(t, handler)
	err := c.DeleteBranch(context.Background(), "acme", "demo", "dev")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindNotFound {
		t.Errorf("expected not_found, got %v", err)
	}
}

func TestDeleteBranchDefaultBranchRejected(t *testing.T) {
	// Forgejo rejects deleting the default branch with 403; the client must
	// surface it as forbidden, not swallow it.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"branch is the default branch"}`))
	})
	c, _ := newTestServer(t, handler)
	err := c.DeleteBranch(context.Background(), "acme", "demo", "main")
	if err == nil {
		t.Fatal("expected error for 403")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindForbidden {
		t.Errorf("expected forbidden, got %v", err)
	}
}

// =============================================================================
// #28 forgejo_issue_delete — issueDelete
// Forgejo: DELETE /repos/{owner}/{repo}/issues/{index} → 204
// =============================================================================

func TestDeleteIssue(t *testing.T) {
	var gotMethod, gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	c, _ := newTestServer(t, handler)

	if err := c.DeleteIssue(context.Background(), "acme", "demo", 7); err != nil {
		t.Fatalf("DeleteIssue() error = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/issues/7" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/issues/7", gotPath)
	}
}

func TestDeleteIssueNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"issue not found"}`))
	})
	c, _ := newTestServer(t, handler)
	err := c.DeleteIssue(context.Background(), "acme", "demo", 999)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindNotFound {
		t.Errorf("expected not_found, got %v", err)
	}
}

// =============================================================================
// #29 forgejo_comment_delete — issueDeleteComment
// Forgejo: DELETE /repos/{owner}/{repo}/issues/comments/{id} → 204
// =============================================================================

func TestDeleteComment(t *testing.T) {
	var gotMethod, gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	c, _ := newTestServer(t, handler)

	if err := c.DeleteComment(context.Background(), "acme", "demo", 9); err != nil {
		t.Fatalf("DeleteComment() error = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/issues/comments/9" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/issues/comments/9", gotPath)
	}
}

func TestDeleteCommentNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"comment not found"}`))
	})
	c, _ := newTestServer(t, handler)
	err := c.DeleteComment(context.Background(), "acme", "demo", 999)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindNotFound {
		t.Errorf("expected not_found, got %v", err)
	}
}

// =============================================================================
// #30 forgejo_release_delete — repoDeleteRelease
// Forgejo: DELETE /repos/{owner}/{repo}/releases/{id} → 204 (tag remains)
// =============================================================================

func TestDeleteRelease(t *testing.T) {
	var gotMethod, gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	c, _ := newTestServer(t, handler)

	if err := c.DeleteRelease(context.Background(), "acme", "demo", 3); err != nil {
		t.Fatalf("DeleteRelease() error = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/releases/3" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/releases/3", gotPath)
	}
}

func TestDeleteReleaseNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"release not found"}`))
	})
	c, _ := newTestServer(t, handler)
	err := c.DeleteRelease(context.Background(), "acme", "demo", 999)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindNotFound {
		t.Errorf("expected not_found, got %v", err)
	}
}

// =============================================================================
// #31 forgejo_repo_delete — repoDelete
// Forgejo: DELETE /repos/{owner}/{repo} → 204 (permanent)
// =============================================================================

func TestDeleteRepository(t *testing.T) {
	var gotMethod, gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	c, _ := newTestServer(t, handler)

	if err := c.DeleteRepository(context.Background(), "acme", "demo"); err != nil {
		t.Fatalf("DeleteRepository() error = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo", gotPath)
	}
}

func TestDeleteRepositoryNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"repo not found"}`))
	})
	c, _ := newTestServer(t, handler)
	err := c.DeleteRepository(context.Background(), "acme", "demo")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindNotFound {
		t.Errorf("expected not_found, got %v", err)
	}
}

func TestDeleteRepositoryTransientRedactsToken(t *testing.T) {
	body := `{"message":"boom ` + testToken + `"}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(body))
	})
	c, _ := newTestServer(t, handler)
	err := c.DeleteRepository(context.Background(), "acme", "demo")
	if err == nil {
		t.Fatal("expected error for 5xx")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindTransient {
		t.Errorf("expected transient, got %v", err)
	}
	if strings.Contains(err.Error(), testToken) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("token not redacted: %q", err.Error())
	}
}
