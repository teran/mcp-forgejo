package server

import (
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teran/mcp-forgejo/domain"
)

// NewHTTPHandler returns an http.Handler serving the MCP streamable HTTP/SSE
// transport (M2) behind a Bearer-token authentication middleware (M6). Every
// incoming request must carry an `Authorization: Bearer <token>` header; the
// token is injected into the request context so the Forgejo client authenticates
// the outbound upstream request with it (per-request token override). Requests
// without a valid Bearer header are rejected with 401 Unauthorized. The server
// serves plain HTTP; TLS is the reverse proxy's job (S1/N1).
func NewHTTPHandler(s *mcp.Server) http.Handler {
	return bearerAuth(mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return s
	}, &mcp.StreamableHTTPOptions{}))
}

// bearerAuth wraps next with an authentication middleware that requires an
// `Authorization: Bearer <token>` header (scheme case-insensitive, non-empty
// token). On success it injects the token into the request context via
// domain.WithToken and continues to the wrapped handler.
func bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token, ok := bearerToken(auth)
		if !ok {
			http.Error(w, "unauthorized: missing or invalid Bearer token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(domain.WithToken(r.Context(), token)))
	})
}

// bearerToken parses an Authorization header value and returns the token when
// it uses the Bearer scheme (case-insensitive) with a non-empty token. It
// returns ok=false for missing, malformed, non-Bearer or empty-token values.
func bearerToken(auth string) (string, bool) {
	scheme, token, found := strings.Cut(auth, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	return token, true
}
