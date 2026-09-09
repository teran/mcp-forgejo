package forgejo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.homelab.teran.dev/teran/mcp-forgejo/internal/domain"
)

const testToken = "super-secret-pat"

// newTestServer starts an httptest server and a client pointed at it.
func newTestServer(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	c := New(Config{BaseURL: ts.URL, Token: testToken}, ts.Client())
	return c, ts
}

func TestNewDefaultsClient(t *testing.T) {
	c := New(Config{BaseURL: "https://git.example.dev/", Token: "t"}, nil)
	if c.httpClient != http.DefaultClient {
		t.Error("expected http.DefaultClient when nil passed")
	}
	if c.baseURL != "https://git.example.dev" {
		t.Errorf("baseURL = %q, want trailing slash trimmed", c.baseURL)
	}
}

func TestGetRepositoryHappy(t *testing.T) {
	var gotPath, gotAuth string
	var gotQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":7,"name":"demo","full_name":"acme/demo","private":true,"default_branch":"master","description":"a repo","archived":false,"empty":false,"size":123,"language":"Go"}`))
	})
	c, _ := newTestServer(t, handler)

	repo, err := c.GetRepository(context.Background(), "acme", "demo")
	if err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}
	if gotPath != "/api/v1/repos/acme/demo" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "token "+testToken {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want empty", gotQuery)
	}
	if repo.ID != 7 || repo.Name != "demo" || repo.FullName != "acme/demo" || !repo.Private {
		t.Errorf("unexpected repo: %+v", repo)
	}
	if repo.DefaultBranch != "master" || repo.Language != "Go" || repo.Size != 123 {
		t.Errorf("unexpected repo fields: %+v", repo)
	}
}

func TestGetRepositoryEscapesPath(t *testing.T) {
	var gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{}`))
	})
	c, _ := newTestServer(t, handler)
	if _, err := c.GetRepository(context.Background(), "acme", "my repo"); err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}
	if gotPath != "/api/v1/repos/acme/my%20repo" {
		t.Errorf("path = %q, want space escaped", gotPath)
	}
}

func TestListContentsParams(t *testing.T) {
	var gotPath, gotQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[]`))
	})
	c, _ := newTestServer(t, handler)

	if _, err := c.ListContents(context.Background(), "acme", "demo", "", "", 0, 0); err != nil {
		t.Fatalf("ListContents() error = %v", err)
	}
	if gotPath != "/api/v1/repos/acme/demo/contents" {
		t.Errorf("root path = %q", gotPath)
	}
	if gotQuery != "" {
		t.Errorf("root query = %q, want empty", gotQuery)
	}

	if _, err := c.ListContents(context.Background(), "acme", "demo", "src/lib", "main", 2, 25); err != nil {
		t.Fatalf("ListContents() error = %v", err)
	}
	if gotPath != "/api/v1/repos/acme/demo/contents/src/lib" {
		t.Errorf("dir path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "ref=main") || !strings.Contains(gotQuery, "page=2") || !strings.Contains(gotQuery, "limit=25") {
		t.Errorf("query = %q, want ref, page and limit", gotQuery)
	}
}

func TestListContentsDecodesEntries(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"a.go","path":"src/a.go","type":"file","size":10,"sha":"abc","download_url":"https://x/a.go"}]`))
	})
	c, _ := newTestServer(t, handler)
	entries, err := c.ListContents(context.Background(), "acme", "demo", "src", "", 0, 0)
	if err != nil {
		t.Fatalf("ListContents() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "a.go" || entries[0].Type != "file" || entries[0].SHA != "abc" || entries[0].Size != 10 {
		t.Errorf("unexpected entries: %+v", entries)
	}
}

func TestGetFileText(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("package main\n"))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/acme/demo/contents/main.go" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"name":"main.go","path":"main.go","sha":"sha1","size":13,"type":"file","encoding":"base64","content":"` + payload + `"}`))
	})
	c, _ := newTestServer(t, handler)
	f, err := c.GetFile(context.Background(), "acme", "demo", "main.go", "")
	if err != nil {
		t.Fatalf("GetFile() error = %v", err)
	}
	if f.Binary {
		t.Error("expected text file, got binary")
	}
	if f.Content != "package main\n" {
		t.Errorf("content = %q", f.Content)
	}
	if f.SHA != "sha1" || f.Encoding != "base64" {
		t.Errorf("metadata = %+v", f)
	}
}

