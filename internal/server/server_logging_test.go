package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"

	"github.com/teran/mcp-forgejo/internal/domain"
	"github.com/teran/mcp-forgejo/internal/infrastructure/forgejo"
)

// captureLogger returns a logrus logger writing JSON to a byte buffer so tests
// can assert on the emitted fields.
func captureLogger(t *testing.T) (*logrus.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	log := logrus.New()
	log.SetOutput(&buf)
	log.SetFormatter(&logrus.JSONFormatter{})
	log.SetLevel(logrus.TraceLevel)
	return log, &buf
}

// lastJSONLine decodes the last log line from buf into a field map. JSON
// numbers decode as float64.
func lastJSONLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	out := strings.TrimRight(buf.String(), "\n")
	if out == "" {
		t.Fatal("no log lines captured")
	}
	lines := strings.Split(out, "\n")
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		t.Fatalf("decode last log line: %v (line=%q)", err, lines[len(lines)-1])
	}
	return m
}

func fieldString(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("log line missing field %q: %v", key, m)
	}
	s, _ := v.(string)
	return s
}

// ============================================================================
// request_id (L9/G11)
// ============================================================================

// TestNewRequestID verifies a fresh request ID is non-empty, hex-encoded and
// unique per call.
func TestNewRequestID(t *testing.T) {
	a := newRequestID()
	if a == "" {
		t.Fatal("newRequestID() returned empty")
	}
	b := newRequestID()
	if a == b {
		t.Errorf("newRequestID() not unique: %q == %q", a, b)
	}
	if len(a) != 16 {
		t.Errorf("newRequestID() length = %d, want 16 (hex of 8 bytes)", len(a))
	}
	for _, c := range a {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("newRequestID() contains non-hex char %q", c)
			break
		}
	}
}

// ============================================================================
// source extraction (L8)
// ============================================================================

