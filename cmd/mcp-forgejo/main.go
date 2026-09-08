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

	"git.homelab.teran.dev/teran/mcp-forgejo/internal/config"
	"git.homelab.teran.dev/teran/mcp-forgejo/internal/infrastructure/forgejo"
	"git.homelab.teran.dev/teran/mcp-forgejo/internal/logging"
	"git.homelab.teran.dev/teran/mcp-forgejo/internal/server"
)

// These variables are the seams that let tests replace the real server build
// and transport runners without spawning blocking servers.
var (
	buildServer   = server.Build
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s, err := buildServer(forgejo.Config{BaseURL: cfg.ForgejoURL, Token: cfg.ForgejoToken}, http.DefaultClient)
	if err != nil {
		logger.WithError(err).Error("failed to build server")
		return 1
	}

	switch *transport {
	case "stdio":
		logger.Info("starting mcp-forgejo in stdio mode")
		if err := runStdio(ctx, s); err != nil {
			logger.WithError(err).Error("stdio server failed")
			return 1
		}
	case "http-sse":
		logger.WithFields(logrus.Fields{"host": cfg.Host, "port": cfg.Port}).
			Info("starting mcp-forgejo in http-sse mode")
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
