package main

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"git.homelab.teran.dev/teran/mcp-forgejo/internal/config"
	"git.homelab.teran.dev/teran/mcp-forgejo/internal/infrastructure/forgejo"
)

// setBaseEnv sets the required config env vars.
func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("FORGEJO_URL", "https://git.example.dev")
	t.Setenv("FORGEJO_TOKEN", "secret")
	t.Setenv("LOG_FILENAME", t.TempDir()+"/log.log")
}

func TestRunConfigError(t *testing.T) {
	t.Setenv("FORGEJO_URL", "")
	t.Setenv("FORGEJO_TOKEN", "")
	if code := run(nil); code == 0 {
		t.Fatal("expected non-zero exit code for missing config")
	}
}

func TestRunFlagParseError(t *testing.T) {
	setBaseEnv(t)
	if code := run([]string{"--bogus-flag"}); code == 0 {
		t.Fatal("expected non-zero exit code for bad flag")
	}
}

func TestRunLoggingError(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("LOG_LEVEL", "bogus")
	if code := run([]string{"--transport", "stdio"}); code == 0 {
		t.Fatal("expected non-zero exit code for invalid log level")
	}
}

func TestRunBuildError(t *testing.T) {
	setBaseEnv(t)
	orig := buildServer
	buildServer = func(forgejo.Config, *http.Client) (*mcp.Server, error) {
		return nil, errors.New("build failed")
	}
	defer func() { buildServer = orig }()

	if code := run([]string{"--transport", "stdio"}); code == 0 {
		t.Fatal("expected non-zero exit code for build error")
	}
}

func TestRunStdioSuccess(t *testing.T) {
	setBaseEnv(t)
	origRun := runStdio
	runStdio = func(context.Context, *mcp.Server) error { return nil }
	defer func() { runStdio = origRun }()

	if code := run([]string{"--transport", "stdio"}); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
}

func TestRunStdioError(t *testing.T) {
	setBaseEnv(t)
	origRun := runStdio
	runStdio = func(context.Context, *mcp.Server) error { return errors.New("stdio failed") }
	defer func() { runStdio = origRun }()

	if code := run([]string{"--transport", "stdio"}); code == 0 {
		t.Fatal("expected non-zero exit for stdio failure")
	}
}

func TestRunHTTPSuccess(t *testing.T) {
	setBaseEnv(t)
	origRun := runHTTPServer
	runHTTPServer = func(context.Context, config.Config, *mcp.Server) error { return nil }
	defer func() { runHTTPServer = origRun }()

	if code := run([]string{"--transport", "http-sse"}); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
}

func TestRunHTTPError(t *testing.T) {
	setBaseEnv(t)
	origRun := runHTTPServer
	runHTTPServer = func(context.Context, config.Config, *mcp.Server) error { return errors.New("http failed") }
	defer func() { runHTTPServer = origRun }()

	if code := run([]string{"--transport", "http-sse"}); code == 0 {
		t.Fatal("expected non-zero exit for http failure")
	}
}

func TestRunUnknownTransport(t *testing.T) {
	setBaseEnv(t)
	if code := run([]string{"--transport", "unknown"}); code == 0 {
		t.Fatal("expected non-zero exit for unknown transport")
	}
}

func TestRunStdioClosesLogFile(t *testing.T) {
	// With a valid LOG_LEVEL and the stdio transport, logging.Setup opens a log
	// file and returns a non-nil closer, exercising the deferred Close path.
	setBaseEnv(t)
	t.Setenv("LOG_LEVEL", "info")
	origRun := runStdio
	runStdio = func(context.Context, *mcp.Server) error { return nil }
	defer func() { runStdio = origRun }()

	if code := run([]string{"--transport", "stdio"}); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
}
