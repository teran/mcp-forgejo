// Command mcp-forgejo is the composition root of the mcp-forgejo MCP server.
// It loads configuration, sets up logging for the chosen transport, builds the
// Forgejo client and tool registry, and runs the selected transport driver.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"

	"github.com/teran/mcp-forgejo/config"
	"github.com/teran/mcp-forgejo/infrastructure/forgejo"
	"github.com/teran/mcp-forgejo/logging"
	"github.com/teran/mcp-forgejo/observability"
	"github.com/teran/mcp-forgejo/server"
)

// Build metadata, stamped at link time via ldflags (B2). Under `go test` and
// plain `go build` these defaults hold; goreleaser overrides them (see
// .goreleaser.yaml and SPEC.md §8).
var (
	appName       = "mcp-forgejo"
	appVersion    = "dev"
	appCommitHash = "none"
	appTimestamp  = "unknown"
)

// startupBanner returns the B5 startup banner describing the build (B2).
func startupBanner() string {
	return fmt.Sprintf("Starting %s/%s (commit: %s; built at %s)", appName, appVersion, appCommitHash, appTimestamp)
}

// effectiveLogLevel returns the level at which to configure logging for the
// selected transport (L02/M06). In HTTP/SSE mode logging is ALWAYS enabled,
// defaulting to "info" and overridable via LOG_LEVEL. In stdio mode logging is
// enabled only when LOG_LEVEL is set; an empty configured level disables it
// (logging.Setup writes to io.Discard and no log file is opened).
func effectiveLogLevel(transport, configured string) string {
	if transport == "http-sse" {
		if configured == "" {
			return "info"
		}
		return configured
	}
	return configured
}

// These variables are the seams that let tests replace the real server build
// and transport runners without spawning blocking servers.
var (
	// buildServerWithObserver is the seam that lets tests replace the real
	// server build. It wires an UpstreamObserver (O03) into the server so the
	// Forgejo client emits upstream metrics. upstreamObserver is created once at
	// package init (it registers on the default registry, which /metrics reads)
	// and is reused across run() calls to avoid re-registering the collectors.
	buildServerWithObserver = server.BuildWithObserver
	upstreamObserver        = observability.NewUpstreamCollector()
	runStdio                = server.RunStdio
	runHTTPServer           = func(ctx context.Context, cfg config.Config, s *mcp.Server) error {
		httpSrv := &http.Server{
			Addr:              cfg.Host + ":" + cfg.Port,
			Handler:           server.NewHTTPHandler(s),
			ReadHeaderTimeout: 10 * time.Second,
		}

		// serveErr carries the result of ListenAndServe back to the main
		// goroutine. A buffered channel of one is enough: the serve goroutine
		// writes at most once and never blocks on the send.
		serveErr := make(chan error, 1)
		go func() {
			err := httpSrv.ListenAndServe()
			if err == http.ErrServerClosed {
				err = nil
			}
			serveErr <- err
		}()

		// Block until either the server fails on its own or the caller cancels
		// the context (e.g. SIGINT/SIGTERM), then shut down gracefully.
		select {
		case err := <-serveErr:
			return err
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = httpSrv.Shutdown(shutdownCtx)
			return nil
		}
	}
	runObservability = func(ctx context.Context, cfg config.Config, log *logrus.Logger) error {
		return observability.Run(ctx, cfg.InternalAddr, log)
	}
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run executes the server and returns a process exit code. It is separated
// from main so it can be exercised by tests with injected server runners.
func run(args []string) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	fs := flag.NewFlagSet("mcp-forgejo", flag.ContinueOnError)
	transport := fs.String("transport", cfg.Transport, "transport: stdio | http-sse")
	if err = fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parse flags: %v\n", err)
		return 1
	}

	channel := logging.ChannelHTTP
	if *transport == "stdio" {
		channel = logging.ChannelStdio
	}

	// L02/M06: derive the effective log level from the launch mode. HTTP/SSE
	// mode always logs (default "info"); stdio mode only when LOG_LEVEL is set.
	level := effectiveLogLevel(*transport, cfg.LogLevel)

	logger, closer, err := logging.Setup(channel, level, cfg.LogFilename, cfg.LogFormat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logging: %v\n", err)
		return 1
	}
	if closer != nil {
		defer func() { _ = closer.Close() }()
	}

	// B5/L6: when logging is enabled (always in HTTP/SSE mode; in stdio only
	// when LOG_LEVEL is set — L02), emit the startup banner as the very first
	// line, before the transport startup line. When the effective level is
	// empty the logger writes to io.Discard, so nothing is emitted.
	if level != "" {
		logger.Info(startupBanner())
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s, err := buildServerWithObserver(forgejo.Config{BaseURL: cfg.ForgejoURL, Token: cfg.ForgejoToken}, logger, upstreamObserver)
	if err != nil {
		logger.WithError(err).Error("failed to build server")
		return 1
	}

	switch *transport {
	case "stdio":
		// The stdio transport has no HTTP Authorization header, so the Forgejo
		// PAT must come from the environment (M6). HTTP/SSE mode instead accepts
		// the token per-request from the Authorization: Bearer header, so it is
		// not required at startup.
		if cfg.ForgejoToken == "" {
			logger.Error("FORGEJO_TOKEN is required for the stdio transport")
			return 1
		}
		logger.Info("starting mcp-forgejo in stdio mode")
		if err := runStdio(ctx, s); err != nil {
			logger.WithError(err).Error("stdio server failed")
			return 1
		}
	case "http-sse":
		logger.WithFields(logrus.Fields{"host": cfg.Host, "port": cfg.Port}).
			Info("starting mcp-forgejo in http-sse mode")
		// The observability endpoint (metrics/pprof/healthz/readyz) runs on a
		// separate internal listener and is only enabled in HTTP mode; stdio
		// owns stdout and does not start it (O1/O2/O4, N32).
		logger.WithField("addr", cfg.InternalAddr).Info("observability enabled")
		if err := runObservability(ctx, cfg, logger); err != nil {
			logger.WithError(err).Error("observability server failed")
			return 1
		}
		if err := runHTTPServer(ctx, cfg, s); err != nil {
			logger.WithError(err).Error("http server failed")
			return 1
		}
	default:
		logger.Errorf("unknown transport %q (must be stdio or http-sse)", *transport)
		return 1
	}
	return 0
}
