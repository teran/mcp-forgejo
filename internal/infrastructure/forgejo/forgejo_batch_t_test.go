package forgejo

// Batch T client tests (SPEC §6): forgejo_tag_list, forgejo_tag_create,
// forgejo_tag_delete, forgejo_repo_fork, forgejo_repo_update. Test
// expectations for @developer: endpoints, HTTP methods, request bodies and the
// domain types/methods they must add. Read the header of forgejo_test.go for
// the shared helpers (newTestServer, testToken) these tests build on.
//
// @developer must add to internal/domain:
//   - type Tag struct { Name, SHA, Message, TarballURL, ZipballURL string; ID int64 }
//     with json tags name/id/sha/message/tarball_url/zipball_url
//   - type CreateTagInput struct { Name, Target, Message string }
//     with json tags tag_name/target/message
//   - type ForkRepositoryInput struct { Organization, Name, DefaultBranch string }
//     with json tags organization/name/default_branch (omitempty)
//   - type UpdateRepositoryInput struct { Description, Website, DefaultBranch string; Private *bool }
//     with json tags description/website/default_branch/private (omitempty; private is a pointer)
//
// and the client methods:
//   - ListTags(ctx, owner, repo) ([]Tag, error)               GET  /api/v1/repos/{o}/{r}/tags
//   - CreateTag(ctx, owner, repo, CreateTagInput) (Tag, error) POST /api/v1/repos/{o}/{r}/tags
//   - DeleteTag(ctx, owner, repo, tag) error                  DELETE /api/v1/repos/{o}/{r}/tags/{tag}
//   - ForkRepository(ctx, owner, repo, ForkRepositoryInput) (Repository, error)
//     POST /api/v1/repos/{o}/{r}/forks
//   - UpdateRepository(ctx, owner, repo, UpdateRepositoryInput) (Repository, error)
//     PATCH /api/v1/repos/{o}/{r}

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/teran/mcp-forgejo/internal/domain"
)

// =============================================================================
// forgejo_tag_list — repoListTags
// =============================================================================

func TestListTags(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[
			{"name":"v1.0","id":1,"sha":"abc123","message":"release 1","tarball_url":"https://x/t/v1.0.tar.gz","zipball_url":"https://x/z/v1.0.zip"},
			{"name":"v2.0","id":2,"sha":"def456","message":"release 2","tarball_url":"https://x/t/v2.0.tar.gz","zipball_url":"https://x/z/v2.0.zip"}
		]`))
	})
	c, _ := newTestServer(t, handler)

	tags, err := c.ListTags(context.Background(), "acme", "demo")
	if err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/tags" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/tags", gotPath)
	}
	if gotAuth != "token "+testToken {
		t.Errorf("auth = %q", gotAuth)
	}
	if len(tags) != 2 {
		t.Fatalf("len(tags) = %d, want 2: %+v", len(tags), tags)
	}
	if tags[0].Name != "v1.0" || tags[0].ID != 1 || tags[0].SHA != "abc123" || tags[0].Message != "release 1" {
		t.Errorf("tags[0] = %+v", tags[0])
	}
	if tags[0].TarballURL != "https://x/t/v1.0.tar.gz" || tags[0].ZipballURL != "https://x/z/v1.0.zip" {
		t.Errorf("tags[0] urls = %+v", tags[0])
	}
	if tags[1].Name != "v2.0" || tags[1].SHA != "def456" {
		t.Errorf("tags[1] = %+v", tags[1])
	}
}

func TestListTagsEmpty(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	c, _ := newTestServer(t, handler)
	tags, err := c.ListTags(context.Background(), "acme", "demo")
	if err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("expected empty tags, got %+v", tags)
	}
}

// =============================================================================
// forgejo_tag_create — repoCreateTag
// =============================================================================

func TestCreateTag(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"name":"v1.0","id":1,"sha":"abc123","message":"release 1","tarball_url":"https://x/t/v1.0.tar.gz","zipball_url":"https://x/z/v1.0.zip"}`))
	})
	c, _ := newTestServer(t, handler)

	tag, err := c.CreateTag(context.Background(), "acme", "demo", domain.CreateTagInput{
		Name: "v1.0", Target: "main", Message: "release 1",
	})
	if err != nil {
		t.Fatalf("CreateTag() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/tags" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/tags", gotPath)
	}
	if gotBody["tag_name"] != "v1.0" || gotBody["target"] != "main" || gotBody["message"] != "release 1" {
		t.Errorf("body = %+v, want tag_name/target/message", gotBody)
	}
	if tag.Name != "v1.0" || tag.SHA != "abc123" || tag.Message != "release 1" {
		t.Errorf("tag = %+v", tag)
	}
}

