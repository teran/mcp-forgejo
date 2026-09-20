package forgejo

// Batch U client tests (SPEC §6): forgejo_user_get, forgejo_user_list,
// forgejo_release_asset_upload, forgejo_branch_get. Test expectations for
// @developer: endpoints, HTTP methods, request bodies and the domain
// types/methods they must add. Read the header of forgejo_test.go for the
// shared helpers (newTestServer, testToken) these tests build on.
//
// @developer must add to domain:
//   - type ReleaseAsset struct { ID int64; Name string; Size int64;
//     DownloadURL, BrowserDownloadURL string } with json tags
//     id/name/size/download_url/browser_download_url
//   - the client may reuse the existing domain.User (ID, Login, FullName,
//     AvatarURL; json id/login/full_name/avatar_url) and domain.Branch (Name,
//     Protected, Default, CommitSHA; json name/protected/default/commit_sha).
//
// and the client methods:
//   - GetUser(ctx, username) (User, error)
//     GET /api/v1/users/{username}; when username is empty GET /api/v1/user
//   - SearchUsers(ctx, q) ([]User, error)
//     GET /api/v1/users/search?q=...  (unwrap {"ok":true,"data":[...]}.data)
//   - UploadReleaseAsset(ctx, owner, repo, releaseID, filename, content) (ReleaseAsset, error)
//     POST /api/v1/repos/{o}/{r}/releases/{id}/assets, multipart form
//     (name=filename, attachment=file)
//   - GetBranch(ctx, owner, repo, branch) (Branch, error)
//     GET /api/v1/repos/{o}/{r}/branches/{branch}  (commit.id flattened to CommitSHA)

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/teran/mcp-forgejo/domain"
)

// =============================================================================
// forgejo_user_get — userGet (current or by username)
// =============================================================================

func TestGetUserByName(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"id":5,"login":"alice","full_name":"Alice A.","avatar_url":"https://x/a.png"}`))
	})
	c, _ := newTestServer(t, handler)

	u, err := c.GetUser(context.Background(), "alice")
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v1/users/alice" {
		t.Errorf("path = %q, want /api/v1/users/alice", gotPath)
	}
	if gotAuth != "token "+testToken {
		t.Errorf("auth = %q", gotAuth)
	}
	if u.ID != 5 || u.Login != "alice" || u.FullName != "Alice A." || u.AvatarURL != "https://x/a.png" {
		t.Errorf("user = %+v", u)
	}
}

func TestGetUserCurrent(t *testing.T) {
	var gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"id":5,"login":"alice","full_name":"Alice A."}`))
	})
	c, _ := newTestServer(t, handler)

	u, err := c.GetUser(context.Background(), "")
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if gotPath != "/api/v1/user" {
		t.Errorf("path = %q, want /api/v1/user", gotPath)
	}
	if u.Login != "alice" {
		t.Errorf("user = %+v", u)
	}
}

func TestGetUserNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"user not found"}`))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.GetUser(context.Background(), "ghost")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindNotFound {
		t.Errorf("expected not_found, got %v", err)
	}
}

// =============================================================================
// forgejo_user_list — userSearch
// =============================================================================

func TestSearchUsers(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"ok":true,"data":[
			{"id":1,"login":"alice","full_name":"Alice A.","avatar_url":"https://x/a.png"},
			{"id":2,"login":"bob","full_name":"Bob B.","avatar_url":"https://x/b.png"}
		]}`))
	})
	c, _ := newTestServer(t, handler)

	users, err := c.SearchUsers(context.Background(), "alice")
	if err != nil {
		t.Fatalf("SearchUsers() error = %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v1/users/search" {
		t.Errorf("path = %q, want /api/v1/users/search", gotPath)
	}
	if gotAuth != "token "+testToken {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotQuery != "q=alice" {
		t.Errorf("query = %q, want q=alice", gotQuery)
	}
	// The {"ok":true,"data":[...]} envelope must be unwrapped to the slice.
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2: %+v", len(users), users)
	}
	if users[0].Login != "alice" || users[0].ID != 1 || users[1].Login != "bob" {
		t.Errorf("users = %+v", users)
	}
}

func TestSearchUsersEmptyQ(t *testing.T) {
	var gotQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"ok":true,"data":[]}`))
	})
	c, _ := newTestServer(t, handler)

	users, err := c.SearchUsers(context.Background(), "")
	if err != nil {
		t.Fatalf("SearchUsers() error = %v", err)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want empty when q is empty", gotQuery)
	}
	if len(users) != 0 {
		t.Errorf("expected empty users, got %+v", users)
	}
}

// =============================================================================
// forgejo_release_asset_upload — repoCreateReleaseAttachment
// =============================================================================

func TestUploadReleaseAsset(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotName string
	var gotFileHeader, gotContent []byte
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		gotName = r.FormValue("name")
		f, h, err := r.FormFile("attachment")
		if err != nil {
			t.Fatalf("attachment file: %v", err)
		}
		defer f.Close()
		gotFileHeader = []byte(h.Filename)
		gotContent, _ = io.ReadAll(f)
		_, _ = w.Write([]byte(`{"id":99,"name":"demo.bin","size":7,"download_url":"https://x/d/demo.bin","browser_download_url":"https://x/b/demo.bin"}`))
	})
	c, _ := newTestServer(t, handler)

	asset, err := c.UploadReleaseAsset(context.Background(), "acme", "demo", 5, "demo.bin", []byte("payload"))
	if err != nil {
		t.Fatalf("UploadReleaseAsset() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/releases/5/assets" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/releases/5/assets", gotPath)
	}
	if gotAuth != "token "+testToken {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotName != "demo.bin" {
		t.Errorf("form name = %q, want demo.bin", gotName)
	}
	if string(gotFileHeader) != "demo.bin" {
		t.Errorf("attachment filename = %q, want demo.bin", gotFileHeader)
	}
	if !bytes.Equal(gotContent, []byte("payload")) {
		t.Errorf("attachment content = %q, want payload", gotContent)
	}
	if asset.ID != 99 || asset.Name != "demo.bin" || asset.Size != 7 ||
		asset.DownloadURL != "https://x/d/demo.bin" || asset.BrowserDownloadURL != "https://x/b/demo.bin" {
		t.Errorf("asset = %+v", asset)
	}
}

// =============================================================================
// forgejo_branch_get — repoGetBranch
// =============================================================================

func TestGetBranch(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"name":"main","protected":true,"default":true,"commit":{"id":"abc123def456"}}`))
	})
	c, _ := newTestServer(t, handler)

	b, err := c.GetBranch(context.Background(), "acme", "demo", "main")
	if err != nil {
		t.Fatalf("GetBranch() error = %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/branches/main" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/branches/main", gotPath)
	}
	if gotAuth != "token "+testToken {
		t.Errorf("auth = %q", gotAuth)
	}
	// The nested commit.id must be flattened into CommitSHA.
	if b.Name != "main" || !b.Protected || !b.Default || b.CommitSHA != "abc123def456" {
		t.Errorf("branch = %+v", b)
	}
}

func TestGetBranchNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"branch not found"}`))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.GetBranch(context.Background(), "acme", "demo", "nope")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindNotFound {
		t.Errorf("expected not_found, got %v", err)
	}
}

// =============================================================================
// Batch U client: every method must surface 5xx as transient and never leak
// the token (SPEC S2), mirroring TestBatchTMethodsServerError.
// =============================================================================

func TestBatchUMethodsServerError(t *testing.T) {
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
		{"GetUser", func() error { _, err := c.GetUser(context.Background(), "alice"); return err }},
		{"SearchUsers", func() error { _, err := c.SearchUsers(context.Background(), "alice"); return err }},
		{"UploadReleaseAsset", func() error {
			_, err := c.UploadReleaseAsset(context.Background(), "acme", "demo", 5, "f.bin", []byte("x"))
			return err
		}},
		{"GetBranch", func() error { _, err := c.GetBranch(context.Background(), "acme", "demo", "main"); return err }},
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

// TestBatchUResponseBodyReadError verifies the read methods also surface a
// failing response-body read as transient (mirrors TestResponseBodyReadErrorIsTransient).
func TestBatchUResponseBodyReadError(t *testing.T) {
	c := NewWithClient(Config{BaseURL: "https://git.example.dev", Token: testToken}, &http.Client{Transport: errTransport{}})
	calls := []struct {
		name string
		call func() error
	}{
		{"GetUser", func() error { _, err := c.GetUser(context.Background(), "alice"); return err }},
		{"SearchUsers", func() error { _, err := c.SearchUsers(context.Background(), "alice"); return err }},
		{"UploadReleaseAsset", func() error {
			_, err := c.UploadReleaseAsset(context.Background(), "acme", "demo", 5, "f.bin", []byte("x"))
			return err
		}},
		{"GetBranch", func() error { _, err := c.GetBranch(context.Background(), "acme", "demo", "main"); return err }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("expected error for failing response body read")
			}
			fe, ok := err.(*domain.ForgejoError)
			if !ok || fe.Kind != domain.KindTransient {
				t.Errorf("expected transient, got %v", err)
			}
		})
	}
}
