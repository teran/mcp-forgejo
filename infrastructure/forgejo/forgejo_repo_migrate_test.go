package forgejo

// forgejo_repo_migrate client tests (SPEC 6.2, repoMigrate). Test expectations
// for @developer: the endpoint, HTTP method, request body and the domain
// type/method they must add. Read the header of forgejo_test.go for the shared
// helpers (newTestServer, testToken) and forgejo_write_test.go for decodeBody.
//
// @developer must add to domain:
//   - type MigrateRepositoryInput struct — json tags clone_addr / repo_name
//     (required), optional repo_owner, service, auth_username, auth_token,
//     mirror, mirror_interval, private, description, issues, labels,
//     milestones, pull_requests, releases, wiki, lfs, lfs_endpoint. Optional
//     booleans are *bool (omitempty) so an omitted value is left to the server
//     default.
//   - type RepositoryMigrateService interface { MigrateRepository(ctx, in
//     MigrateRepositoryInput) (Repository, error) }
//
// and the client method:
//   - MigrateRepository(ctx, MigrateRepositoryInput) (Repository, error)
//     POST /api/v1/repos/migrate  (operationId repoMigrate)
//
// S02 redaction: the source PAT arrives in the body as auth_token. It is a
// SECRET and must never surface in an error/log. The generic mapStatusError
// redacts against the Forgejo PAT (c.token) only, so the client must ALSO scrub
// in.AuthToken from any surfaced error (see
// TestMigrateRepositoryServerErrorRedactsAuthToken).

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/teran/mcp-forgejo/domain"
)

// srcAuthToken is the (fake) source git provider PAT used to prove it is
// redacted from any surfaced error (S02).
const srcAuthToken = "ghp_src_secret_token"

func boolPtr(b bool) *bool { return &b }

// =============================================================================
// forgejo_repo_migrate — happy path
// =============================================================================

func TestMigrateRepository(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":12,"name":"migrated","full_name":"alice/migrated","private":true,"default_branch":"main"}`))
	})
	c, _ := newTestServer(t, handler)

	mirror, priv := true, true
	repo, err := c.MigrateRepository(context.Background(), domain.MigrateRepositoryInput{
		CloneAddr:      "https://github.com/acme/upstream.git",
		RepoName:       "migrated",
		RepoOwner:      "alice",
		Service:        "github",
		AuthUsername:   "alice",
		AuthToken:      srcAuthToken,
		Mirror:         &mirror,
		MirrorInterval: "8h",
		Private:        &priv,
		Description:    "imported repo",
		Issues:         boolPtr(true),
		Labels:         boolPtr(false),
		LFS:            boolPtr(true),
		LFSEndpoint:    "https://lfs.example.com",
	})
	if err != nil {
		t.Fatalf("MigrateRepository() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/migrate" {
		t.Errorf("path = %q, want /api/v1/repos/migrate", gotPath)
	}
	if gotAuth != "token "+testToken {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotBody["clone_addr"] != "https://github.com/acme/upstream.git" || gotBody["repo_name"] != "migrated" {
		t.Errorf("body = %+v, want clone_addr/repo_name set", gotBody)
	}
	if gotBody["repo_owner"] != "alice" || gotBody["service"] != "github" ||
		gotBody["auth_username"] != "alice" || gotBody["auth_token"] != srcAuthToken {
		t.Errorf("body = %+v, want repo_owner/service/auth_username/auth_token set", gotBody)
	}
	if gotBody["mirror"] != true || gotBody["mirror_interval"] != "8h" || gotBody["private"] != true ||
		gotBody["description"] != "imported repo" {
		t.Errorf("body = %+v, want mirror/mirror_interval/private/description set", gotBody)
	}
	if gotBody["issues"] != true || gotBody["labels"] != false || gotBody["lfs"] != true ||
		gotBody["lfs_endpoint"] != "https://lfs.example.com" {
		t.Errorf("body = %+v, want issues/labels/lfs/lfs_endpoint set", gotBody)
	}
	if repo.Name != "migrated" || repo.FullName != "alice/migrated" || !repo.Private {
		t.Errorf("repo = %+v", repo)
	}
}

// The body must omit empty optional strings and nil optional booleans, keeping
// only the required clone_addr/repo_name.
func TestMigrateRepositoryOmitsEmptyOptional(t *testing.T) {
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":12,"name":"migrated","full_name":"alice/migrated"}`))
	})
	c, _ := newTestServer(t, handler)

	_, err := c.MigrateRepository(context.Background(), domain.MigrateRepositoryInput{
		CloneAddr: "https://github.com/acme/upstream.git",
		RepoName:  "migrated",
	})
	if err != nil {
		t.Fatalf("MigrateRepository() error = %v", err)
	}
	if gotBody["clone_addr"] != "https://github.com/acme/upstream.git" || gotBody["repo_name"] != "migrated" {
		t.Errorf("required fields must always be present: %+v", gotBody)
	}
	for _, f := range []string{
		"repo_owner", "service", "auth_username", "auth_token", "mirror", "mirror_interval",
		"private", "description", "issues", "labels", "milestones", "pull_requests",
		"releases", "wiki", "lfs", "lfs_endpoint",
	} {
		if _, ok := gotBody[f]; ok {
			t.Errorf("body contains %q but it should be omitted when empty/nil: %+v", f, gotBody)
		}
	}
}

