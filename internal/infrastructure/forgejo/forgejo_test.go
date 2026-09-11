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

	"example.com/teran/mcp-forgejo/internal/domain"
)

const testToken = "super-secret-pat"

// newTestServer starts an httptest server and a client pointed at it. The
// client is built with NewWithClient so the injected *http.Client carries the
// test server's transport, keeping every test hermetic and offline.
func newTestServer(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	c := NewWithClient(Config{BaseURL: ts.URL, Token: testToken}, ts.Client())
	return c, ts
}

func TestNew(t *testing.T) {
	c := New(Config{BaseURL: "https://git.example.dev/", Token: testToken})
	if c == nil {
		t.Fatal("New returned nil client")
	}
}

func TestNewWithClientInjectsHTTPClient(t *testing.T) {
	var gotAuth string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1}`))
	})
	// Build the client directly (not via newTestServer) to prove the injected
	// *http.Client is the one actually used for requests.
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	c := NewWithClient(Config{BaseURL: ts.URL, Token: testToken}, ts.Client())
	if _, err := c.GetRepository(context.Background(), "acme", "demo"); err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}
	if gotAuth != "token "+testToken {
		t.Errorf("auth = %q, want token "+testToken, gotAuth)
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

func TestListContentsEmptyArray(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	c, _ := newTestServer(t, handler)
	entries, err := c.ListContents(context.Background(), "acme", "demo", "", "", 0, 0)
	if err != nil {
		t.Fatalf("ListContents() error = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty entries, got %+v", entries)
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

// TestErrorMapping exercises the status-to-domain-kind mapping end-to-end
// through a public method for the statuses the contract requires (404, 401,
// 409, 500) plus the rest of the taxonomy.
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

// TestMethodsServerError runs every public read method against a 5xx response
// and asserts the error is transient and the PAT never leaks into the message.
func TestMethodsServerError(t *testing.T) {
	body := `{"message":"server boom "+` + `"` + testToken + `"}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(body))
	})
	c, _ := newTestServer(t, handler)

	calls := []struct {
		name string
		call func() error
	}{
		{"GetRepository", func() error { _, err := c.GetRepository(context.Background(), "acme", "demo"); return err }},
		{"ListContents", func() error { _, err := c.ListContents(context.Background(), "acme", "demo", "", "", 0, 0); return err }},
		{"GetFile", func() error { _, err := c.GetFile(context.Background(), "acme", "demo", "f.go", ""); return err }},
		{"ListOrganizations", func() error { _, err := c.ListOrganizations(context.Background()); return err }},
		{"GetIssue", func() error { _, err := c.GetIssue(context.Background(), "acme", "demo", 1); return err }},
		{"ListComments", func() error { _, err := c.ListComments(context.Background(), "acme", "demo", 1); return err }},
		{"SearchRepos", func() error { _, err := c.SearchRepos(context.Background(), "q", "", "", "", nil); return err }},
		{"GetDiff", func() error { _, err := c.GetDiff(context.Background(), "acme", "demo", "main..dev"); return err }},
		{"GetPullDiff", func() error { _, err := c.GetPullDiff(context.Background(), "acme", "demo", 3); return err }},
		{"ListCommits", func() error { _, err := c.ListCommits(context.Background(), "acme", "demo", "", 0, 0); return err }},
		{"ListBranches", func() error { _, err := c.ListBranches(context.Background(), "acme", "demo"); return err }},
		{"ListIssues", func() error { _, err := c.ListIssues(context.Background(), "acme", "demo", "", 0, 0); return err }},
		{"ListPullRequests", func() error { _, err := c.ListPullRequests(context.Background(), "acme", "demo", "", 0, 0); return err }},
		{"GetPullRequest", func() error { _, err := c.GetPullRequest(context.Background(), "acme", "demo", 5); return err }},
		{"ListReleases", func() error { _, err := c.ListReleases(context.Background(), "acme", "demo", 0, 0); return err }},
		{"GetLatestRelease", func() error { _, err := c.GetLatestRelease(context.Background(), "acme", "demo"); return err }},
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
			if strings.Contains(err.Error(), testToken) {
				t.Errorf("token leaked in error: %q", err.Error())
			}
			if !strings.Contains(err.Error(), "[REDACTED]") {
				t.Errorf("expected [REDACTED] in error: %q", err.Error())
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

func TestEmptyBodyAt200ReturnsDecodeError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	c, _ := newTestServer(t, handler)
	_, err := c.GetRepository(context.Background(), "acme", "demo")
	if err == nil {
		t.Fatal("expected decode error for empty body")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok {
		t.Fatalf("expected *domain.ForgejoError, got %T", err)
	}
	if fe.Kind != domain.KindValidation {
		t.Errorf("Kind = %q, want validation for empty body decode", fe.Kind)
	}
}

// netErrTransport returns a network-level error for every request, simulating
// an unreachable upstream without touching the network.
type netErrTransport struct{}

func (netErrTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial tcp: connection refused")
}

func TestTransportErrorIsTransient(t *testing.T) {
	c := NewWithClient(Config{BaseURL: "http://127.0.0.1:1", Token: testToken}, &http.Client{Transport: netErrTransport{}})
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
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("token leaked in transport error: %q", err.Error())
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

func TestResponseBodyReadErrorIsTransient(t *testing.T) {
	// A response body that errors on read must surface as a transient error
	// for every read method.
	c := NewWithClient(Config{BaseURL: "https://git.example.dev", Token: testToken}, &http.Client{Transport: errTransport{}})
	calls := []struct {
		name string
		call func() error
	}{
		{"GetRepository", func() error { _, err := c.GetRepository(context.Background(), "acme", "demo"); return err }},
		{"ListContents", func() error { _, err := c.ListContents(context.Background(), "acme", "demo", "", "", 0, 0); return err }},
		{"GetFile", func() error { _, err := c.GetFile(context.Background(), "acme", "demo", "f.go", ""); return err }},
		{"ListOrganizations", func() error { _, err := c.ListOrganizations(context.Background()); return err }},
		{"GetIssue", func() error { _, err := c.GetIssue(context.Background(), "acme", "demo", 1); return err }},
		{"ListComments", func() error { _, err := c.ListComments(context.Background(), "acme", "demo", 1); return err }},
		{"SearchRepos", func() error { _, err := c.SearchRepos(context.Background(), "q", "", "", "", nil); return err }},
		{"GetDiff", func() error { _, err := c.GetDiff(context.Background(), "acme", "demo", "main..dev"); return err }},
		{"GetPullDiff", func() error { _, err := c.GetPullDiff(context.Background(), "acme", "demo", 3); return err }},
		{"ListCommits", func() error { _, err := c.ListCommits(context.Background(), "acme", "demo", "", 0, 0); return err }},
		{"ListBranches", func() error { _, err := c.ListBranches(context.Background(), "acme", "demo"); return err }},
		{"ListIssues", func() error { _, err := c.ListIssues(context.Background(), "acme", "demo", "", 0, 0); return err }},
		{"ListPullRequests", func() error { _, err := c.ListPullRequests(context.Background(), "acme", "demo", "", 0, 0); return err }},
		{"GetPullRequest", func() error { _, err := c.GetPullRequest(context.Background(), "acme", "demo", 5); return err }},
		{"ListReleases", func() error { _, err := c.ListReleases(context.Background(), "acme", "demo", 0, 0); return err }},
		{"GetLatestRelease", func() error { _, err := c.GetLatestRelease(context.Background(), "acme", "demo"); return err }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
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
		})
	}
}

// TestKindForStatus exercises every branch of kindForStatus directly so a
// regressing/mutated mapping is caught.
func TestKindForStatus(t *testing.T) {
	tests := []struct {
		status int
		want   domain.ErrorKind
	}{
		{http.StatusUnauthorized, domain.KindUnauthorized},
		{http.StatusForbidden, domain.KindForbidden},
		{http.StatusNotFound, domain.KindNotFound},
		{http.StatusUnprocessableEntity, domain.KindValidation},
		{http.StatusConflict, domain.KindConflict},
		{http.StatusLocked, domain.KindArchived},
		{http.StatusInternalServerError, domain.KindTransient},
		{http.StatusBadGateway, domain.KindTransient},
		{http.StatusTeapot, domain.KindUnknown},
		{http.StatusOK, domain.KindUnknown},
	}
	for _, tt := range tests {
		if got := kindForStatus(tt.status); got != tt.want {
			t.Errorf("kindForStatus(%d) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

// TestMapStatusError exercises mapStatusError directly: body fallback, body
// passthrough and token redaction.
func TestMapStatusError(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantKind  domain.ErrorKind
		wantInMsg string
	}{
		{"empty body fallback", http.StatusNotFound, "", domain.KindNotFound, "Forgejo resource not found"},
		{"body passthrough", http.StatusConflict, `{"message":"nope"}`, domain.KindConflict, "nope"},
		{"redact token", http.StatusInternalServerError, "boom " + testToken, domain.KindTransient, "[REDACTED]"},
		{"unknown empty", http.StatusTeapot, "", domain.KindUnknown, "418"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mapStatusError(tt.status, tt.body, testToken)
			fe, ok := err.(*domain.ForgejoError)
			if !ok {
				t.Fatalf("expected *domain.ForgejoError, got %T", err)
			}
			if fe.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", fe.Kind, tt.wantKind)
			}
			if !strings.Contains(fe.Message, tt.wantInMsg) {
				t.Errorf("Message = %q, want to contain %q", fe.Message, tt.wantInMsg)
			}
			if strings.Contains(fe.Message, testToken) {
				t.Errorf("token leaked: %q", fe.Message)
			}
		})
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

// =============================================================================
// Batch A read tools (SPEC 6.1 #1, #6, #7, #8, #9, #11, #12, #13).
// Test expectations for @developer: endpoints, wire JSON, and the domain types
// they must decode/map to.
// =============================================================================

func TestSearchRepos(t *testing.T) {
	var gotPath, gotAuth, gotQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"ok":true,"data":[{"id":1,"name":"demo","full_name":"acme/demo","private":true,"default_branch":"master","description":"a repo"}]}`))
	})
	c, _ := newTestServer(t, handler)

	priv := true
	repos, err := c.SearchRepos(context.Background(), "go", "topic1", "stars", "asc", &priv)
	if err != nil {
		t.Fatalf("SearchRepos() error = %v", err)
	}
	if gotPath != "/api/v1/repos/search" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "token "+testToken {
		t.Errorf("auth = %q", gotAuth)
	}
	for _, kv := range []string{"q=go", "topic=topic1", "sort=stars", "order=asc", "private=true"} {
		if !strings.Contains(gotQuery, kv) {
			t.Errorf("query = %q, want to contain %q", gotQuery, kv)
		}
	}
	if len(repos) != 1 || repos[0].Name != "demo" || repos[0].FullName != "acme/demo" || !repos[0].Private {
		t.Errorf("unexpected repos: %+v", repos)
	}
}

func TestSearchReposOmitsPrivateWhenNil(t *testing.T) {
	var gotQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"ok":true,"data":[]}`))
	})
	c, _ := newTestServer(t, handler)
	if _, err := c.SearchRepos(context.Background(), "go", "", "", "", nil); err != nil {
		t.Fatalf("SearchRepos() error = %v", err)
	}
	if strings.Contains(gotQuery, "private=") {
		t.Errorf("query = %q, want no private param when nil", gotQuery)
	}
}

