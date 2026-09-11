package forgejo

// Batch B write/update tools (SPEC 6.2 #14–#25). Test expectations for
// @developer: endpoints, HTTP methods, request bodies, wire JSON and the
// domain types/methods they must add. Read the header of forgejo_test.go for
// the shared helpers (newTestServer, testToken) these tests build on.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"example.com/teran/mcp-forgejo/internal/domain"
)

// decodeBody reads the request body as JSON into a map for field assertions.
func decodeBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return m
}

// =============================================================================
// #14 forgejo_repo_create — createCurrentUserRepo
// =============================================================================

func TestCreateRepositoryCurrentUser(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":1,"name":"demo","full_name":"alice/demo","private":true,"default_branch":"main"}`))
	})
	c, _ := newTestServer(t, handler)

	repo, err := c.CreateRepository(context.Background(), domain.CreateRepositoryInput{Name: "demo", Private: true, AutoInit: true})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/user/repos" {
		t.Errorf("path = %q, want /api/v1/user/repos", gotPath)
	}
	if gotAuth != "token "+testToken {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotBody["name"] != "demo" || gotBody["private"] != true || gotBody["auto_init"] != true {
		t.Errorf("body = %+v", gotBody)
	}
	if repo.Name != "demo" || !repo.Private {
		t.Errorf("repo = %+v", repo)
	}
}

func TestCreateRepositoryInOrg(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":2,"name":"demo","full_name":"acme/demo"}`))
	})
	c, _ := newTestServer(t, handler)

	_, err := c.CreateRepository(context.Background(), domain.CreateRepositoryInput{Owner: "acme", Name: "demo"})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	if gotPath != "/api/v1/orgs/acme/repos" {
		t.Errorf("path = %q, want /api/v1/orgs/acme/repos", gotPath)
	}
	if _, ok := gotBody["name"]; !ok {
		t.Errorf("body = %+v, want name field", gotBody)
	}
}

func TestCreateRepositoryConflict(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"repository already exists"}`))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.CreateRepository(context.Background(), domain.CreateRepositoryInput{Name: "demo"})
	if err == nil {
		t.Fatal("expected error for 409")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindConflict {
		t.Errorf("expected conflict, got %v", err)
	}
}

func TestCreateRepositoryServerErrorRedactsToken(t *testing.T) {
	body := `{"message":"boom ` + testToken + `"}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(body))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.CreateRepository(context.Background(), domain.CreateRepositoryInput{Name: "demo"})
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
// #15 forgejo_file_write — repoCreateFile / repoUpdateFile
// =============================================================================

func TestCreateFile(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		content := base64.StdEncoding.EncodeToString([]byte("hello"))
		_, _ = w.Write([]byte(`{"content":{"name":"main.go","path":"main.go","sha":"f1","encoding":"base64","content":"` + content + `","size":5},"commit":{"sha":"c1"}}`))
	})
	c, _ := newTestServer(t, handler)

	res, err := c.CreateFile(context.Background(), domain.CreateFileInput{
		Owner: "acme", Repo: "demo", Path: "main.go", Branch: "main", Message: "add main", Content: "hello",
	})
	if err != nil {
		t.Fatalf("CreateFile() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/contents/main.go" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["message"] != "add main" || gotBody["branch"] != "main" {
		t.Errorf("body = %+v", gotBody)
	}
	if gotBody["content"] != base64.StdEncoding.EncodeToString([]byte("hello")) {
		t.Errorf("content not base64 on the wire: %+v", gotBody["content"])
	}
	if res.Path != "main.go" || res.SHA != "f1" || res.Content != "hello" || res.CommitSHA != "c1" {
		t.Errorf("res = %+v", res)
	}
}

func TestUpdateFile(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"content":{"name":"main.go","path":"main.go","sha":"f2"},"commit":{"sha":"c2"}}`))
	})
	c, _ := newTestServer(t, handler)

	res, err := c.UpdateFile(context.Background(), domain.UpdateFileInput{
		Owner: "acme", Repo: "demo", Path: "main.go", Branch: "main", Message: "update", SHA: "f1", Content: "hi",
	})
	if err != nil {
		t.Fatalf("UpdateFile() error = %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/contents/main.go" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["sha"] != "f1" {
		t.Errorf("body = %+v, want sha f1", gotBody)
	}
	if res.SHA != "f2" || res.CommitSHA != "c2" {
		t.Errorf("res = %+v", res)
	}
}

// =============================================================================
// #16 forgejo_file_write_many — repoChangeFiles
// =============================================================================

func TestChangeFiles(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"commit":{"sha":"c9"},"files":[{"path":"a.txt","sha":"fa","status":"created"},{"path":"b.txt","sha":"fb","status":"created"}]}`))
	})
	c, _ := newTestServer(t, handler)

	res, err := c.ChangeFiles(context.Background(), domain.ChangeFilesInput{
		Owner: "acme", Repo: "demo", Branch: "main", Message: "add many",
		Files: []domain.ChangeFileEntry{
			{Path: "a.txt", Content: "aaa", Operation: "create"},
			{Path: "b.txt", Content: "bbb", Operation: "create"},
		},
	})
	if err != nil {
		t.Fatalf("ChangeFiles() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/contents" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/contents", gotPath)
	}
	if gotBody["message"] != "add many" || gotBody["branch"] != "main" {
		t.Errorf("body = %+v", gotBody)
	}
	files, ok := gotBody["files"].([]any)
	if !ok || len(files) != 2 {
		t.Fatalf("body files = %+v", gotBody["files"])
	}
	first, _ := files[0].(map[string]any)
	if first["path"] != "a.txt" || first["operation"] != "create" {
		t.Errorf("file[0] = %+v", first)
	}
	if first["content"] != base64.StdEncoding.EncodeToString([]byte("aaa")) {
		t.Errorf("file[0] content not base64: %+v", first["content"])
	}
	if res.CommitSHA != "c9" || len(res.Files) != 2 || res.Files[0].Path != "a.txt" {
		t.Errorf("res = %+v", res)
	}
}

