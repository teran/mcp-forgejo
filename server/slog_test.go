package server

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestSlogToLogrus verifies the slog→logrus level mapping for every branch,
// including the boundary between the configured levels and the default.
func TestSlogToLogrus(t *testing.T) {
	tests := []struct {
		name  string
		level slog.Level
		want  string
	}{
		{"error boundary", slog.LevelError, "error"},
		{"above error", slog.Level(20), "error"},
		{"warn boundary", slog.LevelWarn, "warning"},
		{"between warn and error", slog.Level(6), "warning"},
		{"info boundary", slog.LevelInfo, "info"},
		{"between info and warn", slog.Level(2), "info"},
		{"debug level", slog.LevelDebug, "debug"},
		{"below debug", slog.Level(-8), "debug"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slogToLogrus(tt.level).String(); got != tt.want {
				t.Errorf("slogToLogrus(%v) = %q, want %q", tt.level, got, tt.want)
			}
		})
	}
}

// TestLogrusSlogHandlerHandle verifies each record level is forwarded to the
// matching logrus level and that the record message and attributes are carried
// over, with the "logger":"mcp" field always present.
func TestLogrusSlogHandlerHandle(t *testing.T) {
	tests := []struct {
		name      string
		level     slog.Level
		wantLevel string
	}{
		{"error record", slog.LevelError, "error"},
		{"warn record", slog.LevelWarn, "warning"},
		{"info record", slog.LevelInfo, "info"},
		{"debug record", slog.LevelDebug, "debug"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, buf := captureLogger(t)
			h := logrusSlogHandler{log: log}

			rec := slog.NewRecord(time.Now(), tt.level, "hello world", 0)
			rec.AddAttrs(slog.String("key", "value"))
			if err := h.Handle(context.Background(), rec); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			m := lastJSONLine(t, buf)
			if got := fieldString(t, m, "level"); got != tt.wantLevel {
				t.Errorf("level = %q, want %q", got, tt.wantLevel)
			}
			if got := fieldString(t, m, "msg"); got != "hello world" {
				t.Errorf("msg = %q, want %q", got, "hello world")
			}
			if got := fieldString(t, m, "logger"); got != "mcp" {
				t.Errorf("logger = %q, want mcp", got)
			}
			if got := fieldString(t, m, "key"); got != "value" {
				t.Errorf("attr key = %q, want value", got)
			}
		})
	}
}

// TestLogrusSlogHandlerEnabled verifies Enabled delegates to the logrus logger's
// level gate through the same mapping used by Handle.
func TestLogrusSlogHandlerEnabled(t *testing.T) {
	log, _ := captureLogger(t)
	lvl, err := logrus.ParseLevel("info")
	if err != nil {
		t.Fatalf("parse level: %v", err)
	}
	log.SetLevel(lvl)
	h := logrusSlogHandler{log: log}

	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled(info) = false, want true at info level")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("Enabled(error) = false, want true at info level")
	}
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Enabled(debug) = true, want false at info level")
	}
}

// TestLogrusSlogHandlerNoOp returns the same handler for WithAttrs/WithGroup so
// attribute grouping cannot accidentally drop the "mcp" bridge.
func TestLogrusSlogHandlerNoOp(t *testing.T) {
	log, _ := captureLogger(t)
	h := logrusSlogHandler{log: log}

	if got := h.WithAttrs([]slog.Attr{slog.String("a", "b")}); got != h {
		t.Errorf("WithAttrs did not return the same handler")
	}
	if got := h.WithGroup("group"); got != h {
		t.Errorf("WithGroup did not return the same handler")
	}
}
