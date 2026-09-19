//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// forgejoImage is the Forgejo container image used by the e2e stand.
const forgejoImage = "codeberg.org/forgejo/forgejo:16.0.4"

// createPAT mints a throwaway personal access token for the admin user via the
// Forgejo REST API using HTTP Basic auth, and returns the raw token value.
// The returned token is a test-only secret; callers must never log it.
func createPAT(t *testing.T, ctx context.Context, baseURL, username, password string) string {
	t.Helper()

	body, err := json.Marshal(map[string]any{"name": "e2e", "scopes": []string{"all"}})
	if err != nil {
		t.Fatalf("marshal token request: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/v1/users/"+username+"/tokens", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new token request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(username, password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create PAT: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("create PAT: status %d, body %s", resp.StatusCode, raw)
	}

	var tok struct {
		SHA1  string `json:"sha1"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil {
		t.Fatalf("decode PAT response: %v", err)
	}
	switch {
	case tok.SHA1 != "":
		return tok.SHA1
	case tok.Token != "":
		return tok.Token
	default:
		t.Fatalf("create PAT: no token in response: %s", raw)
		return ""
	}
}

// authedClient returns an *http.Client whose RoundTripper injects the given PAT
// as an `Authorization: Bearer <pat>` header on every request before forwarding
// to the default transport. The MCP server's HTTP/SSE transport requires this
// header (M6); a RoundTripper covers both POST and the standalone SSE GET. The
// pat is never logged (S2).
func authedClient(pat string) *http.Client {
	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+pat)
		return http.DefaultTransport.RoundTrip(req)
	})
	return &http.Client{Transport: rt}
}

// roundTripperFunc adapts a plain function to the http.RoundTripper interface.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// textOf returns the first text content of a tool result, or "" if none.
func textOf(res *mcp.CallToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// truncate bounds the length of a string used in error messages.
func truncate(s string) string {
	const maxLen = 500
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