// TestSourceFromRequest verifies the client-IP chain priority:
// X-Real-IP -> X-Forwarded-For -> RemoteAddr, joined by ", ".
func TestSourceFromRequest(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		want       string
	}{
		{"x-real-ip only", map[string]string{"X-Real-IP": "10.1.1.1"}, "127.0.0.1:5", "10.1.1.1"},
		{"x-forwarded-for only", map[string]string{"X-Forwarded-For": "10.2.2.2"}, "127.0.0.1:5", "10.2.2.2"},
		{"both headers, real first", map[string]string{"X-Real-IP": "10.1.1.1", "X-Forwarded-For": "10.2.2.2, 10.3.3.3"}, "127.0.0.1:5", "10.1.1.1, 10.2.2.2, 10.3.3.3"},
		{"remote addr fallback", map[string]string{}, "203.0.113.9:1234", "203.0.113.9:1234"},
		{"all empty", map[string]string{}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}
			if got := sourceFromRequest(r); got != tt.want {
				t.Errorf("sourceFromRequest() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestClientIPFromHeader verifies the header-only chain used by the receiving
// middleware (no RemoteAddr available there).
func TestClientIPFromHeader(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"no headers", map[string]string{}, ""},
		{"real only", map[string]string{"X-Real-IP": "10.1.1.1"}, "10.1.1.1"},
		{"forwarded only", map[string]string{"X-Forwarded-For": "10.2.2.2, 10.3.3.3"}, "10.2.2.2, 10.3.3.3"},
		{"real then forwarded", map[string]string{"X-Real-IP": "10.1.1.1", "X-Forwarded-For": "10.2.2.2"}, "10.1.1.1, 10.2.2.2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tt.headers {
				h.Set(k, v)
			}
			if got := clientIPFromHeader(h); got != tt.want {
				t.Errorf("clientIPFromHeader() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ============================================================================
// args redaction (L8 / no secret leaks)
// ============================================================================

// TestRedactArgs verifies sensitive argument keys are replaced with
// "[REDACTED]" while non-sensitive values are preserved.
func TestRedactArgs(t *testing.T) {
	raw := json.RawMessage(`{"owner":"acme","repo":"demo","token":"SECRET","password":"hunter2","body":"hello"}`)
	got, ok := redactArgs(raw).(map[string]any)
	if !ok {
		t.Fatalf("redactArgs() = %T, want map[string]any", redactArgs(raw))
	}
	if got["owner"] != "acme" || got["repo"] != "demo" || got["body"] != "hello" {
		t.Errorf("non-sensitive fields altered: %v", got)
	}
	if got["token"] != "[REDACTED]" {
		t.Errorf("token not redacted: %v", got["token"])
	}
	if got["password"] != "[REDACTED]" {
		t.Errorf("password not redacted: %v", got["password"])
	}
}

// TestRedactArgsCaseInsensitive verifies sensitive-key matching is
// case-insensitive.
func TestRedactArgsCaseInsensitive(t *testing.T) {
	raw := json.RawMessage(`{"Owner":"acme","Token":"S","PASSWORD":"x","ApiKey":"y"}`)
	got := redactArgs(raw).(map[string]any)
	if got["Token"] != "[REDACTED]" || got["PASSWORD"] != "[REDACTED]" || got["ApiKey"] != "[REDACTED]" {
		t.Errorf("case-insensitive redaction failed: %v", got)
	}
	if got["Owner"] != "acme" {
		t.Errorf("non-sensitive key redacted by mistake: %v", got)
	}
}

// TestRedactArgsNonObject verifies non-object or invalid JSON is passed through
// as the raw string (never crashes, never fabricates a secret).
func TestRedactArgsNonObject(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`"just a string"`),
		json.RawMessage(`[1,2,3]`),
		json.RawMessage(`{not json`),
		json.RawMessage(""),
	} {
		if got := redactArgs(raw); got != string(raw) {
			t.Errorf("redactArgs(%q) = %v (%T), want raw string %q", raw, got, got, string(raw))
		}
	}
}

// TestRedactArgsSecretDoesNotEchoToken verifies that even when an argument
// value happens to be the token itself under a sensitive key, the log output
// never contains the raw token.
func TestRedactArgsSecretDoesNotEchoToken(t *testing.T) {
	raw := json.RawMessage(`{"token":"` + testToken + `"}`)
	out, _ := json.Marshal(redactArgs(raw))
	if strings.Contains(string(out), testToken) {
		t.Errorf("token leaked in redacted args: %s", out)
	}
	if !strings.Contains(string(out), "[REDACTED]") {
		t.Errorf("expected [REDACTED] in output: %s", out)
	}
}

// ============================================================================
// per-request tool logging (L8)
// ============================================================================

// TestLogToolCallFields verifies the tool-call log line carries all required
// fields and that args are redacted.
func TestLogToolCallFields(t *testing.T) {
	log, buf := captureLogger(t)
	ctx := domain.WithRequestID(domain.WithSource(context.Background(), "1.2.3.4"), "rid-1")
	args := json.RawMessage(`{"owner":"acme","token":"SECRET"}`)
	start := time.Now().Add(-10 * time.Millisecond)
	logToolCall(log, ctx, "forgejo_repo_get", args, start, 42, nil)

	m := lastJSONLine(t, buf)
	if got := fieldString(t, m, "tool"); got != "forgejo_repo_get" {
		t.Errorf("tool = %q", got)
	}
	if got := fieldString(t, m, "request_id"); got != "rid-1" {
		t.Errorf("request_id = %q", got)
	}
	if got := fieldString(t, m, "source"); got != "1.2.3.4" {
		t.Errorf("source = %q", got)
	}
	if got := fieldString(t, m, "outcome"); got != "ok" {
		t.Errorf("outcome = %q, want ok", got)
	}
	if m["in_bytes"] != float64(len(args)) {
		t.Errorf("in_bytes = %v, want %d", m["in_bytes"], len(args))
	}
	if m["out_bytes"] != float64(42) {
		t.Errorf("out_bytes = %v, want 42", m["out_bytes"])
	}
	if m["duration"] == nil || fieldString(t, m, "duration") == "" {
		t.Errorf("duration missing: %v", m)
	}
	argsMap, ok := m["args"].(map[string]any)
	if !ok {
		t.Fatalf("args = %T, want map", m["args"])
	}
	if argsMap["owner"] != "acme" {
		t.Errorf("args.owner = %v", argsMap["owner"])
	}
	if argsMap["token"] != "[REDACTED]" {
		t.Errorf("args.token = %v, want [REDACTED]", argsMap["token"])
	}
}

// TestLogToolCallErrorOutcome verifies the outcome field reflects an error.
func TestLogToolCallErrorOutcome(t *testing.T) {
	log, buf := captureLogger(t)
	ctx := domain.WithRequestID(context.Background(), "rid-2")
	logToolCall(log, ctx, "forgejo_repo_get", nil, time.Now(), 0, domain.NewForgejoError(domain.KindNotFound, "nope"))
	if got := fieldString(t, lastJSONLine(t, buf), "outcome"); got != "error" {
		t.Errorf("outcome = %q, want error", got)
	}
}

// TestLogToolCallNilLogger verifies the helper is a no-op with a nil logger
// (no panic, nothing written).
func TestLogToolCallNilLogger(t *testing.T) {
	logToolCall(nil, context.Background(), "t", nil, time.Now(), 0, nil) // must not panic
}

// TestOutcome verifies the outcome helper maps error/nil correctly.
func TestOutcome(t *testing.T) {
	if outcome(nil) != "ok" {
		t.Errorf("outcome(nil) = %q, want ok", outcome(nil))
	}
	if outcome(domain.NewForgejoError(domain.KindTransient, "x")) != "error" {
		t.Errorf("outcome(err) = %q, want error", outcome(domain.NewForgejoError(domain.KindTransient, "x")))
	}
}

// ============================================================================
// receiving middleware (L9/G11)
// ============================================================================

func callToolRequest(name string, args string, h http.Header) mcp.Request {
	return &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
		Params: &mcp.CallToolParamsRaw{Name: name, Arguments: json.RawMessage(args)},
		Extra:  &mcp.RequestExtra{Header: h},
	}
}

// TestRequestContextMiddlewareGeneratesRequestID verifies that, when no
// incoming X-Request-ID is present, the middleware generates a fresh ID and
// injects it into the context passed to next.
func TestRequestContextMiddlewareGeneratesRequestID(t *testing.T) {
	log, _ := captureLogger(t)
	var gotID, gotSource string
	next := func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		gotID, _ = domain.RequestIDFromContext(ctx)
		gotSource, _ = domain.SourceFromContext(ctx)
		return &mcp.CallToolResult{}, nil
	}
	wrapped := requestContextMiddleware(log)(next)
	_, err := wrapped(context.Background(), "tools/call", callToolRequest("forgejo_repo_get", `{}`, nil))
	if err != nil {
		t.Fatalf("middleware error = %v", err)
	}
	if gotID == "" {
		t.Error("middleware did not inject a generated request_id")
	}
	if gotSource != "STDIO" {
		t.Errorf("source = %q, want STDIO for nil header", gotSource)
	}
}

// TestRequestContextMiddlewareReusesIncomingID verifies the incoming
// X-Request-ID header is reused rather than regenerated.
func TestRequestContextMiddlewareReusesIncomingID(t *testing.T) {
	log, _ := captureLogger(t)
	var gotID string
	next := func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		gotID, _ = domain.RequestIDFromContext(ctx)
		return &mcp.CallToolResult{}, nil
	}
	h := http.Header{}
	h.Set("X-Request-ID", "incoming-1")
	wrapped := requestContextMiddleware(log)(next)
	_, err := wrapped(context.Background(), "tools/call", callToolRequest("forgejo_repo_get", `{}`, h))
	if err != nil {
		t.Fatalf("middleware error = %v", err)
	}
	if gotID != "incoming-1" {
		t.Errorf("request_id = %q, want reuse of incoming-1", gotID)
	}
}

