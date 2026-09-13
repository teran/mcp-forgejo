// Package e2e provides an in-process end-to-end test stand for the mcp-forgejo
// MCP server. It boots a REAL Forgejo container (via the go-docker-testsuite
// wrapper), mints a throwaway PAT, builds the MCP server in-process against it,
// serves the server over a real HTTP/SSE transport (httptest), and connects an
// MCP client to it. Tests then drive the client exactly as a remote client
// would — a genuine full-stack round-trip over HTTP with no subprocess/IPC.
//
// The stand is guarded so the whole suite skips gracefully when Docker is
// unavailable or the Forgejo image cannot start, keeping plain `go test ./...`
// green on machines without Docker.
package e2e

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/teran/mcp-forgejo/internal/infrastructure/forgejo"
	"github.com/teran/mcp-forgejo/internal/server"

	forgejoapp "github.com/teran/go-docker-testsuite/applications/forgejo"
)

// Stand is the in-process end-to-end test stand. It owns the lifecycle of the
// Forgejo container, the in-process HTTP/SSE server, the connected MCP client,
// and a LIFO stack of deferred destructive-cleanup functions (so tests can
// release the resources they create in the right order).
type Stand struct {
	ctx    context.Context
	cancel context.CancelFunc

	app     forgejoapp.Forgejo
	baseURL string
	admin   string
	// pat is the throwaway token minted at runtime. It is test-only and MUST
	// never be logged, echoed or otherwise surfaced (S2).
	pat string

	ts     *httptest.Server
	client *mcp.ClientSession

	closeOnce sync.Once
}

// NewStand boots the full stand: a real Forgejo container, a throwaway PAT, the
// in-process MCP server over HTTP/SSE, and a connected MCP client.
//
// It is guarded: when Docker is unavailable or the Forgejo image cannot start it
// calls t.Skipf, so the caller's suite/test skips cleanly instead of failing.
//
// Teardown is owned by the test framework: NewStand registers a t.Cleanup that
// runs deferred destructive cleanup, then closes the MCP client, then closes the
// HTTP server; the Forgejo container is torn down by the docker-testsuite
// wrapper's own t.Cleanup, which runs afterwards (t.Cleanup executes LIFO).
func NewStand(t *testing.T, ctx context.Context) *Stand {
	t.Helper()

	// 1. Boot a real Forgejo instance; its container lifecycle is tied to the
	//    test via the wrapper's t.Cleanup.
	app, err := forgejoapp.NewWithT(t, ctx, forgejoImage)
	if err != nil {
		t.Skipf("skipping e2e: cannot start Forgejo container (is Docker running / image present?): %v", err)
	}

	baseURL := app.MustURL()
	username := app.AdminUsername()
	password := app.AdminPassword()

	// 2. Mint a throwaway PAT for the admin user. The PAT is never logged.
	pat := createPAT(t, ctx, baseURL, username, password)

	// 3. Build the MCP server in-process, bound to the REAL Forgejo instance.
	s, err := server.Build(forgejo.Config{BaseURL: baseURL, Token: pat})
	if err != nil {
		t.Fatalf("Build server: %v", err)
	}

	// 4. Serve MCP over HTTP/SSE in-process. NewHTTPHandler wraps
	//    mcp.NewStreamableHTTPHandler, which creates+connects the server session
	//    itself — so we must NOT call s.Connect here (that would double-connect).
	ts := httptest.NewServer(server.NewHTTPHandler(s))

	// 5. Connect an MCP client over the HTTP/SSE StreamableClientTransport.
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-client"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		ts.Close()
		t.Fatalf("client connect: %v", err)
	}

	stand := &Stand{
		ctx:     ctx,
		app:     app,
		baseURL: baseURL,
		admin:   username,
		pat:     pat,
		ts:      ts,
		client:  cs,
	}

	// Register ordered teardown: deferred destructive cleanup -> client ->
	// HTTP server. The container teardown (registered earlier by the wrapper)
	// runs last via LIFO.
	t.Cleanup(stand.Close)

	return stand
}

// Defer registers a destructive-cleanup function (e.g. an org/repo/file delete
// issued over MCP) to run at the end of the CURRENT test via t.Cleanup. Because
// it is registered on the test's own *testing.T, the cleanup runs while that
// test is still alive (so a failing cleanup correctly fails the test) and BEFORE
// the suite-level Close() tears down the MCP client and HTTP server. Multiple
// defers run in reverse registration order (LIFO), so a test can create several
// resources and rely on them being released in the right order.
func (s *Stand) Defer(t *testing.T, fn func()) {
	t.Helper()
	t.Cleanup(fn)
}

// Close tears down the stand's MCP client and HTTP server (in that order),
// releasing the in-process server session. It is idempotent and safe to call
// from t.Cleanup (NewStand registers it automatically). The Forgejo container is
// owned by the docker-testsuite wrapper's t.Cleanup and is closed separately;
// destructive MCP cleanup is handled by per-test Defer registrations.
func (s *Stand) Close() {
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.client != nil {
			_ = s.client.Close()
		}
		if s.ts != nil {
			s.ts.Close()
		}
	})
}

// BaseURL returns the Forgejo REST base URL the stand is bound to.
func (s *Stand) BaseURL() string { return s.baseURL }

// Admin returns the Forgejo admin username.
func (s *Stand) Admin() string { return s.admin }

// Call invokes the named MCP tool over the live HTTP transport and returns the
// raw tool result. It does NOT fail the test; callers decide how to handle
// errors (see CallOK / CallJSON for fail-fast variants).
func (s *Stand) Call(t *testing.T, tool string, args map[string]any) (*mcp.CallToolResult, error) {
	t.Helper()
	return s.client.CallTool(s.ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
}

// CallOK invokes the named tool and fails the test on any protocol or tool-level
// error, returning the successful result. Prefer it for happy-path assertions.
func (s *Stand) CallOK(t *testing.T, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := s.Call(t, tool, args)
	require.NoErrorf(t, err, "CallTool(%s) error", tool)
	require.Falsef(t, res.IsError, "CallTool(%s) returned error: %s", tool, textOf(res))
	return res
}

// CallJSON invokes the named tool, fails the test on error, and decodes the
// tool's JSON text content into T. Tools that legitimately return no content
// (e.g. deletes) yield the zero value of T.
func CallJSON[T any](s *Stand, t *testing.T, tool string, args map[string]any) T {
	t.Helper()
	var zero T
	res := s.CallOK(t, tool, args)
	if len(res.Content) == 0 {
		return zero
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	require.Truef(t, ok, "CallTool(%s): expected TextContent, got %T", tool, res.Content[0])
	require.NoErrorf(t, json.Unmarshal([]byte(tc.Text), &zero),
		"CallTool(%s): decode result (text=%s)", tool, truncate(tc.Text))
	return zero
}

// ListTools returns the full tool surface advertised by the server over
// tools/list.
func (s *Stand) ListTools(t *testing.T) []*mcp.Tool {
	t.Helper()
	res, err := s.client.ListTools(s.ctx, &mcp.ListToolsParams{})
	require.NoError(t, err, "ListTools")
	return res.Tools
}