func TestCreateTagConflict(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"tag already exists"}`))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.CreateTag(context.Background(), "acme", "demo", domain.CreateTagInput{Name: "v1.0"})
	if err == nil {
		t.Fatal("expected error for 409")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindConflict {
		t.Errorf("expected conflict, got %v", err)
	}
}

// =============================================================================
// forgejo_tag_delete — repoDeleteTag
// =============================================================================

func TestDeleteTag(t *testing.T) {
	var gotMethod, gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	c, _ := newTestServer(t, handler)

	if err := c.DeleteTag(context.Background(), "acme", "demo", "v1.0"); err != nil {
		t.Fatalf("DeleteTag() error = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/tags/v1.0" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/tags/v1.0", gotPath)
	}
}

// =============================================================================
// forgejo_repo_fork — repoFork
// =============================================================================

func TestForkRepository(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":9,"name":"demo-fork","full_name":"acme/demo-fork","default_branch":"main"}`))
	})
	c, _ := newTestServer(t, handler)

	repo, err := c.ForkRepository(context.Background(), "acme", "demo", domain.ForkRepositoryInput{
		Organization: "team", Name: "demo-fork", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("ForkRepository() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/forks" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/forks", gotPath)
	}
	if gotBody["organization"] != "team" || gotBody["name"] != "demo-fork" || gotBody["default_branch"] != "main" {
		t.Errorf("body = %+v, want organization/name/default_branch", gotBody)
	}
	if repo.FullName != "acme/demo-fork" || repo.DefaultBranch != "main" {
		t.Errorf("repo = %+v", repo)
	}
}

func TestForkRepositoryOmitsEmptyFields(t *testing.T) {
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":9,"name":"demo","full_name":"acme/demo"}`))
	})
	c, _ := newTestServer(t, handler)

	_, err := c.ForkRepository(context.Background(), "acme", "demo", domain.ForkRepositoryInput{})
	if err != nil {
		t.Fatalf("ForkRepository() error = %v", err)
	}
	for _, f := range []string{"organization", "name", "default_branch"} {
		if _, ok := gotBody[f]; ok {
			t.Errorf("body contains %q but it should be omitted when empty: %+v", f, gotBody)
		}
	}
}

func TestForkRepositoryConflict(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"fork name already exists"}`))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.ForkRepository(context.Background(), "acme", "demo", domain.ForkRepositoryInput{Name: "demo-fork"})
	if err == nil {
		t.Fatal("expected error for 409")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindConflict {
		t.Errorf("expected conflict, got %v", err)
	}
}

// =============================================================================
// forgejo_repo_update — repoEdit (idempotent)
// =============================================================================

func TestUpdateRepository(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":7,"name":"demo","full_name":"acme/demo","description":"new desc","website":"https://example.com","default_branch":"dev","private":false}`))
	})
	c, _ := newTestServer(t, handler)

	priv := true
	repo, err := c.UpdateRepository(context.Background(), "acme", "demo", domain.UpdateRepositoryInput{
		Description: "new desc", Website: "https://example.com", DefaultBranch: "dev", Private: &priv,
	})
	if err != nil {
		t.Fatalf("UpdateRepository() error = %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo", gotPath)
	}
	if gotBody["description"] != "new desc" || gotBody["website"] != "https://example.com" ||
		gotBody["default_branch"] != "dev" || gotBody["private"] != true {
		t.Errorf("body = %+v, want description/website/default_branch/private", gotBody)
	}
	if repo.Description != "new desc" || repo.DefaultBranch != "dev" {
		t.Errorf("repo = %+v", repo)
	}
}

func TestUpdateRepositoryOmitsEmptyAndPrivateNil(t *testing.T) {
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":7,"name":"demo","full_name":"acme/demo"}`))
	})
	c, _ := newTestServer(t, handler)

	_, err := c.UpdateRepository(context.Background(), "acme", "demo", domain.UpdateRepositoryInput{})
	if err != nil {
		t.Fatalf("UpdateRepository() error = %v", err)
	}
	for _, f := range []string{"description", "website", "default_branch", "private"} {
		if _, ok := gotBody[f]; ok {
			t.Errorf("body contains %q but it should be omitted when empty/nil: %+v", f, gotBody)
		}
	}
}

// =============================================================================
// Batch T client: every method must surface 5xx as transient and never leak
// the token (SPEC S2), mirroring TestMethodsServerError/TestWriteMethodsServerError.
// =============================================================================

func TestBatchTMethodsServerError(t *testing.T) {
	body := `{"message":"server boom ` + testToken + `"}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(body))
	})
	c, _ := newTestServer(t, handler)

	priv := true
	calls := []struct {
		name string
		call func() error
	}{
		{"ListTags", func() error { _, err := c.ListTags(context.Background(), "acme", "demo"); return err }},
		{"CreateTag", func() error {
			_, err := c.CreateTag(context.Background(), "acme", "demo", domain.CreateTagInput{Name: "v1"})
			return err
		}},
		{"DeleteTag", func() error { return c.DeleteTag(context.Background(), "acme", "demo", "v1") }},
		{"ForkRepository", func() error {
			_, err := c.ForkRepository(context.Background(), "acme", "demo", domain.ForkRepositoryInput{Name: "f"})
			return err
		}},
		{"UpdateRepository", func() error {
			_, err := c.UpdateRepository(context.Background(), "acme", "demo", domain.UpdateRepositoryInput{Private: &priv})
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

// TestBatchTResponseBodyReadError verifies the read methods also surface a
// failing response-body read as transient (mirrors TestResponseBodyReadErrorIsTransient).
func TestBatchTResponseBodyReadError(t *testing.T) {
	c := NewWithClient(Config{BaseURL: "https://git.example.dev", Token: testToken}, &http.Client{Transport: errTransport{}})
	_, err := c.ListTags(context.Background(), "acme", "demo")
	if err == nil {
		t.Fatal("expected error for failing response body read")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindTransient {
		t.Errorf("expected transient, got %v", err)
	}
}
