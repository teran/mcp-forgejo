package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"

	"github.com/teran/mcp-forgejo/config"
	"github.com/teran/mcp-forgejo/domain"
	"github.com/teran/mcp-forgejo/infrastructure/forgejo"
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
	orig := buildServerWithObserver
	buildServerWithObserver = func(forgejo.Config, *logrus.Logger, domain.UpstreamObserver) (*mcp.Server, error) {
		return nil, errors.New("build failed")
	}
	defer func() { buildServerWithObserver = orig }()

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

// TestRunHTTPInvokesObservability verifies that in http-sse mode the
// composition root starts the observability server via the runObservability
// seam and passes it cfg.InternalAddr.
func TestRunHTTPInvokesObservability(t *testing.T) {
	setBaseEnv(t)
	origHTTP := runHTTPServer
	origObs := runObservability
	runHTTPServer = func(context.Context, config.Config, *mcp.Server) error { return nil }
	called := false
	var gotAddr string
	runObservability = func(_ context.Context, cfg config.Config, _ *logrus.Logger) error {
		called = true
		gotAddr = cfg.InternalAddr
		return nil
	}
	defer func() {
		runHTTPServer = origHTTP
		runObservability = origObs
	}()

	if code := run([]string{"--transport", "http-sse"}); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !called {
		t.Fatal("expected runObservability to be invoked in http-sse mode")
	}
	if gotAddr != ":8081" {
		t.Fatalf("runObservability received InternalAddr = %q, want :8081", gotAddr)
	}
}

// TestRunStdioDoesNotInvokeObservability verifies that in stdio mode the
// observability server is NOT started (the stdio transport owns stdout).
func TestRunStdioDoesNotInvokeObservability(t *testing.T) {
	setBaseEnv(t)
	origStdio := runStdio
	origObs := runObservability
	runStdio = func(context.Context, *mcp.Server) error { return nil }
	called := false
	runObservability = func(context.Context, config.Config, *logrus.Logger) error {
		called = true
		return nil
	}
	defer func() {
		runStdio = origStdio
		runObservability = origObs
	}()

	if code := run([]string{"--transport", "stdio"}); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if called {
		t.Fatal("expected runObservability NOT to be invoked in stdio mode")
	}
}

// TestRunStdioRequiresToken verifies that in stdio mode FORGEJO_TOKEN is
// required: running with --transport stdio and no FORGEJO_TOKEN must fail.
func TestRunStdioRequiresToken(t *testing.T) {
	t.Setenv("FORGEJO_URL", "https://git.example.dev")
	t.Setenv("FORGEJO_TOKEN", "")
	t.Setenv("LOG_FILENAME", t.TempDir()+"/log.log")

	if code := run([]string{"--transport", "stdio"}); code == 0 {
		t.Fatal("expected non-zero exit for stdio without FORGEJO_TOKEN")
	}
}

// TestRunHTTPSuccessWithoutToken verifies that in http-sse mode FORGEJO_TOKEN
// is NOT required at startup: the token arrives per-request from the Bearer
// header, so the server must start successfully without it.
func TestRunHTTPSuccessWithoutToken(t *testing.T) {
	t.Setenv("FORGEJO_URL", "https://git.example.dev")
	t.Setenv("FORGEJO_TOKEN", "")
	t.Setenv("LOG_FILENAME", t.TempDir()+"/log.log")

	origHTTP := runHTTPServer
	origObs := runObservability
	runHTTPServer = func(context.Context, config.Config, *mcp.Server) error { return nil }
	runObservability = func(context.Context, config.Config, *logrus.Logger) error { return nil }
	defer func() {
		runHTTPServer = origHTTP
		runObservability = origObs
	}()

	if code := run([]string{"--transport", "http-sse"}); code != 0 {
		t.Fatalf("expected exit 0 for http-sse without FORGEJO_TOKEN, got %d", code)
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

// TestRunPassesLoggerToBuildWhenEnabled verifies that when logging is enabled
// (LOG_LEVEL set) the composition root forwards the non-nil logrus logger from
// logging.Setup into buildServer, so the server can wire L7/L8/L9 logging
// (per-call tool Debug logs, upstream resty request_id logs, slog->logrus
// bridge).
func TestRunPassesLoggerToBuildWhenEnabled(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("LOG_LEVEL", "info")

	origBuild := buildServerWithObserver
	origRun := runStdio
	var gotLogger *logrus.Logger
	buildServerWithObserver = func(_ forgejo.Config, log *logrus.Logger, _ domain.UpstreamObserver) (*mcp.Server, error) {
		gotLogger = log
		return &mcp.Server{}, nil
	}
	runStdio = func(context.Context, *mcp.Server) error { return nil }
	defer func() {
		buildServerWithObserver = origBuild
		runStdio = origRun
	}()

	if code := run([]string{"--transport", "stdio"}); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	if gotLogger == nil {
		t.Fatal("expected a non-nil *logrus.Logger to be passed to buildServer when LOG_LEVEL is set")
	}
	if gotLogger.Out == io.Discard {
		t.Fatal("expected an enabled logger (output != io.Discard) to be passed to buildServer")
	}
}

// TestRunPassesLoggerToBuildWhenDisabled verifies that when logging is disabled
// (LOG_LEVEL unset) the composition root still forwards the logger returned by
// logging.Setup. Setup always returns a non-nil logger; when the level is empty
// it writes to io.Discard. buildServer therefore receives a non-nil discard
// logger (never a nil pointer).
func TestRunPassesLoggerToBuildWhenDisabled(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("LOG_LEVEL", "")

	origBuild := buildServerWithObserver
	origRun := runStdio
	var gotLogger *logrus.Logger
	buildServerWithObserver = func(_ forgejo.Config, log *logrus.Logger, _ domain.UpstreamObserver) (*mcp.Server, error) {
		gotLogger = log
		return &mcp.Server{}, nil
	}
	runStdio = func(context.Context, *mcp.Server) error { return nil }
	defer func() {
		buildServerWithObserver = origBuild
		runStdio = origRun
	}()

	if code := run([]string{"--transport", "stdio"}); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	if gotLogger == nil {
		t.Fatal("expected a non-nil *logrus.Logger to be passed to buildServer even when logging is disabled")
	}
	if gotLogger.Out != io.Discard {
		t.Fatalf("expected a discard logger when LOG_LEVEL is unset, got output writer %T", gotLogger.Out)
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