// =============================================================================
// #17 forgejo_branch_create — repoCreateBranch
// =============================================================================

func TestCreateBranch(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"name":"dev","commit":{"id":"c1"}}`))
	})
	c, _ := newTestServer(t, handler)

	branch, err := c.CreateBranch(context.Background(), "acme", "demo", "dev", "main")
	if err != nil {
		t.Fatalf("CreateBranch() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/branches" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["new_branch_name"] != "dev" || gotBody["old_ref_name"] != "main" {
		t.Errorf("body = %+v", gotBody)
	}
	if branch.Name != "dev" || branch.CommitSHA != "c1" {
		t.Errorf("branch = %+v", branch)
	}
}

func TestCreateBranchConflict(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})
	c, _ := newTestServer(t, handler)
	_, err := c.CreateBranch(context.Background(), "acme", "demo", "main", "main")
	if err == nil {
		t.Fatal("expected error for 409")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindConflict {
		t.Errorf("expected conflict, got %v", err)
	}
}

// =============================================================================
// #18 forgejo_issue_create — issueCreateIssue
// =============================================================================

func TestCreateIssue(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":1,"number":7,"title":"Bug","body":"detail","state":"open"}`))
	})
	c, _ := newTestServer(t, handler)

	issue, err := c.CreateIssue(context.Background(), domain.CreateIssueInput{
		Owner: "acme", Repo: "demo", Title: "Bug", Body: "detail",
		Labels: []int64{1, 2}, Milestone: 3,
	})
	if err != nil {
		t.Fatalf("CreateIssue() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/issues" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["title"] != "Bug" || gotBody["body"] != "detail" || gotBody["milestone"] != float64(3) {
		t.Errorf("body = %+v", gotBody)
	}
	labels, ok := gotBody["labels"].([]any)
	if !ok || len(labels) != 2 || labels[0] != float64(1) || labels[1] != float64(2) {
		t.Errorf("body labels = %+v", gotBody["labels"])
	}
	if issue.Number != 7 || issue.Title != "Bug" {
		t.Errorf("issue = %+v", issue)
	}
}

// =============================================================================
// #19 forgejo_issue_update — issueEditIssue (idempotent)
// =============================================================================

func TestUpdateIssue(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":1,"number":7,"title":"Bug v2","body":"new","state":"closed"}`))
	})
	c, _ := newTestServer(t, handler)

	issue, err := c.UpdateIssue(context.Background(), domain.UpdateIssueInput{
		Owner: "acme", Repo: "demo", Index: 7, Title: "Bug v2", Body: "new", State: "closed",
	})
	if err != nil {
		t.Fatalf("UpdateIssue() error = %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/issues/7" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["title"] != "Bug v2" || gotBody["state"] != "closed" {
		t.Errorf("body = %+v", gotBody)
	}
	if issue.State != "closed" {
		t.Errorf("issue = %+v", issue)
	}
}

// =============================================================================
// #20 forgejo_issue_comment_add — issueCreateComment
// =============================================================================

func TestCreateIssueComment(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":9,"body":"me too","created_at":"2024-01-01"}`))
	})
	c, _ := newTestServer(t, handler)

	comment, err := c.CreateIssueComment(context.Background(), "acme", "demo", 7, "me too")
	if err != nil {
		t.Fatalf("CreateIssueComment() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/issues/7/comments" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["body"] != "me too" {
		t.Errorf("body = %+v", gotBody)
	}
	if comment.ID != 9 || comment.Body != "me too" {
		t.Errorf("comment = %+v", comment)
	}
}

// =============================================================================
// #21 forgejo_pull_create — repoCreatePullRequest
// =============================================================================

func TestCreatePullRequest(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":1,"number":5,"title":"PR","body":"b","state":"open"}`))
	})
	c, _ := newTestServer(t, handler)

	pr, err := c.CreatePullRequest(context.Background(), domain.CreatePullRequestInput{
		Owner: "acme", Repo: "demo", Title: "PR", Body: "b", Head: "dev", Base: "main",
	})
	if err != nil {
		t.Fatalf("CreatePullRequest() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/pulls" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["head"] != "dev" || gotBody["base"] != "main" || gotBody["title"] != "PR" {
		t.Errorf("body = %+v", gotBody)
	}
	if pr.Number != 5 || pr.Title != "PR" {
		t.Errorf("pr = %+v", pr)
	}
}

// =============================================================================
// #22 forgejo_pull_update — repoEditPullRequest (idempotent)
// =============================================================================

func TestUpdatePullRequest(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":1,"number":5,"title":"PR v2","body":"new","state":"closed"}`))
	})
	c, _ := newTestServer(t, handler)

	pr, err := c.UpdatePullRequest(context.Background(), domain.UpdatePullRequestInput{
		Owner: "acme", Repo: "demo", Index: 5, Title: "PR v2", Body: "new", State: "closed",
	})
	if err != nil {
		t.Fatalf("UpdatePullRequest() error = %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/pulls/5" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["title"] != "PR v2" || gotBody["state"] != "closed" {
		t.Errorf("body = %+v", gotBody)
	}
	if pr.State != "closed" {
		t.Errorf("pr = %+v", pr)
	}
}

// =============================================================================
// #23 forgejo_pull_merge — repoMergePullRequest + repoPullRequestIsMerged
// =============================================================================

func TestIsPullRequestMergedTrue(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	c, _ := newTestServer(t, handler)
	merged, err := c.IsPullRequestMerged(context.Background(), "acme", "demo", 5)
	if err != nil {
		t.Fatalf("IsPullRequestMerged() error = %v", err)
	}
	if !merged {
		t.Error("expected merged=true for 204")
	}
}

func TestIsPullRequestMergedFalse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	c, _ := newTestServer(t, handler)
	merged, err := c.IsPullRequestMerged(context.Background(), "acme", "demo", 5)
	if err != nil {
		t.Fatalf("IsPullRequestMerged() error = %v", err)
	}
	if merged {
		t.Error("expected merged=false for 404")
	}
}

