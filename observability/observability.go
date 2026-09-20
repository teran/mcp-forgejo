// Package observability exposes Prometheus metrics, pprof profiling and
// liveness/readiness probes on a separate internal HTTP listener, distinct
// from the MCP listen address. It is only started when the HTTP transport is
// used (see cmd/mcp-forgejo). It has no internal dependencies.
package observability

import (
	"context"
	"errors"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

// NewHandler returns the http.Handler serving the observability endpoints:
//
//   - GET /metrics     Prometheus metrics (default Go collectors: runtime/memstats + net/http)
//   - GET /debug/pprof/...  standard net/http/pprof profiling handlers
//   - GET /healthz     liveness probe (200)
//   - GET /readyz      readiness probe (200)
//
// The promhttp handler uses the default registry, which already includes the
// default Go collectors (go_goroutines, go_gc_duration_seconds,
// promhttp_metric_handler_requests_total, ...).
func NewHandler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/metrics", promhttp.Handler())

	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return mux
}

// Run starts the observability HTTP server on addr and returns nil
// immediately; the server keeps serving until ctx is cancelled, at which
// point it shuts down gracefully in the background. The caller composes it
// alongside the MCP HTTP server (see cmd/mcp-forgejo), so Run itself must not
// block. If logger is non-nil it logs the listen address (never any secret).
func Run(ctx context.Context, addr string, logger *logrus.Logger) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           NewHandler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	if logger != nil {
		logger.WithField("addr", addr).Info("starting observability endpoint")
	}

	go func() {
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.WithError(err).Error("observability server failed")
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	return nil
}
