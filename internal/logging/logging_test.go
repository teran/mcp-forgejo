package logging

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestSetupDisabledWhenLevelEmpty(t *testing.T) {
	logger, closer, err := Setup(ChannelStdio, "", "/tmp/nope.log", "text")
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if closer != nil {
		t.Errorf("closer should be nil when logging disabled, got %v", closer)
	}
	if logger == nil {
		t.Fatal("logger should not be nil")
	}
	if logger.Out == os.Stdout {
		t.Error("disabled logger should not write to stdout")
	}
}

func TestSetupInvalidLevel(t *testing.T) {
	if _, _, err := Setup(ChannelHTTP, "bogus", "", "text"); err == nil {
		t.Fatal("expected error for invalid log level")
	}
}

func TestSetupStdioWritesToFileWithPerms(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "mcp-forgejo.log")

	logger, closer, err := Setup(ChannelStdio, "info", filename, "text")
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if closer == nil {
		t.Fatal("closer should be non-nil for stdio file logging")
	}
	defer func() { _ = closer.Close() }()

	info, err := os.Stat(filename)
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("log file perms = %o, want 600", perm)
	}

	logger.Info("hello from test")

	if _, err := os.Stat(filename); err != nil {
		t.Fatalf("log file should exist: %v", err)
	}
	data, _ := os.ReadFile(filename)
	if len(data) == 0 {
		t.Error("log file should contain the logged line")
	}
}

func TestSetupHTTPWritesToStdout(t *testing.T) {
	logger, closer, err := Setup(ChannelHTTP, "info", "", "text")
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if closer != nil {
		t.Errorf("closer should be nil for http channel, got %v", closer)
	}
	if logger.Out != os.Stdout {
		t.Error("http channel logger should write to stdout")
	}
}

func TestSetupJSONFormat(t *testing.T) {
	logger, _, err := Setup(ChannelHTTP, "info", "", "json")
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if _, ok := logger.Formatter.(*logrus.JSONFormatter); !ok {
		t.Errorf("expected JSONFormatter, got %T", logger.Formatter)
	}
}

func TestSetupTextFormat(t *testing.T) {
	logger, _, err := Setup(ChannelHTTP, "info", "", "text")
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	tf, ok := logger.Formatter.(*logrus.TextFormatter)
	if !ok {
		t.Fatalf("expected TextFormatter, got %T", logger.Formatter)
	}
	if !tf.FullTimestamp {
		t.Error("TextFormatter should use FullTimestamp")
	}
}

func TestSetupOpenFileError(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "missing", "sub", "log.log")
	if _, _, err := Setup(ChannelStdio, "info", bad, "text"); err == nil {
		t.Fatal("expected error opening file in missing directory")
	}
}