func TestIsPullRequestMergedOtherError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, _ := newTestServer(t, handler)
	_, err := c.IsPullRequestMerged(context.Background(), "acme", "demo", 5)
	if err == nil {
		t.Fatal("expected error for 5xx")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindTransient {
		t.Errorf("expected transient, got %v", err)
	}
}

func TestMergePullRequest(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		w.WriteHeader(http.StatusOK)
	})
	c, _ := newTestServer(t, handler)

	if err := c.MergePullRequest(context.Background(), "acme", "demo", 5, "squash"); err != nil {
		t.Fatalf("MergePullRequest() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/pulls/5/merge" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["Do"] != "squash" {
		t.Errorf("body = %+v, want Do=squash", gotBody)
	}
}

// =============================================================================
// #24 forgejo_pull_review — repoCreatePullReview + repoSubmitPullReview
// =============================================================================

func TestCreatePullReview(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":11,"body":"looks good","state":"PENDING"}`))
	})
	c, _ := newTestServer(t, handler)

	review, err := c.CreatePullReview(context.Background(), "acme", "demo", 5, "looks good")
	if err != nil {
		t.Fatalf("CreatePullReview() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/pulls/5/reviews" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["event"] != "PENDING" || gotBody["body"] != "looks good" {
		t.Errorf("body = %+v", gotBody)
	}
	if review.ID != 11 || review.State != "PENDING" {
		t.Errorf("review = %+v", review)
	}
}

func TestSubmitPullReview(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":11,"body":"looks good","state":"APPROVED"}`))
	})
	c, _ := newTestServer(t, handler)

	review, err := c.SubmitPullReview(context.Background(), "acme", "demo", 5, 11, "APPROVED")
	if err != nil {
		t.Fatalf("SubmitPullReview() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/pulls/5/reviews/11" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["event"] != "APPROVED" {
		t.Errorf("body = %+v", gotBody)
	}
	if review.State != "APPROVED" {
		t.Errorf("review = %+v", review)
	}
}

// =============================================================================
// #25 forgejo_release_create — repoCreateRelease
// =============================================================================

func TestCreateRelease(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":3,"tag_name":"v1.0","name":"V1.0","body":"notes","draft":false,"prerelease":false}`))
	})
	c, _ := newTestServer(t, handler)

	release, err := c.CreateRelease(context.Background(), domain.CreateReleaseInput{
		Owner: "acme", Repo: "demo", Tag: "v1.0", Name: "V1.0", Notes: "notes",
	})
	if err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/releases" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["tag_name"] != "v1.0" || gotBody["name"] != "V1.0" || gotBody["body"] != "notes" {
		t.Errorf("body = %+v", gotBody)
	}
	if release.TagName != "v1.0" || release.Name != "V1.0" {
		t.Errorf("release = %+v", release)
	}
}