func TestGetFileBinary(t *testing.T) {
	// Invalid UTF-8 byte sequence.
	payload := base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe, 0x00, 0x01})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"img.png","encoding":"base64","content":"` + payload + `"}`))
	})
	c, _ := newTestServer(t, handler)
	f, err := c.GetFile(context.Background(), "acme", "demo", "img.png", "")
	if err != nil {
		t.Fatalf("GetFile() error = %v", err)
	}
	if !f.Binary {
		t.Error("expected binary flag true for invalid utf-8")
	}
	if f.Content != "" {
		t.Errorf("content should be empty for binary, got %q", f.Content)
	}
}

func TestGetFileMalformedBase64(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"bad","encoding":"base64","content":"!!not-base64!!"}`))
	})
	c, _ := newTestServer(t, handler)
	f, err := c.GetFile(context.Background(), "acme", "demo", "bad", "")
	if err != nil {
		t.Fatalf("GetFile() error = %v", err)
	}
	if !f.Binary {
		t.Error("expected binary flag true for malformed base64")
	}
}

func TestGetFileNonBase64Encoding(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"f","encoding":"none","content":"plain text"}`))
	})
	c, _ := newTestServer(t, handler)
	f, err := c.GetFile(context.Background(), "acme", "demo", "f", "")
	if err != nil {
		t.Fatalf("GetFile() error = %v", err)
	}
	if f.Binary {
		t.Error("expected text for non-base64 encoding")
	}
	if f.Content != "plain text" {
		t.Errorf("content = %q", f.Content)
	}
}

func TestGetFileErrorPropagates(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	c, _ := newTestServer(t, handler)
	_, err := c.GetFile(context.Background(), "acme", "demo", "missing", "")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindNotFound {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestListOrganizations(t *testing.T) {
	var gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`[{"id":1,"username":"acme","full_name":"ACME Inc","description":"d","website":"https://x","location":"NY"}]`))
	})
	c, _ := newTestServer(t, handler)
	orgs, err := c.ListOrganizations(context.Background())
	if err != nil {
		t.Fatalf("ListOrganizations() error = %v", err)
	}
	if gotPath != "/api/v1/user/orgs" {
		t.Errorf("path = %q", gotPath)
	}
	if len(orgs) != 1 || orgs[0].Username != "acme" || orgs[0].FullName != "ACME Inc" {
		t.Errorf("unexpected orgs: %+v", orgs)
	}
}

func TestGetIssueAndComments(t *testing.T) {
	var paths []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/comments") {
			_, _ = w.Write([]byte(`[{"id":2,"body":"first","created_at":"2024-01-01"}]`))
			return
		}
		_, _ = w.Write([]byte(`{"id":1,"number":5,"title":"t","body":"b","state":"open","comments":1}`))
	})
	c, _ := newTestServer(t, handler)

	issue, err := c.GetIssue(context.Background(), "acme", "demo", 5)
	if err != nil {
		t.Fatalf("GetIssue() error = %v", err)
	}
	if issue.Number != 5 || issue.Title != "t" || issue.State != "open" {
		t.Errorf("unexpected issue: %+v", issue)
	}
	if paths[0] != "/api/v1/repos/acme/demo/issues/5" {
		t.Errorf("issue path = %q", paths[0])
	}

	comments, err := c.ListComments(context.Background(), "acme", "demo", 5)
	if err != nil {
		t.Fatalf("ListComments() error = %v", err)
	}
	if len(comments) != 1 || comments[0].ID != 2 || comments[0].Body != "first" {
		t.Errorf("unexpected comments: %+v", comments)
	}
	if paths[1] != "/api/v1/repos/acme/demo/issues/5/comments" {
		t.Errorf("comments path = %q", paths[1])
	}
}

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantKind   domain.ErrorKind
		wantInBody string
	}{
		{"not found", http.StatusNotFound, "", domain.KindNotFound, "Forgejo resource not found"},
		{"unauthorized", http.StatusUnauthorized, "", domain.KindUnauthorized, "Forgejo authorization failed"},
		{"forbidden", http.StatusForbidden, "", domain.KindForbidden, "Forgejo access forbidden"},
		{"validation", http.StatusUnprocessableEntity, `{"message":"bad input"}`, domain.KindValidation, "bad input"},
		{"conflict", http.StatusConflict, "", domain.KindConflict, "Forgejo conflict"},
		{"archived", http.StatusLocked, "", domain.KindArchived, "Forgejo repository is archived"},
		{"server error", http.StatusInternalServerError, "", domain.KindTransient, "500"},
		{"unknown", http.StatusTeapot, "", domain.KindUnknown, "418"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			c, _ := newTestServer(t, handler)
			_, err := c.GetRepository(context.Background(), "acme", "demo")
			if err == nil {
				t.Fatal("expected error")
			}
			fe, ok := err.(*domain.ForgejoError)
			if !ok {
				t.Fatalf("expected *domain.ForgejoError, got %T", err)
			}
			if fe.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", fe.Kind, tt.wantKind)
			}
			if !strings.Contains(fe.Message, tt.wantInBody) {
				t.Errorf("Message = %q, want to contain %q", fe.Message, tt.wantInBody)
			}
		})
	}
}

func TestErrorMappingRedactsToken(t *testing.T) {
	// Simulate a Forgejo body that echoes back something containing the token.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"invalid token "+` + `"` + testToken + `"}`))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.GetRepository(context.Background(), "acme", "demo")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("token leaked in error: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("expected [REDACTED] in error: %q", err.Error())
	}
}