func TestGetDiff(t *testing.T) {
	var gotPath, gotQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("@@ -1,3 +1,3 @@\n-old\n+new\n"))
	})
	c, _ := newTestServer(t, handler)
	diff, err := c.GetDiff(context.Background(), "acme", "demo", "main..dev")
	if err != nil {
		t.Fatalf("GetDiff() error = %v", err)
	}
	if gotPath != "/api/v1/repos/acme/demo/compare/main..dev" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want empty", gotQuery)
	}
	if !strings.Contains(diff.Text, "+new") {
		t.Errorf("diff.Text = %q, want to contain the diff body", diff.Text)
	}
	if diff.BaseHead != "main..dev" {
		t.Errorf("BaseHead = %q, want main..dev", diff.BaseHead)
	}
}

func TestGetPullDiff(t *testing.T) {
	var gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("diff for pr 3"))
	})
	c, _ := newTestServer(t, handler)
	diff, err := c.GetPullDiff(context.Background(), "acme", "demo", 3)
	if err != nil {
		t.Fatalf("GetPullDiff() error = %v", err)
	}
	if gotPath != "/api/v1/repos/acme/demo/pulls/3.diff" {
		t.Errorf("path = %q", gotPath)
	}
	if diff.Text != "diff for pr 3" {
		t.Errorf("diff.Text = %q", diff.Text)
	}
}

