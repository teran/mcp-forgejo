package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RunStdio runs the server over the STDIO transport (M2). The MCP protocol
// occupies stdout; logs for this transport go to a file instead (L1).
func RunStdio(ctx context.Context, s *mcp.Server) error {
	return s.Run(ctx, &mcp.StdioTransport{})
}
