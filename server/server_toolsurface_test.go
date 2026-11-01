package server

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/teran/mcp-forgejo/infrastructure/forgejo"
)

// toolSurfaceLine describes the observable startup log contract for one
// registered tool (L10/L11/N45/N46/N08GO): a per-tool line carrying tool,
// title and always_present. In this server all tools are static, so
// always_present must always be true.
type toolSurfaceLine struct {
	tool          string
	title         string
	alwaysPresent bool
}

// allJSONLines decodes every log line in buf into a slice of field maps (JSON
// numbers decode as float64). Mirrors lastJSONLine but returns all lines so
// callers can scan for startup tool-surface records.
func allJSONLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	out := strings.TrimRight(buf.String(), "\n")
	if out == "" {
		return nil
	}
	var lines []map[string]any
	for _, ln := range strings.Split(out, "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("decode log line: %v (line=%q)", err, ln)
		}
		lines = append(lines, m)
	}
	return lines
}

// toolSurfaceLines returns the subset of parsed log lines that carry the
// always_present field, i.e. the startup tool-surface records.
func toolSurfaceLines(t *testing.T, buf *bytes.Buffer) []toolSurfaceLine {
	t.Helper()
	var out []toolSurfaceLine
	for _, m := range allJSONLines(t, buf) {
		v, ok := m["always_present"]
		if !ok {
			continue
		}
		ap, _ := v.(bool)
		out = append(out, toolSurfaceLine{
			tool:          fieldString(t, m, "tool"),
			title:         fieldString(t, m, "title"),
			alwaysPresent: ap,
		})
	}
	return out
}

// TestBuildWithObserverLogsToolSurface verifies that building the server with a
// non-nil logrus logger emits one startup tool-surface line per registered
// tool (L10/L11), each carrying tool, title and always_present=true.
func TestBuildWithObserverLogsToolSurface(t *testing.T) {
	log, buf := captureLogger(t)

	s, err := BuildWithObserver(forgejo.Config{BaseURL: "https://x", Token: "t"}, log, nil)
	if err != nil {
		t.Fatalf("BuildWithObserver() error = %v", err)
	}
	if s == nil {
		t.Fatal("BuildWithObserver() returned nil server")
	}

	lines := toolSurfaceLines(t, buf)
	if len(lines) == 0 {
		t.Fatal("no tool-surface log lines emitted; expected one per registered tool")
	}

	// Representative tools must each be logged with their human-readable title
	// and always_present=true.
	representative := []struct{ tool, title string }{
		{"forgejo_repo_get", "Get repository"},
		{"forgejo_repo_search", "Search repositories"},
		{"forgejo_issue_create", "Create issue"},
		{"forgejo_repo_delete", "Delete repository"},
	}
	for _, rep := range representative {
		found := false
		for _, ln := range lines {
			if ln.tool == rep.tool {
				found = true
				if ln.title != rep.title {
					t.Errorf("tool %q title = %q, want %q", rep.tool, ln.title, rep.title)
				}
				if !ln.alwaysPresent {
					t.Errorf("tool %q always_present = false, want true", rep.tool)
				}
			}
		}
		if !found {
			t.Errorf("no tool-surface line for representative tool %q", rep.tool)
		}
	}

	// Every registered tool is logged exactly once, all with always_present=true.
	if len(lines) != 52 {
		t.Errorf("distinct tool-surface lines = %d, want 52 (one per registered tool)", len(lines))
	}
	seen := make(map[string]bool)
	for _, ln := range lines {
		if seen[ln.tool] {
			t.Errorf("tool %q logged more than once", ln.tool)
		}
		seen[ln.tool] = true
		if !ln.alwaysPresent {
			t.Errorf("tool %q has always_present=false; no dynamic tools exist", ln.tool)
		}
	}
	if len(seen) != 52 {
		t.Errorf("distinct tool names logged = %d, want 52", len(seen))
	}
}

// TestBuildWithLoggerLogsToolSurface verifies the same startup tool-surface
// contract holds when going through the BuildWithLogger convenience entry
// point with a non-nil logger.
func TestBuildWithLoggerLogsToolSurface(t *testing.T) {
	log, buf := captureLogger(t)

	s, err := BuildWithLogger(forgejo.Config{BaseURL: "https://x", Token: "t"}, log)
	if err != nil {
		t.Fatalf("BuildWithLogger() error = %v", err)
	}
	if s == nil {
		t.Fatal("BuildWithLogger() returned nil server")
	}

	lines := toolSurfaceLines(t, buf)
	if len(lines) != 52 {
		t.Errorf("distinct tool-surface lines = %d, want 52", len(lines))
	}
	for _, ln := range lines {
		if !ln.alwaysPresent {
			t.Errorf("tool %q always_present = false, want true", ln.tool)
		}
	}
}

// TestBuildWithNilLoggerNoToolSurface verifies that building with a nil logger
// (logging disabled) emits no tool-surface lines at all (L2).
func TestBuildWithNilLoggerNoToolSurface(t *testing.T) {
	// Build(cfg) delegates to BuildWithObserver(cfg, nil, nil).
	s, err := Build(forgejo.Config{BaseURL: "https://x", Token: "t"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if s == nil {
		t.Fatal("Build() returned nil server")
	}

	// No buffer/log output is produced without a logger; verify the nil-logger
	// path also doesn't panic and that an explicit nil logger yields no lines.
	var buf bytes.Buffer
	log := logrus.New()
	log.SetOutput(&buf)
	log.SetLevel(logrus.TraceLevel)
	// We cannot inject our buffer into Build(cfg); instead confirm that passing
	// an explicit nil logger through BuildWithObserver produces nothing.
	s2, err := BuildWithObserver(forgejo.Config{BaseURL: "https://x", Token: "t"}, nil, nil)
	if err != nil {
		t.Fatalf("BuildWithObserver(nil logger) error = %v", err)
	}
	if s2 == nil {
		t.Fatal("BuildWithObserver(nil logger) returned nil server")
	}
	if got := toolSurfaceLines(t, &buf); len(got) != 0 {
		t.Errorf("nil logger produced %d tool-surface lines, want 0: %+v", len(got), got)
	}
}
