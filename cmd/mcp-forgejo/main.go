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

	"github.com/teran/mcp-forgejo/internal/config"
	"github.com/teran/mcp-forgejo/internal/infrastructure/forgejo"
	"github.com/teran/mcp-forgejo/internal/logging"
	"github.com/teran/mcp-forgejo/internal/observability"
	"github.com/teran/mcp-forgejo/internal/server"
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

// These variables are the seams that let tests replace the real server build
// and transport runners without spawning blocking servers.
var (
	buildServer   = server.BuildWithLogger
	runStdio      = server.RunStdio
	runHTTPServer = func(_ context.Context, cfg config.Config, s *mcp.Server) error {
		httpSrv := &http.Server{
			Addr:              cfg.Host + ":" + cfg.Port,
			Handler:           server.NewHTTPHandler(s),
			ReadHeaderTimeout: 10 * time.Second,
		}
		err := httpSrv.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
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

	logger, closer, err := logging.Setup(channel, cfg.LogLevel, cfg.LogFilename, cfg.LogFormat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logging: %v\n", err)
		return 1
	}
	if closer != nil {
		defer func() { _ = closer.Close() }()
	}

	// B5/L6: when logging is enabled (LOG_LEVEL set), emit the startup banner as
	// the very first line, before the transport startup line. If LOG_LEVEL is
	// empty the logger writes to io.Discard, so nothing is emitted.
	if cfg.LogLevel != "" {
		logger.Info(startupBanner())
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s, err := buildServer(forgejo.Config{BaseURL: cfg.ForgejoURL, Token: cfg.ForgejoToken}, logger)
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