func TestListCommits(t *testing.T) {
	var gotPath, gotQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[{"sha":"abc","commit":{"message":"fix build","author":{"name":"Alice","date":"2024-01-01T00:00:00Z"}},"html_url":"https://x/abc"}]`))
	})
	c, _ := newTestServer(t, handler)
	commits, err := c.ListCommits(context.Background(), "acme", "demo", "main", 2, 25)
	if err != nil {
		t.Fatalf("ListCommits() error = %v", err)
	}
	if gotPath != "/api/v1/repos/acme/demo/commits" {
		t.Errorf("path = %q", gotPath)
	}
	for _, kv := range []string{"sha=main", "page=2", "limit=25"} {
		if !strings.Contains(gotQuery, kv) {
			t.Errorf("query = %q, want to contain %q", gotQuery, kv)
		}
	}
	if len(commits) != 1 || commits[0].SHA != "abc" || commits[0].Message != "fix build" || commits[0].Author != "Alice" {
		t.Errorf("unexpected commits: %+v", commits)
	}
}

func TestListCommitsOmitsBranchWhenEmpty(t *testing.T) {
	var gotQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[]`))
	})
	c, _ := newTestServer(t, handler)
	if _, err := c.ListCommits(context.Background(), "acme", "demo", "", 0, 0); err != nil {
		t.Fatalf("ListCommits() error = %v", err)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want empty when branch empty", gotQuery)
	}
}