// =============================================================================
// Batch B client: every write method must surface 5xx as transient and never
// leak the token (SPEC S2). This mirrors TestMethodsServerError for reads.
// =============================================================================

func TestWriteMethodsServerError(t *testing.T) {
	body := `{"message":"server boom ` + testToken + `"}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(body))
	})
	c, _ := newTestServer(t, handler)

	calls := []struct {
		name string
		call func() error
	}{
		{"CreateRepository", func() error {
			_, err := c.CreateRepository(context.Background(), domain.CreateRepositoryInput{Name: "n"})
			return err
		}},
		{"CreateFile", func() error {
			_, err := c.CreateFile(context.Background(), domain.CreateFileInput{Owner: "o", Repo: "r", Path: "p", Message: "m", Content: "c"})
			return err
		}},
		{"UpdateFile", func() error {
			_, err := c.UpdateFile(context.Background(), domain.UpdateFileInput{Owner: "o", Repo: "r", Path: "p", Message: "m", SHA: "s", Content: "c"})
			return err
		}},
		{"ChangeFiles", func() error {
			_, err := c.ChangeFiles(context.Background(), domain.ChangeFilesInput{Owner: "o", Repo: "r", Message: "m", Files: []domain.ChangeFileEntry{{Path: "p", Content: "c"}}})
			return err
		}},
		{"CreateBranch", func() error { _, err := c.CreateBranch(context.Background(), "o", "r", "nb", "ref"); return err }},
		{"CreateIssue", func() error {
			_, err := c.CreateIssue(context.Background(), domain.CreateIssueInput{Owner: "o", Repo: "r", Title: "t"})
			return err
		}},
		{"UpdateIssue", func() error {
			_, err := c.UpdateIssue(context.Background(), domain.UpdateIssueInput{Owner: "o", Repo: "r", Index: 1, Title: "t"})
			return err
		}},
		{"CreateIssueComment", func() error { _, err := c.CreateIssueComment(context.Background(), "o", "r", 1, "b"); return err }},
		{"CreatePullRequest", func() error {
			_, err := c.CreatePullRequest(context.Background(), domain.CreatePullRequestInput{Owner: "o", Repo: "r", Title: "t", Head: "h", Base: "b"})
			return err
		}},
		{"UpdatePullRequest", func() error {
			_, err := c.UpdatePullRequest(context.Background(), domain.UpdatePullRequestInput{Owner: "o", Repo: "r", Index: 1, Title: "t"})
			return err
		}},
		{"IsPullRequestMerged", func() error { _, err := c.IsPullRequestMerged(context.Background(), "o", "r", 1); return err }},
		{"MergePullRequest", func() error { return c.MergePullRequest(context.Background(), "o", "r", 1, "merge") }},
		{"CreatePullReview", func() error { _, err := c.CreatePullReview(context.Background(), "o", "r", 1, "b"); return err }},
		{"SubmitPullReview", func() error {
			_, err := c.SubmitPullReview(context.Background(), "o", "r", 1, 1, "APPROVED")
			return err
		}},
		{"CreateRelease", func() error {
			_, err := c.CreateRelease(context.Background(), domain.CreateReleaseInput{Owner: "o", Repo: "r", Tag: "v1"})
			return err
		}},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("expected error for 5xx")
			}
			fe, ok := err.(*domain.ForgejoError)
			if !ok {
				t.Fatalf("expected *domain.ForgejoError, got %T", err)
			}
			if fe.Kind != domain.KindTransient {
				t.Errorf("Kind = %q, want transient", fe.Kind)
			}
			if strings.Contains(err.Error(), testToken) || !strings.Contains(err.Error(), "[REDACTED]") {
				t.Errorf("token not redacted: %q", err.Error())
			}
		})
	}
}
