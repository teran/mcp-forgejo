package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teran/mcp-forgejo/internal/infrastructure/forgejo"
)

// buildHTTPHandler builds an mcp server and returns the NewHTTPHandler output
// for auth middleware tests.
func buildHTTPHandler(t *testing.T) http.Handler {
	t.Helper()
	s, err := Build(forgejo.Config{BaseURL: "https://x", Token: "t"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	return NewHTTPHandler(s)
}

// mcpInitRequest builds a well-formed MCP initialize POST. When auth is empty no
// Authorization header is set, otherwise the header carries auth verbatim.
func mcpInitRequest(t *testing.T, auth string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	return req
}

// TestHTTPHandlerRejectsMissingAuth verifies that a request with no
// Authorization header is rejected with 401 Unauthorized by the middleware.
func TestHTTPHandlerRejectsMissingAuth(t *testing.T) {
	h := buildHTTPHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, mcpInitRequest(t, ""))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("missing auth: status = %d, want 401", rec.Code)
	}
}

// TestHTTPHandlerRejectsNonBearerAuth verifies that an Authorization header not
// using the Bearer scheme is rejected with 401.
func TestHTTPHandlerRejectsNonBearerAuth(t *testing.T) {
	h := buildHTTPHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, mcpInitRequest(t, "Basic abc123"))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("non-bearer auth: status = %d, want 401", rec.Code)
	}
}

// TestHTTPHandlerRejectsEmptyBearer verifies that a Bearer header with an empty
// token is rejected with 401.
func TestHTTPHandlerRejectsEmptyBearer(t *testing.T) {
	h := buildHTTPHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, mcpInitRequest(t, "Bearer "))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("empty bearer: status = %d, want 401", rec.Code)
	}
}

// TestHTTPHandlerAcceptsBearerToken verifies that a valid Bearer token passes
// the auth middleware. The downstream handler may still return a 4xx if the
// body is not a proper MCP request, but it must NOT be the middleware's 401.
func TestHTTPHandlerAcceptsBearerToken(t *testing.T) {
	h := buildHTTPHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, mcpInitRequest(t, "Bearer abc123"))

	if rec.Code == http.StatusUnauthorized {
		t.Error("valid bearer token must not be rejected with 401")
	}
}
