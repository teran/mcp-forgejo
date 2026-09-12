package server

import (
	"context"
	"log/slog"

	"github.com/sirupsen/logrus"
)

// logrusSlogHandler is a slog.Handler that forwards structured slog records
// into a logrus logger. It bridges the go-sdk's internal slog logging onto the
// server's logrus logger (L7), so a single configured logger captures both the
// SDK's activity and the server's own tool/upstream logging.
type logrusSlogHandler struct {
	log *logrus.Logger
}

// Enabled reports whether a record at level is enabled for the given context.
func (h logrusSlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return h.log.IsLevelEnabled(slogToLogrus(level))
}

// Handle forwards a slog record to logrus, mapping the level and carrying over
// the record's attributes as fields.
func (h logrusSlogHandler) Handle(_ context.Context, r slog.Record) error {
	entry := h.log.WithFields(logrus.Fields{"logger": "mcp"})
	r.Attrs(func(a slog.Attr) bool {
		entry = entry.WithField(a.Key, a.Value.Any())
		return true
	})
	switch {
	case r.Level >= slog.LevelError:
		entry.Error(r.Message)
	case r.Level >= slog.LevelWarn:
		entry.Warn(r.Message)
	case r.Level >= slog.LevelInfo:
		entry.Info(r.Message)
	default:
		entry.Debug(r.Message)
	}
	return nil
}

// WithAttrs returns a handler that is a no-op for the purpose of this bridge.
func (h logrusSlogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }

// WithGroup returns a handler that is a no-op for the purpose of this bridge.
func (h logrusSlogHandler) WithGroup(_ string) slog.Handler { return h }

// slogToLogrus maps a slog level onto the equivalent logrus level.
func slogToLogrus(l slog.Level) logrus.Level {
	switch {
	case l >= slog.LevelError:
		return logrus.ErrorLevel
	case l >= slog.LevelWarn:
		return logrus.WarnLevel
	case l >= slog.LevelInfo:
		return logrus.InfoLevel
	default:
		return logrus.DebugLevel
	}
}