func TestListBranches(t *testing.T) {
	var gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`[{"name":"main","protected":true,"default":true,"commit":{"id":"c1"}},{"name":"dev","protected":false,"default":false,"commit":{"id":"c2"}}]`))
	})
	c, _ := newTestServer(t, handler)
	branches, err := c.ListBranches(context.Background(), "acme", "demo")
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	if gotPath != "/api/v1/repos/acme/demo/branches" {
		t.Errorf("path = %q", gotPath)
	}
	if len(branches) != 2 {
		t.Fatalf("expected 2 branches, got %+v", branches)
	}
	if branches[0].Name != "main" || !branches[0].Protected || !branches[0].Default || branches[0].CommitSHA != "c1" {
		t.Errorf("unexpected branch[0]: %+v", branches[0])
	}
	if branches[1].Name != "dev" || branches[1].Protected || branches[1].Default {
		t.Errorf("unexpected branch[1]: %+v", branches[1])
	}
}

func TestListIssues(t *testing.T) {
	var gotPath, gotQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[{"id":1,"number":3,"title":"Issue","body":"b","state":"closed"}]`))
	})
	c, _ := newTestServer(t, handler)
	issues, err := c.ListIssues(context.Background(), "acme", "demo", "closed", 2, 10)
	if err != nil {
		t.Fatalf("ListIssues() error = %v", err)
	}
	if gotPath != "/api/v1/repos/acme/demo/issues" {
		t.Errorf("path = %q", gotPath)
	}
	for _, kv := range []string{"state=closed", "page=2", "limit=10"} {
		if !strings.Contains(gotQuery, kv) {
			t.Errorf("query = %q, want to contain %q", gotQuery, kv)
		}
	}
	if len(issues) != 1 || issues[0].Number != 3 || issues[0].State != "closed" {
		t.Errorf("unexpected issues: %+v", issues)
	}
}

func TestListPullRequests(t *testing.T) {
	var gotPath, gotQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[{"id":1,"number":5,"title":"PR","state":"open"}]`))
	})
	c, _ := newTestServer(t, handler)
	prs, err := c.ListPullRequests(context.Background(), "acme", "demo", "open", 1, 30)
	if err != nil {
		t.Fatalf("ListPullRequests() error = %v", err)
	}
	if gotPath != "/api/v1/repos/acme/demo/pulls" {
		t.Errorf("path = %q", gotPath)
	}
	for _, kv := range []string{"state=open", "page=1", "limit=30"} {
		if !strings.Contains(gotQuery, kv) {
			t.Errorf("query = %q, want to contain %q", gotQuery, kv)
		}
	}
	if len(prs) != 1 || prs[0].Number != 5 || prs[0].Title != "PR" || prs[0].State != "open" {
		t.Errorf("unexpected prs: %+v", prs)
	}
}

