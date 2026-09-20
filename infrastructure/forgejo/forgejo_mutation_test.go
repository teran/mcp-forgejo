package forgejo

// Targeted tests that strengthen coverage of request-core helpers and
// status-boundary branches so that previously live gremlins mutants are
// killed (gremlins efficacy/mcover gate). See AGENTS.md / SPEC §8.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/teran/mcp-forgejo/domain"
)

// IsPullRequestMerged enters the default (non-204/404) branch. Status 200 must
// be treated as merged (kills the `>= 200` negation + boundary mutants) and
// status 300 must be treated as an error (kills the `< 300` boundary mutant).
func TestIsPullRequestMergedStatus200(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	c, _ := newTestServer(t, handler)

	merged, err := c.IsPullRequestMerged(context.Background(), "acme", "demo", 5)
	if err != nil {
		t.Fatalf("IsPullRequestMerged() error = %v", err)
	}
	if !merged {
		t.Error("expected merged=true for 200")
	}
}

func TestIsPullRequestMergedStatus300(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
	})
	c, _ := newTestServer(t, handler)

	merged, err := c.IsPullRequestMerged(context.Background(), "acme", "demo", 5)
	if err == nil {
		t.Fatal("expected error for 300")
	}
	if merged {
		t.Error("expected merged=false for 300")
	}
}

// UploadReleaseAsset forwards the request ID from the context and maps a 300
// response onto the error taxonomy.
func TestUploadReleaseAssetForwardsRequestID(t *testing.T) {
	var gotHeader string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Request-ID")
		_, _ = w.Write([]byte(`{"id":1}`))
	})
	c, _ := newTestServer(t, handler)

	ctx := domain.WithRequestID(context.Background(), "asset-rid-1")
	if _, err := c.UploadReleaseAsset(ctx, "acme", "demo", 5, "f.bin", []byte("x")); err != nil {
		t.Fatalf("UploadReleaseAsset() error = %v", err)
	}
	if gotHeader != "asset-rid-1" {
		t.Errorf("X-Request-ID header = %q, want asset-rid-1", gotHeader)
	}
}

func TestUploadReleaseAssetStatus300(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
		// A well-formed body must still be rejected: status 300 is not 2xx.
		_, _ = w.Write([]byte(`{"id":1}`))
	})
	c, _ := newTestServer(t, handler)

	_, err := c.UploadReleaseAsset(context.Background(), "acme", "demo", 5, "f.bin", []byte("x"))
	if err == nil {
		t.Fatal("expected error for 300")
	}
}

// skipANSI direct unit tests exercise the CSI final-byte boundaries and the
// unterminated-CSI return value precisely.
func TestSkipANSI(t *testing.T) {
	tests := []struct {
		name string
		s    string
		i    int
		want int
	}{
		{"non-esc returns next index", "hello", 0, 1},
		{"simple ESC sequence returns next index", "\x1bcX", 0, 1},
		{"CSI final byte at lower boundary 0x40", "\x1b[@rest", 0, 2},
		{"CSI final byte at upper boundary 0x7e", "\x1b[~rest", 0, 2},
		{"unterminated CSI returns len-1", "\x1b[12", 0, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := skipANSI(tt.s, tt.i); got != tt.want {
				t.Errorf("skipANSI(%q, %d) = %d, want %d", tt.s, tt.i, got, tt.want)
			}
		})
	}
}

// do (via GetRepository) maps a 300 response onto the error taxonomy.
func TestGetRepositoryStatus300(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
		// A well-formed body must still be rejected: status 300 is not 2xx.
		_, _ = w.Write([]byte(`{"id":1,"name":"demo","full_name":"acme/demo"}`))
	})
	c, _ := newTestServer(t, handler)

	if _, err := c.GetRepository(context.Background(), "acme", "demo"); err == nil {
		t.Fatal("expected error for 300")
	}
}

// doJSON (via CreateRepository) forwards the request ID and maps a 300
// response onto the error taxonomy.
func TestCreateRepositoryForwardsRequestID(t *testing.T) {
	var gotHeader string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Request-ID")
		_, _ = w.Write([]byte(`{"id":1,"name":"demo","full_name":"alice/demo"}`))
	})
	c, _ := newTestServer(t, handler)

	ctx := domain.WithRequestID(context.Background(), "repo-rid-1")
	if _, err := c.CreateRepository(ctx, domain.CreateRepositoryInput{Name: "demo"}); err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	if gotHeader != "repo-rid-1" {
		t.Errorf("X-Request-ID header = %q, want repo-rid-1", gotHeader)
	}
}

func TestCreateRepositoryStatus300(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
		// A well-formed body must still be rejected: status 300 is not 2xx.
		_, _ = w.Write([]byte(`{"id":1,"name":"demo","full_name":"alice/demo"}`))
	})
	c, _ := newTestServer(t, handler)

	if _, err := c.CreateRepository(context.Background(), domain.CreateRepositoryInput{Name: "demo"}); err == nil {
		t.Fatal("expected error for 300")
	}
}

// doText (via GetDiff) forwards the request ID and maps a 300 response onto
// the error taxonomy.
func TestGetDiffForwardsRequestID(t *testing.T) {
	var gotHeader string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("@@ -1 +1 @@\n"))
	})
	c, _ := newTestServer(t, handler)

	ctx := domain.WithRequestID(context.Background(), "diff-rid-1")
	if _, err := c.GetDiff(ctx, "acme", "demo", "main..dev"); err != nil {
		t.Fatalf("GetDiff() error = %v", err)
	}
	if gotHeader != "diff-rid-1" {
		t.Errorf("X-Request-ID header = %q, want diff-rid-1", gotHeader)
	}
}

func TestGetDiffStatus300(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
	})
	c, _ := newTestServer(t, handler)

	if _, err := c.GetDiff(context.Background(), "acme", "demo", "main..dev"); err == nil {
		t.Fatal("expected error for 300")
	}
}

// TestNewUsesDefaultTransport verifies New() (nil injected client) builds a
// client that performs real requests over resty's default transport.
func TestNewUsesDefaultTransport(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"name":"demo","full_name":"acme/demo"}`))
	}))
	t.Cleanup(ts.Close)

	c := New(Config{BaseURL: ts.URL, Token: testToken})
	repo, err := c.GetRepository(context.Background(), "acme", "demo")
	if err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}
	if repo.Name != "demo" {
		t.Errorf("repo.Name = %q, want demo", repo.Name)
	}
}

// requestBodyBytes distinguishes nil, []byte and struct bodies (kills the
// `body == nil` negation mutant).
func TestRequestBodyBytes(t *testing.T) {
	if got := requestBodyBytes(nil); got != 0 {
		t.Errorf("requestBodyBytes(nil) = %d, want 0", got)
	}
	if got := requestBodyBytes([]byte("hello")); got != 5 {
		t.Errorf("requestBodyBytes([]byte) = %d, want 5", got)
	}
	if got := requestBodyBytes(struct{ A string }{A: "x"}); got != int64(len(`{"A":"x"}`)) {
		t.Errorf("requestBodyBytes(struct) = %d, want %d", got, len(`{"A":"x"}`))
	}
}
