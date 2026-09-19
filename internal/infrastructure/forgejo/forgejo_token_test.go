package forgejo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/teran/mcp-forgejo/internal/domain"
)

// TestContextTokenOverridesConfigToken verifies that when the request context
// carries a token (via domain.WithToken), the outbound Forgejo request uses
// that token in its Authorization header instead of the base config token.
func TestContextTokenOverridesConfigToken(t *testing.T) {
	var mu sync.Mutex
	var gotAuth string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1}`))
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	c := NewWithClient(Config{BaseURL: ts.URL, Token: testToken}, ts.Client())

	ctx := domain.WithToken(context.Background(), "ctx-token-123")
	if _, err := c.GetRepository(ctx, "acme", "demo"); err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotAuth != "token ctx-token-123" {
		t.Errorf("auth = %q, want %q", gotAuth, "token ctx-token-123")
	}
}

// TestContextWithoutTokenFallsBackToConfigToken verifies that when the request
// context carries no token, the outbound Forgejo request falls back to the base
// config token.
func TestContextWithoutTokenFallsBackToConfigToken(t *testing.T) {
	var mu sync.Mutex
	var gotAuth string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1}`))
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	c := NewWithClient(Config{BaseURL: ts.URL, Token: testToken}, ts.Client())

	if _, err := c.GetRepository(context.Background(), "acme", "demo"); err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotAuth != "token "+testToken {
		t.Errorf("auth = %q, want %q", gotAuth, "token "+testToken)
	}
}
