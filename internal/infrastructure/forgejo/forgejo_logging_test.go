package forgejo

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/teran/mcp-forgejo/internal/domain"
)

// captureLogger returns a logrus logger writing JSON to a byte buffer so tests
// can assert on the emitted upstream request log fields.
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

// TestSetLogger verifies SetLogger wires a logger onto the client without
// error and that a nil logger is accepted (no-op).
func TestSetLogger(t *testing.T) {
	c := New(Config{BaseURL: "https://git.example.dev", Token: testToken})
	log, _ := captureLogger(t)
	c.SetLogger(log)
	c.SetLogger(nil) // must not panic; resets logging
}

// TestLogUpstreamFields verifies the upstream request log line carries all
// required fields including the request_id propagated from the context.
func TestLogUpstreamFields(t *testing.T) {
	c := New(Config{BaseURL: "https://git.example.dev", Token: testToken})
	log, buf := captureLogger(t)
	c.SetLogger(log)

	ctx := domain.WithRequestID(context.Background(), "up-rid-1")
	c.logUpstream(ctx, "POST", "/api/v1/repos/acme/demo/contents", 12, 34, 201, 7*time.Millisecond)

	m := lastJSONLine(t, buf)
	if got := m["method"]; got != "POST" {
		t.Errorf("method = %v, want POST", got)
	}
	if got := m["path"]; got != "/api/v1/repos/acme/demo/contents" {
		t.Errorf("path = %v", got)
	}
	if m["request_id"] != "up-rid-1" {
		t.Errorf("request_id = %v, want up-rid-1", m["request_id"])
	}
	if m["in_bytes"] != float64(12) {
		t.Errorf("in_bytes = %v, want 12", m["in_bytes"])
	}
	if m["out_bytes"] != float64(34) {
		t.Errorf("out_bytes = %v, want 34", m["out_bytes"])
	}
	if m["status"] != float64(201) {
		t.Errorf("status = %v, want 201", m["status"])
	}
	if m["duration"] == nil || m["duration"] == "" {
		t.Errorf("duration missing: %v", m["duration"])
	}
}

// TestLogUpstreamNoRequestIDInContext verifies logUpstream does not add a
// request_id field when none is in the context.
func TestLogUpstreamNoRequestIDInContext(t *testing.T) {
	c := New(Config{BaseURL: "https://git.example.dev", Token: testToken})
	log, buf := captureLogger(t)
	c.SetLogger(log)

	c.logUpstream(context.Background(), "GET", "/api/v1/user/orgs", 0, 5, 200, time.Millisecond)

	m := lastJSONLine(t, buf)
	if _, ok := m["request_id"]; ok {
		t.Errorf("unexpected request_id field: %v", m["request_id"])
	}
	if m["method"] != "GET" || m["status"] != float64(200) {
		t.Errorf("unexpected fields: %v", m)
	}
}

// TestLogUpstreamNilLogger verifies logUpstream is a no-op (and does not
// panic) when the client has no logger configured.
func TestLogUpstreamNilLogger(t *testing.T) {
	c := New(Config{BaseURL: "https://git.example.dev", Token: testToken})
	c.logUpstream(context.Background(), "GET", "/x", 0, 0, 200, time.Millisecond) // must not panic
}

// TestRequestIDForwardedAsHeader verifies the per-request ID from the context
// is forwarded upstream as the X-Request-ID header.
func TestRequestIDForwardedAsHeader(t *testing.T) {
	var gotHeader string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"name":"demo","full_name":"acme/demo"}`))
	})
	c, _ := newTestServer(t, handler)

	ctx := domain.WithRequestID(context.Background(), "fwd-1")
	if _, err := c.GetRepository(ctx, "acme", "demo"); err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}
	if gotHeader != "fwd-1" {
		t.Errorf("X-Request-ID header = %q, want fwd-1", gotHeader)
	}
}

// TestRequestIDNotForwardedWhenAbsent verifies no X-Request-ID header is sent
// when the context carries no request ID.
func TestRequestIDNotForwardedWhenAbsent(t *testing.T) {
	var gotHeader string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Request-ID")
		_, _ = w.Write([]byte(`{}`))
	})
	c, _ := newTestServer(t, handler)

	if _, err := c.GetRepository(context.Background(), "acme", "demo"); err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}
	if gotHeader != "" {
		t.Errorf("X-Request-ID header = %q, want empty", gotHeader)
	}
}

// TestDoEmitsUpstreamLog verifies a real client call emits the upstream log
// line carrying method, path, status and the request_id from the context.
func TestDoEmitsUpstreamLog(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"name":"demo","full_name":"acme/demo"}`))
	})
	c, _ := newTestServer(t, handler)
	log, buf := captureLogger(t)
	c.SetLogger(log)

	ctx := domain.WithRequestID(context.Background(), "up-e2e-9")
	if _, err := c.GetRepository(ctx, "acme", "demo"); err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "up-e2e-9") {
		t.Errorf("upstream log missing request_id up-e2e-9: %s", out)
	}
	if !strings.Contains(out, "GET") || !strings.Contains(out, "/api/v1/repos/acme/demo") {
		t.Errorf("upstream log missing method/path: %s", out)
	}
	if !strings.Contains(out, "200") {
		t.Errorf("upstream log missing status: %s", out)
	}
}

// TestUpstreamLogNoTokenLeak verifies the upstream log line never contains the
// PAT even when the token is present in the client config.
func TestUpstreamLogNoTokenLeak(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"name":"demo","full_name":"acme/demo"}`))
	})
	c, _ := newTestServer(t, handler)
	log, buf := captureLogger(t)
	c.SetLogger(log)

	if _, err := c.GetRepository(context.Background(), "acme", "demo"); err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}
	if strings.Contains(buf.String(), testToken) {
		t.Errorf("token leaked in upstream log: %s", buf.String())
	}
}
