package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"

	"example.com/teran/mcp-forgejo/internal/domain"
)

// newRequestID returns a 16-char lowercase hex string derived from 8 random
// bytes (L9/G11). If the OS RNG fails it falls back to the literal "unknown" so
// correlation logging never blocks or panics on an entropy error.
func newRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}

// sourceFromRequest extracts the client-IP chain for an HTTP request with the
// priority X-Real-IP -> X-Forwarded-For, falling back to RemoteAddr only when no
// header is present. Non-empty header parts are joined by ", ".
func sourceFromRequest(r *http.Request) string {
	parts := headerParts(r.Header)
	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}
	return strings.TrimSpace(r.RemoteAddr)
}

// clientIPFromHeader extracts the client-IP chain from headers alone (used by
// the receiving middleware, where no RemoteAddr is available).
func clientIPFromHeader(h http.Header) string {
	return strings.Join(headerParts(h), ", ")
}

// headerParts returns the trimmed, non-empty X-Real-IP and X-Forwarded-For
// header values in priority order.
func headerParts(h http.Header) []string {
	var parts []string
	if v := strings.TrimSpace(h.Get("X-Real-IP")); v != "" {
		parts = append(parts, v)
	}
	if v := strings.TrimSpace(h.Get("X-Forwarded-For")); v != "" {
		parts = append(parts, v)
	}
	return parts
}

// sensitiveArgsKeys lists argument keys (matched case-insensitively) whose
// values must never be written to a log. Values under these keys are replaced
// with "[REDACTED]" before logging (L8 / no secret leaks).
var sensitiveArgsKeys = map[string]struct{}{
	"token": {}, "password": {}, "passwd": {}, "secret": {}, "apikey": {},
	"access_key": {}, "private_key": {}, "authorization": {}, "cookie": {}, "pat": {},
}

// redactArgs returns a redacted representation of a tool-call arguments JSON
// object: sensitive keys are replaced with "[REDACTED]" while non-sensitive
// values are preserved. Non-object or invalid JSON is passed through as the raw
// string. It never leaks the underlying values (S2).
func redactArgs(raw json.RawMessage) any {
	if len(raw) == 0 || !json.Valid(raw) {
		return string(raw)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return string(raw)
	}
	for k := range m {
		if _, ok := sensitiveArgsKeys[strings.ToLower(k)]; ok {
			m[k] = "[REDACTED]"
		}
	}
	return m
}

// logToolCall emits a single Debug line describing a completed tool call with
// its redacted args, correlation IDs and byte/duration metrics. It is a no-op
// when the logger is nil.
func logToolCall(l *logrus.Logger, ctx context.Context, tool string, argsRaw json.RawMessage, start time.Time, outBytes int64, err error) {
	if l == nil {
		return
	}
	src, _ := domain.SourceFromContext(ctx)
	e := l.WithFields(logrus.Fields{
		"tool":      tool,
		"args":      redactArgs(argsRaw),
		"source":    src,
		"duration":  time.Since(start).String(),
		"in_bytes":  len(argsRaw),
		"out_bytes": outBytes,
		"outcome":   outcome(err),
	})
	if id, ok := domain.RequestIDFromContext(ctx); ok {
		e = e.WithField("request_id", id)
	}
	e.Debug("tool call")
}

// outcome maps an error to the "ok"/"error" outcome label used in logs.
func outcome(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}

// requestContextMiddleware injects a request_id and a source into the request
// context and (for tool calls, when a logger is configured) logs the call.
// The request_id is taken from the incoming X-Request-ID header, else the
// context, else freshly generated. The source is taken from the context, else
// the header chain, else "STDIO". It always invokes next and never alters the
// request handling logic (L9/G11).
func requestContextMiddleware(log *logrus.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			id := requestIDFromHeaders(req)
			if id == "" {
				if existing, ok := domain.RequestIDFromContext(ctx); ok {
					id = existing
				}
			}
			if id == "" {
				id = newRequestID()
			}
			ctx = domain.WithRequestID(ctx, id)

			if _, ok := domain.SourceFromContext(ctx); !ok {
				src := sourceFromRequestExtra(req)
				if src == "" {
					src = "STDIO"
				}
				ctx = domain.WithSource(ctx, src)
			}

			start := time.Now()
			result, err := next(ctx, method, req)

			if params, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok {
				var outBytes int64
				if result != nil {
					if b, merr := json.Marshal(result); merr == nil {
						outBytes = int64(len(b))
					}
				}
				logToolCall(log, ctx, params.Name, params.Arguments, start, outBytes, err)
			}
			return result, err
		}
	}
}

// requestIDFromHeaders returns the incoming X-Request-ID header value (if any).
func requestIDFromHeaders(req mcp.Request) string {
	if extra := req.GetExtra(); extra != nil && extra.Header != nil {
		return strings.TrimSpace(extra.Header.Get("X-Request-ID"))
	}
	return ""
}

// sourceFromRequestExtra computes the client-IP chain from the request's HTTP
// headers, or an empty string when no headers are present.
func sourceFromRequestExtra(req mcp.Request) string {
	if extra := req.GetExtra(); extra != nil && extra.Header != nil {
		return clientIPFromHeader(extra.Header)
	}
	return ""
}
