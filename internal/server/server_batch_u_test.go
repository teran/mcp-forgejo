package server

// Batch U tools (SPEC §6): forgejo_user_get, forgejo_user_list,
// forgejo_release_asset_upload, forgejo_branch_get. These tests verify the
// tools are registered, carry the correct annotations (user_get/user_list/
// branch_get read, release_asset_upload write non-idempotent) and are
// invocable end to end against a mock Forgejo. The input field names below are
// the JSON schema fields @developer must expose on the server input structs.
//
// @developer must add to internal/server:
//   - register a Batch U tool group (registerBatchUTools) wired into
//     registerTools, following the read -> write grouping order.
//   - userGetIn       struct { Username string `json:"username,omitempty"` }
//   - userSearchIn    struct { Q string `json:"q"` }
//   - releaseAssetUploadIn struct { Owner, Repo, Filename string;
//     ReleaseID int64 `json:"release_id"`; Content string `json:"content"` }
//     where Content is base64; the handler must base64-decode it before calling
//     the application layer.
//   - branchGetIn     struct { Owner, Repo, Branch string }
//
// Return types: user_get -> domain.User, user_list -> domain.Items[domain.User],
// branch_get -> domain.Branch, release_asset_upload -> domain.ReleaseAsset.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teran/mcp-forgejo/internal/domain"
	"github.com/teran/mcp-forgejo/internal/infrastructure/forgejo"
)