// =============================================================================
// forgejo_repo_migrate — error taxonomy (SPEC 4.4)
// =============================================================================

func TestMigrateRepositoryConflict(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"repository already exists"}`))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.MigrateRepository(context.Background(), domain.MigrateRepositoryInput{CloneAddr: "u", RepoName: "r"})
	if err == nil {
		t.Fatal("expected error for 409")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindConflict {
		t.Errorf("expected conflict, got %v", err)
	}
}

func TestMigrateRepositoryForbidden(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.MigrateRepository(context.Background(), domain.MigrateRepositoryInput{CloneAddr: "u", RepoName: "r"})
	if err == nil {
		t.Fatal("expected error for 403")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindForbidden {
		t.Errorf("expected forbidden, got %v", err)
	}
}

func TestMigrateRepositoryValidation(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"invalid input"}`))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.MigrateRepository(context.Background(), domain.MigrateRepositoryInput{CloneAddr: "u", RepoName: "r"})
	if err == nil {
		t.Fatal("expected error for 422")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindValidation {
		t.Errorf("expected validation, got %v", err)
	}
}

// A 5xx must surface as transient and never leak the Forgejo PAT (S02).
func TestMigrateRepositoryServerErrorRedactsToken(t *testing.T) {
	body := `{"message":"server boom ` + testToken + `"}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(body))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.MigrateRepository(context.Background(), domain.MigrateRepositoryInput{CloneAddr: "u", RepoName: "r"})
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

// The source git provider PAT (auth_token) is a separate secret from the
// Forgejo PAT. When Forgejo echoes it in a failure body, the surfaced error must
// never contain it (S02). The client must scrub in.AuthToken in addition to the
// generic c.token redaction.
func TestMigrateRepositoryServerErrorRedactsAuthToken(t *testing.T) {
	body := `{"message":"auth failed with token ` + srcAuthToken + ` and forgejo ` + testToken + `"}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(body))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.MigrateRepository(context.Background(), domain.MigrateRepositoryInput{
		CloneAddr: "u", RepoName: "r", AuthToken: srcAuthToken,
	})
	if err == nil {
		t.Fatal("expected error for 5xx")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindTransient {
		t.Errorf("expected transient, got %v", err)
	}
	if strings.Contains(err.Error(), srcAuthToken) {
		t.Errorf("source auth_token leaked in error: %q", err.Error())
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("forgejo token leaked in error: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("expected [REDACTED] in error: %q", err.Error())
	}
}
