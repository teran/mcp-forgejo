package server

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewHTTPHandler returns an http.Handler serving the MCP streamable HTTP/SSE
// transport (M2). The server serves plain HTTP; TLS is the reverse proxy's job
// (S1/N1).
func NewHTTPHandler(s *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return s
	}, &mcp.StreamableHTTPOptions{})
}