// TestRequestContextMiddlewareExtractsSourceFromHeader verifies the middleware
// computes source from the HTTP headers when present.
func TestRequestContextMiddlewareExtractsSourceFromHeader(t *testing.T) {
	log, _ := captureLogger(t)
	var gotSource string
	next := func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		gotSource, _ = domain.SourceFromContext(ctx)
		return &mcp.CallToolResult{}, nil
	}
	h := http.Header{}
	h.Set("X-Real-IP", "10.9.9.9")
	h.Set("X-Forwarded-For", "10.8.8.8")
	wrapped := requestContextMiddleware(log)(next)
	if _, err := wrapped(context.Background(), "tools/call", callToolRequest("forgejo_repo_get", `{}`, h)); err != nil {
		t.Fatalf("middleware error = %v", err)
	}
	if gotSource != "10.9.9.9, 10.8.8.8" {
		t.Errorf("source = %q, want header chain", gotSource)
	}
}

// TestRequestContextMiddlewareReusesContextRequestID verifies a request ID
// already present in the context (e.g. injected by the HTTP wrapper) is kept.
func TestRequestContextMiddlewareReusesContextRequestID(t *testing.T) {
	log, _ := captureLogger(t)
	var gotID string
	next := func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		gotID, _ = domain.RequestIDFromContext(ctx)
		return &mcp.CallToolResult{}, nil
	}
	wrapped := requestContextMiddleware(log)(next)
	ctx := domain.WithRequestID(context.Background(), "preset-7")
	if _, err := wrapped(ctx, "tools/call", callToolRequest("forgejo_repo_get", `{}`, nil)); err != nil {
		t.Fatalf("middleware error = %v", err)
	}
	if gotID != "preset-7" {
		t.Errorf("request_id = %q, want preset-7", gotID)
	}
}