func TestNonJSONBodyParseError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("this is not json"))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.GetRepository(context.Background(), "acme", "demo")
	if err == nil {
		t.Fatal("expected parse error")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok {
		t.Fatalf("expected *domain.ForgejoError, got %T", err)
	}
	if fe.Kind != domain.KindValidation {
		t.Errorf("Kind = %q, want validation for JSON parse error", fe.Kind)
	}
	if !strings.Contains(fe.Message, "decode") {
		t.Errorf("Message = %q, want decode mention", fe.Message)
	}
}

func TestEmptyOutNilBody(t *testing.T) {
	var hit bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		_, _ = w.Write([]byte(`ok`))
	})
	c, _ := newTestServer(t, handler)
	if err := c.do(context.Background(), http.MethodGet, "/api/v1/user/orgs", nil, nil); err != nil {
		t.Fatalf("do() with nil out error = %v", err)
	}
	if !hit {
		t.Error("request not made")
	}
}

func TestResponseReadError(t *testing.T) {
	// A handler that panics mid-write can cause a body read error.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":`))
	})
	c, _ := newTestServer(t, handler)
	_, err := c.GetRepository(context.Background(), "acme", "demo")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestTransportErrorIsTransient(t *testing.T) {
	c := New(Config{BaseURL: "http://127.0.0.1:1", Token: "t"}, &http.Client{})
	_, err := c.GetRepository(context.Background(), "acme", "demo")
	if err == nil {
		t.Fatal("expected transport error")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok {
		t.Fatalf("expected *domain.ForgejoError, got %T", err)
	}
	if fe.Kind != domain.KindTransient {
		t.Errorf("Kind = %q, want transient", fe.Kind)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	var body domain.Repository
	_ = json.Unmarshal([]byte(`{"id":1}`), &body)
	if body.ID != 1 {
		t.Errorf("round trip failed: %+v", body)
	}
}

func TestGetFileWithRef(t *testing.T) {
	var gotQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"name":"f","encoding":"none","content":"x"}`))
	})
	c, _ := newTestServer(t, handler)
	f, err := c.GetFile(context.Background(), "acme", "demo", "main.go", "main")
	if err != nil {
		t.Fatalf("GetFile() error = %v", err)
	}
	if gotQuery != "ref=main" {
		t.Errorf("query = %q, want ref=main", gotQuery)
	}
	if f.Content != "x" {
		t.Errorf("content = %q", f.Content)
	}
}

func TestDoBuildRequestError(t *testing.T) {
	// An invalid HTTP method makes http.NewRequestWithContext fail, which must
	// surface as a transient ForgejoError.
	c := New(Config{BaseURL: "https://git.example.dev", Token: testToken}, &http.Client{})
	err := c.do(context.Background(), "bad method", "/api/v1/x", nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid HTTP method")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok {
		t.Fatalf("expected *domain.ForgejoError, got %T", err)
	}
	if fe.Kind != domain.KindTransient {
		t.Errorf("Kind = %q, want transient", fe.Kind)
	}
}

// errReadCloser is a response body whose Read always fails.
type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) { return 0, errors.New("read boom") }
func (errReadCloser) Close() error             { return nil }

// errTransport returns a 200 response whose body fails on read.
type errTransport struct{}

func (errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{},
		Body:       errReadCloser{},
	}, nil
}

func TestDoResponseBodyReadError(t *testing.T) {
	// A response body that errors on read must surface as a transient error.
	c := New(Config{BaseURL: "https://git.example.dev", Token: testToken}, &http.Client{Transport: errTransport{}})
	err := c.do(context.Background(), http.MethodGet, "/api/v1/repos/a/b", nil, &domain.Repository{})
	if err == nil {
		t.Fatal("expected error for failing response body read")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok {
		t.Fatalf("expected *domain.ForgejoError, got %T", err)
	}
	if fe.Kind != domain.KindTransient {
		t.Errorf("Kind = %q, want transient", fe.Kind)
	}
}
