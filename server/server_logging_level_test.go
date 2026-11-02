package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"

	"github.com/teran/mcp-forgejo/domain"
	"github.com/teran/mcp-forgejo/infrastructure/forgejo"
)

// captureLoggerAtLevel returns a logrus logger writing JSON to a byte buffer at
// the given level, so tests can assert which level a line survives at (L08:
// the tool-call access log must be visible at the default `info` level).
func captureLoggerAtLevel(t *testing.T, level logrus.Level) (*logrus.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	log := logrus.New()
	log.SetOutput(&buf)
	log.SetFormatter(&logrus.JSONFormatter{})
	log.SetLevel(level)
	return log, &buf
}

// findToolCallLine scans every captured JSON line and returns the first whose
// msg is "tool call", together with its field map. If none is present the test
// fails — this is what makes the test red while the line is emitted at Debug
// level (a Debug line is dropped at Info level).
func findToolCallLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	out := strings.TrimRight(buf.String(), "\n")
	if out == "" {
		t.Fatalf("no log lines captured at all")
	}
	for line := range strings.SplitSeq(out, "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("decode log line: %v (line=%q)", err, line)
		}
		if msg, _ := m["msg"].(string); msg == "tool call" {
			return m
		}
	}
	t.Fatalf("no log line with msg=\"tool call\" found at level; captured:\n%s", out)
	return nil
}

// TestToolCallLogVisibleAtInfoLevel verifies L08: the per-tool-call access log
// line (msg "tool call") is emitted at `info` level, so it is present when the
// logger is configured at the default HTTP-mode level (info). Currently the
// line is emitted via e.Debug, which is dropped at Info level — this test is
// the red half of the TDD pair.
func TestToolCallLogVisibleAtInfoLevel(t *testing.T) {
	log, buf := captureLoggerAtLevel(t, logrus.InfoLevel)

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

	// The tool-call access line must survive at Info level and carry its fields.
	m := findToolCallLine(t, buf)
	if got := fieldString(t, m, "tool"); got != "forgejo_repo_get" {
		t.Errorf("tool = %q, want forgejo_repo_get", got)
	}
	if got := fieldString(t, m, "outcome"); got != "ok" {
		t.Errorf("outcome = %q, want ok", got)
	}
}
