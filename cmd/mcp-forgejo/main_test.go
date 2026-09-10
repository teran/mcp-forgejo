package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
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

// captureStdout redirects os.Stdout to a pipe so a test can read what the
// logger wrote (the HTTP/SSE channel logs to stdout). It returns a function
// that flushes the pipe and returns the captured output, plus a restore
// function that resets os.Stdout. Do not use with t.Parallel.
func captureStdout(t *testing.T) (func() string, func()) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	restore := func() {
		os.Stdout = old
	}

	read := func() string {
		_ = w.Close()
		b, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatalf("read captured stdout: %v", err)
		}
		return string(b)
	}

	t.Cleanup(restore)
	return read, restore
}

// TestStartupBannerFormat verifies that startupBanner returns exactly the B5
// banner format: "Starting <appName>/<appVersion> (commit: <appCommitHash>;
// built at <appTimestamp>)".
func TestStartupBannerFormat(t *testing.T) {
	origName, origVer, origCommit, origTS := appName, appVersion, appCommitHash, appTimestamp
	appName = "test-app"
	appVersion = "v1.2.3"
	appCommitHash = "abc123"
	appTimestamp = "2026-09-10T00:00:00Z"
	defer func() {
		appName, appVersion, appCommitHash, appTimestamp = origName, origVer, origCommit, origTS
	}()

	want := "Starting test-app/v1.2.3 (commit: abc123; built at 2026-09-10T00:00:00Z)"
	if got := startupBanner(); got != want {
		t.Fatalf("startupBanner() = %q, want %q", got, want)
	}
}

// TestRunEmitsBannerFirstWhenLogLevelSet verifies that when logging is enabled
// (LOG_LEVEL set) the B5 startup banner is the very first line emitted, before
// the transport startup line.
func TestRunEmitsBannerFirstWhenLogLevelSet(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("LOG_FORMAT", "text")

	origRun := runHTTPServer
	runHTTPServer = func(context.Context, config.Config, *mcp.Server) error { return nil }
	defer func() { runHTTPServer = origRun }()

	out, restore := captureStdout(t)
	defer restore()

	if code := run([]string{"--transport", "http-sse"}); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	output := out()

	wantBanner := "Starting " + appName + "/" + appVersion +
		" (commit: " + appCommitHash + "; built at " + appTimestamp + ")"
	if !strings.Contains(output, wantBanner) {
		t.Fatalf("output %q does not contain startup banner %q", output, wantBanner)
	}

	const startupLine = "starting mcp-forgejo in http-sse mode"
	if !strings.Contains(output, startupLine) {
		t.Fatalf("output %q does not contain transport startup line %q", output, startupLine)
	}

	bannerIdx := strings.Index(output, wantBanner)
	startupIdx := strings.Index(output, startupLine)
	if bannerIdx > startupIdx {
		t.Fatalf("banner at index %d must be logged before transport startup line at index %d: %q", bannerIdx, startupIdx, output)
	}

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatal("expected at least one log line")
	}
	if !strings.Contains(lines[0], wantBanner) {
		t.Fatalf("first log line %q is not the startup banner %q", lines[0], wantBanner)
	}
}

// TestRunNoBannerWhenLogLevelUnset verifies that when LOG_LEVEL is unset the
// startup banner is NOT emitted (logging is disabled, so nothing is written).
func TestRunNoBannerWhenLogLevelUnset(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("LOG_LEVEL", "")

	origRun := runHTTPServer
	runHTTPServer = func(context.Context, config.Config, *mcp.Server) error { return nil }
	defer func() { runHTTPServer = origRun }()

	out, restore := captureStdout(t)
	defer restore()

	if code := run([]string{"--transport", "http-sse"}); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	output := out()

	if strings.Contains(output, "Starting ") {
		t.Fatalf("expected no startup banner when LOG_LEVEL unset, got %q", output)
	}
}

// TestBuildMetadataDefaults verifies the B2 build-metadata variables that are
// overridden at link time via ldflags have their compile-time defaults. Under
// `go test` no ldflags are applied, so the defaults must hold.
func TestBuildMetadataDefaults(t *testing.T) {
	if appName != "mcp-forgejo" {
		t.Errorf("appName = %q, want %q", appName, "mcp-forgejo")
	}
	if appVersion != "dev" {
		t.Errorf("appVersion = %q, want %q", appVersion, "dev")
	}
	if appCommitHash != "none" {
		t.Errorf("appCommitHash = %q, want %q", appCommitHash, "none")
	}
	if appTimestamp != "unknown" {
		t.Errorf("appTimestamp = %q, want %q", appTimestamp, "unknown")
	}
}