// mockForgejoBatchU answers the Batch U endpoints.
func mockForgejoBatchU() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		m := r.Method

		switch {
		case m == http.MethodGet && p == "/api/v1/users/alice":
			_, _ = w.Write([]byte(`{"id":5,"login":"alice","full_name":"Alice A.","avatar_url":"https://x/a.png"}`))
			return
		case m == http.MethodGet && p == "/api/v1/user":
			_, _ = w.Write([]byte(`{"id":5,"login":"alice","full_name":"Alice A.","avatar_url":"https://x/a.png"}`))
			return
		case m == http.MethodGet && p == "/api/v1/users/search" && r.URL.RawQuery == "q=ali":
			_, _ = w.Write([]byte(`{"ok":true,"data":[
				{"id":1,"login":"alice","full_name":"Alice A.","avatar_url":"https://x/a.png"},
				{"id":2,"login":"alicia","full_name":"Alicia B.","avatar_url":"https://x/b.png"}
			]}`))
			return
		case m == http.MethodPost && p == "/api/v1/repos/acme/demo/releases/5/assets":
			// Echo back the uploaded filename as the asset name to prove the
			// multipart body reached the mock.
			if err := r.ParseMultipartForm(10 << 20); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"message":"bad multipart"}`))
				return
			}
			name := r.FormValue("name")
			f, _, err := r.FormFile("attachment")
			if err == nil {
				data, _ := io.ReadAll(f)
				_, _ = w.Write([]byte(`{"id":99,"name":"` + name + `","size":` + strconv.Itoa(len(data)) + `,"download_url":"https://x/d/` + name + `","browser_download_url":"https://x/b/` + name + `"}`))
				_ = f.Close()
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"missing attachment"}`))
			return
		case m == http.MethodGet && p == "/api/v1/repos/acme/demo/branches/main":
			_, _ = w.Write([]byte(`{"name":"main","protected":true,"default":true,"commit":{"id":"abc123def456"}}`))
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"unexpected "` + m + `" ` + p + `"}`))
		}
	})
}

// setupBatchU builds the MCP server backed by mockForgejoBatchU and returns a
// connected client session.
func setupBatchU(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ts := httptest.NewServer(mockForgejoBatchU())
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

var batchUWriteIdempotent = map[string]bool{
	"forgejo_release_asset_upload": false,
}

var batchUReadTools = []string{"forgejo_user_get", "forgejo_user_list", "forgejo_branch_get"}

func TestBatchUReadToolsRegistered(t *testing.T) {
	cs, _ := setup(t) // registration is transport-independent
	names := listToolMap(t, cs)
	for _, name := range batchUReadTools {
		if _, ok := names[name]; !ok {
			t.Errorf("read tool %q not registered", name)
		}
	}
}

func TestBatchUWriteToolsRegistered(t *testing.T) {
	cs, _ := setup(t)
	names := listToolMap(t, cs)
	for name := range batchUWriteIdempotent {
		if _, ok := names[name]; !ok {
			t.Errorf("write tool %q not registered", name)
		}
	}
}

func TestBatchUReadAnnotations(t *testing.T) {
	cs, _ := setup(t)
	byName := listToolMap(t, cs)
	for _, name := range batchUReadTools {
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
		if !a.ReadOnlyHint || !a.IdempotentHint {
			t.Errorf("tool %q: readOnlyHint=%v idempotentHint=%v, want true/true", name, a.ReadOnlyHint, a.IdempotentHint)
		}
		if a.DestructiveHint == nil || *a.DestructiveHint {
			t.Errorf("tool %q: destructiveHint != false", name)
		}
		if a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("tool %q: openWorldHint != false", name)
		}
	}
}

func TestBatchUWriteAnnotations(t *testing.T) {
	cs, _ := setup(t)
	byName := listToolMap(t, cs)
	for name, wantIdem := range batchUWriteIdempotent {
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

// =============================================================================
// End-to-end invocation
// =============================================================================

func TestUserGetTool(t *testing.T) {
	cs := setupBatchU(t)
	var u domain.User
	callTool(t, cs, "forgejo_user_get", map[string]any{"username": "alice"}, &u)
	if u.ID != 5 || u.Login != "alice" || u.FullName != "Alice A." || u.AvatarURL != "https://x/a.png" {
		t.Errorf("user = %+v", u)
	}
}

func TestUserGetToolCurrent(t *testing.T) {
	cs := setupBatchU(t)
	var u domain.User
	// Omitting the username returns the current authenticated user.
	callTool(t, cs, "forgejo_user_get", map[string]any{}, &u)
	if u.Login != "alice" || u.ID != 5 {
		t.Errorf("user = %+v", u)
	}
}

func TestUserSearchTool(t *testing.T) {
	cs := setupBatchU(t)
	var users domain.Items[domain.User]
	callTool(t, cs, "forgejo_user_list", map[string]any{"q": "ali"}, &users)
	if len(users.Items) != 2 || users.Items[0].Login != "alice" || users.Items[1].Login != "alicia" {
		t.Errorf("users = %+v", users)
	}
}

func TestReleaseAssetUploadTool(t *testing.T) {
	cs := setupBatchU(t)
	var asset domain.ReleaseAsset
	callTool(t, cs, "forgejo_release_asset_upload", map[string]any{
		"owner": "acme", "repo": "demo", "release_id": int64(5),
		"filename": "demo.bin", "content": base64.StdEncoding.EncodeToString([]byte("payload")),
	}, &asset)
	if asset.ID != 99 || asset.Name != "demo.bin" || asset.Size != 7 ||
		asset.DownloadURL != "https://x/d/demo.bin" || asset.BrowserDownloadURL != "https://x/b/demo.bin" {
		t.Errorf("asset = %+v", asset)
	}
}

func TestReleaseAssetUploadToolInvalidBase64(t *testing.T) {
	cs := setupBatchU(t)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "forgejo_release_asset_upload",
		Arguments: map[string]any{"owner": "acme", "repo": "demo", "release_id": int64(5), "filename": "f.bin", "content": "%%%not-base64%%%"},
	})
	if err != nil {
		t.Fatalf("CallTool error = %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError for invalid base64 content")
	}
}

func TestBranchGetTool(t *testing.T) {
	cs := setupBatchU(t)
	var b domain.Branch
	callTool(t, cs, "forgejo_branch_get", map[string]any{"owner": "acme", "repo": "demo", "branch": "main"}, &b)
	if b.Name != "main" || !b.Protected || !b.Default || b.CommitSHA != "abc123def456" {
		t.Errorf("branch = %+v", b)
	}
}

// =============================================================================
// Error surfacing + token redaction (SPEC S2)
// =============================================================================

func TestBatchUToolErrorSurfacesIsError(t *testing.T) {
	cs, _ := setup(t) // read mock returns 404 for unknown Batch U paths
	names := append(append([]string{}, batchUReadTools...), "forgejo_release_asset_upload")
	for _, name := range names {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      name,
			Arguments: map[string]any{"owner": "ghost", "repo": "missing", "branch": "main", "username": "ghost", "q": "x", "release_id": int64(1), "filename": "f.bin", "content": base64.StdEncoding.EncodeToString([]byte("x"))},
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
// List object contract: user_list returns a JSON object ({items:[...]})
// =============================================================================

func TestUserSearchReturnsObjectContract(t *testing.T) {
	cs := setupBatchU(t)
	res := callToolRaw(t, cs, "forgejo_user_list", map[string]any{"q": "ali"})
	items := listStructuredItems(t, res)
	if len(items) != 2 {
		t.Errorf("items length = %d, want 2", len(items))
	}
}

// =============================================================================
// JSON schema field pins
// =============================================================================

func TestBatchUToolJSONSchemaPinsFields(t *testing.T) {
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

	check("forgejo_user_get", []string{"username"})
	check("forgejo_user_list", []string{"q"})
	check("forgejo_release_asset_upload", []string{"owner", "repo", "release_id", "filename", "content"})
	check("forgejo_branch_get", []string{"owner", "repo", "branch"})
}