// TestRequestContextMiddlewareLogsToolCall verifies the middleware emits a
// per-request tool log line (with request_id and tool name) when the incoming
// request is a tool call.
func TestRequestContextMiddlewareLogsToolCall(t *testing.T) {
	log, buf := captureLogger(t)
	next := func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	}
	h := http.Header{}
	h.Set("X-Request-ID", "logme")
	wrapped := requestContextMiddleware(log)(next)
	if _, err := wrapped(context.Background(), "tools/call", callToolRequest("forgejo_repo_get", `{"owner":"acme"}`, h)); err != nil {
		t.Fatalf("middleware error = %v", err)
	}
	m := lastJSONLine(t, buf)
	if got := fieldString(t, m, "tool"); got != "forgejo_repo_get" {
		t.Errorf("tool = %q", got)
	}
	if got := fieldString(t, m, "request_id"); got != "logme" {
		t.Errorf("request_id = %q", got)
	}
}

// TestRequestContextMiddlewareLogsOutBytes verifies the middleware computes a
// non-zero out_bytes from a non-nil tool result (kills the result==nil and
// marshal-error negation mutants in the tool-call branch).
func TestRequestContextMiddlewareLogsOutBytes(t *testing.T) {
	log, buf := captureLogger(t)
	next := func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "hello"}},
		}, nil
	}
	wrapped := requestContextMiddleware(log)(next)
	if _, err := wrapped(context.Background(), "tools/call", callToolRequest("forgejo_repo_get", `{}`, nil)); err != nil {
		t.Fatalf("middleware error = %v", err)
	}
	m := lastJSONLine(t, buf)
	if out, ok := m["out_bytes"].(float64); !ok || out <= 0 {
		t.Errorf("out_bytes = %v, want > 0", m["out_bytes"])
	}
}

// TestRequestContextMiddlewareNoLogForNonTool verifies non-tool methods do not
// produce a tool-call log line.
func TestRequestContextMiddlewareNoLogForNonTool(t *testing.T) {
	log, buf := captureLogger(t)
	next := func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	}
	wrapped := requestContextMiddleware(log)(next)
	// A request whose params are not *CallToolParamsRaw (e.g. ping).
	req := &mcp.ServerRequest[*mcp.PingParams]{Extra: nil}
	if _, err := wrapped(context.Background(), "ping", req); err != nil {
		t.Fatalf("middleware error = %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("unexpected log line for non-tool method: %s", buf.String())
	}
}

// TestRequestContextMiddlewareNilLoggerNoLog verifies the middleware does not
// log (but still injects context) when the logger is nil.
func TestRequestContextMiddlewareNilLoggerNoLog(t *testing.T) {
	var gotID string
	next := func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		gotID, _ = domain.RequestIDFromContext(ctx)
		return &mcp.CallToolResult{}, nil
	}
	wrapped := requestContextMiddleware(nil)(next)
	if _, err := wrapped(context.Background(), "tools/call", callToolRequest("forgejo_repo_get", `{}`, nil)); err != nil {
		t.Fatalf("middleware error = %v", err)
	}
	if gotID == "" {
		t.Error("request_id not injected with nil logger")
	}
}

// ============================================================================
// end-to-end: BuildWithLogger emits request_id on a real tool call (L7/L9)
// ============================================================================

// TestBuildWithLoggerEmitsRequestID verifies that a real tool call through the
// built server produces a log line carrying a request_id (injected by the
// receiving middleware). The buffer is replaced by captureLogger's so the
// assertion reads the actual emitted lines.
func TestBuildWithLoggerEmitsRequestID(t *testing.T) {
	log, buf := captureLogger(t)

	ts := httptest.NewServer(mockForgejo())
	t.Cleanup(ts.Close)
	s, err := BuildWithLogger(forgejo.Config{BaseURL: ts.URL, Token: testToken}, log)
	if err != nil {
		t.Fatalf("BuildWithLogger() error = %v", err)
	}

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	var repo domain.Repository
	callTool(t, cs, "forgejo_repo_get", map[string]any{"owner": "acme", "repo": "demo"}, &repo)
	if repo.FullName != "acme/demo" {
		t.Fatalf("unexpected repo: %+v", repo)
	}

	out := buf.String()
	if !strings.Contains(out, "request_id") {
		t.Errorf("expected request_id in tool-call logs, got: %s", out)
	}
}

// TestBuildWithLoggerRegistersSlogLogger verifies BuildWithLogger wires a slog
// logger into the server options (L7) without erroring, and that a disabled
// (nil) Build still works for backward compatibility.
func TestBuildWithLoggerRegistersSlogLogger(t *testing.T) {
	log, _ := captureLogger(t)
	s, err := BuildWithLogger(forgejo.Config{BaseURL: "https://x", Token: "t"}, log)
	if err != nil {
		t.Fatalf("BuildWithLogger() error = %v", err)
	}
	if s == nil {
		t.Fatal("BuildWithLogger() returned nil server")
	}
	// Build (nil logger path) must still construct a valid server.
	s2, err := Build(forgejo.Config{BaseURL: "https://x", Token: "t"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if s2 == nil {
		t.Fatal("Build() returned nil server")
	}
}
