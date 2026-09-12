package domain

import "context"

// request-scoped context keys (L9/G11). A request ID and a source (client IP or
// transport label) are carried on the context so that every downstream layer —
// the tool logging in the server and the upstream Forgejo logging — can
// correlate a single client request.
type sessionKey int

const (
	sessionKeyRequestID sessionKey = iota
	sessionKeySource
)

// WithRequestID returns a derived context carrying id. It never mutates its
// input.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionKeyRequestID, id)
}

// RequestIDFromContext returns the request ID stored on the context and whether
// one is present. An empty-but-present ID reports ok=true so callers can fall
// back to generating a fresh ID when appropriate.
func RequestIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(sessionKeyRequestID).(string)
	return id, ok
}

// WithSource returns a derived context carrying source. It never mutates its
// input.
func WithSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, sessionKeySource, source)
}

// SourceFromContext returns the source stored on the context and whether one is
// present.
func SourceFromContext(ctx context.Context) (string, bool) {
	s, ok := ctx.Value(sessionKeySource).(string)
	return s, ok
}