// TestGetPullRequest verifies the single-call composition (SPEC 6.1 #12 / M5):
// one tool call must fetch the PR, its changed files AND its combined checks.
func TestGetPullRequest(t *testing.T) {
	var paths []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/files"):
			_, _ = w.Write([]byte(`[{"filename":"a.go","status":"modified","additions":1,"deletions":1,"changes":2}]`))
		case strings.HasSuffix(r.URL.Path, "/status"):
			_, _ = w.Write([]byte(`{"state":"success","statuses":[{"context":"ci","state":"success","target_url":"https://ci/x","description":"ok"}]}`))
		default:
			_, _ = w.Write([]byte(`{"id":1,"number":5,"title":"PR5","state":"open","head":{"sha":"headsha"}}`))
		}
	})
	c, _ := newTestServer(t, handler)

	detail, err := c.GetPullRequest(context.Background(), "acme", "demo", 5)
	if err != nil {
		t.Fatalf("GetPullRequest() error = %v", err)
	}
	if detail.PullRequest.Number != 5 || detail.PullRequest.Title != "PR5" {
		t.Errorf("unexpected PR: %+v", detail.PullRequest)
	}
	if len(detail.Files) != 1 || detail.Files[0].Filename != "a.go" || detail.Files[0].Status != "modified" {
		t.Errorf("unexpected files: %+v", detail.Files)
	}
	if len(detail.Checks) != 1 || detail.Checks[0].Context != "ci" || detail.Checks[0].State != "success" {
		t.Errorf("unexpected checks: %+v", detail.Checks)
	}
	if len(paths) != 3 {
		t.Errorf("expected 3 HTTP calls, got %d: %v", len(paths), paths)
	}
	if paths[0] != "/api/v1/repos/acme/demo/pulls/5" {
		t.Errorf("PR path = %q", paths[0])
	}
	if paths[1] != "/api/v1/repos/acme/demo/pulls/5/files" {
		t.Errorf("files path = %q", paths[1])
	}
	if paths[2] != "/api/v1/repos/acme/demo/commits/headsha/status" {
		t.Errorf("status path = %q", paths[2])
	}
}

func TestListReleases(t *testing.T) {
	var gotPath, gotQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[{"id":1,"tag_name":"v1","name":"V1","body":"notes","draft":false,"prerelease":false,"created_at":"2024-01-01"}]`))
	})
	c, _ := newTestServer(t, handler)
	releases, err := c.ListReleases(context.Background(), "acme", "demo", 1, 20)
	if err != nil {
		t.Fatalf("ListReleases() error = %v", err)
	}
	if gotPath != "/api/v1/repos/acme/demo/releases" {
		t.Errorf("path = %q", gotPath)
	}
	for _, kv := range []string{"page=1", "limit=20"} {
		if !strings.Contains(gotQuery, kv) {
			t.Errorf("query = %q, want to contain %q", gotQuery, kv)
		}
	}
	if len(releases) != 1 || releases[0].TagName != "v1" || releases[0].Name != "V1" {
		t.Errorf("unexpected releases: %+v", releases)
	}
}

func TestGetLatestRelease(t *testing.T) {
	var gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"id":2,"tag_name":"v2","name":"V2","body":"latest","draft":false,"prerelease":false}`))
	})
	c, _ := newTestServer(t, handler)
	rel, err := c.GetLatestRelease(context.Background(), "acme", "demo")
	if err != nil {
		t.Fatalf("GetLatestRelease() error = %v", err)
	}
	if gotPath != "/api/v1/repos/acme/demo/releases/latest" {
		t.Errorf("path = %q", gotPath)
	}
	if rel.TagName != "v2" || rel.Name != "V2" {
		t.Errorf("unexpected release: %+v", rel)
	}
}

func TestSearchReposErrorIsNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	c, _ := newTestServer(t, handler)
	_, err := c.SearchRepos(context.Background(), "q", "", "", "", nil)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindNotFound {
		t.Errorf("expected not-found, got %v", err)
	}
}
