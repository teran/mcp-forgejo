package config

import (
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("FORGEJO_URL", "https://git.example.dev")
	t.Setenv("FORGEJO_TOKEN", "secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ForgejoURL != "https://git.example.dev" {
		t.Errorf("ForgejoURL = %q", cfg.ForgejoURL)
	}
	if cfg.ForgejoToken != "secret" {
		t.Errorf("ForgejoToken = %q", cfg.ForgejoToken)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host default = %q, want 0.0.0.0", cfg.Host)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port default = %q, want 8080", cfg.Port)
	}
	if cfg.LogFilename != "/tmp/mcp-forgejo.log" {
		t.Errorf("LogFilename default = %q", cfg.LogFilename)
	}
	if cfg.LogFormat != "text" {
		t.Errorf("LogFormat default = %q", cfg.LogFormat)
	}
	if cfg.Transport != "stdio" {
		t.Errorf("Transport default = %q", cfg.Transport)
	}
	if cfg.LogLevel != "" {
		t.Errorf("LogLevel default = %q, want empty", cfg.LogLevel)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("FORGEJO_URL", "https://git.example.dev")
	t.Setenv("FORGEJO_TOKEN", "secret")
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("PORT", "9090")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FILENAME", "/var/log/forgejo.log")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("TRANSPORT", "http-sse")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != "9090" {
		t.Errorf("host/port = %s/%s", cfg.Host, cfg.Port)
	}
	if cfg.LogLevel != "debug" || cfg.LogFilename != "/var/log/forgejo.log" || cfg.LogFormat != "json" {
		t.Errorf("log config = %s/%s/%s", cfg.LogLevel, cfg.LogFilename, cfg.LogFormat)
	}
	if cfg.Transport != "http-sse" {
		t.Errorf("Transport = %q, want http-sse", cfg.Transport)
	}
}

func TestLoadMissingURL(t *testing.T) {
	t.Setenv("FORGEJO_URL", "")
	t.Setenv("FORGEJO_TOKEN", "secret")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when FORGEJO_URL is empty")
	} else if !strings.Contains(err.Error(), "FORGEJO_URL") {
		t.Errorf("error = %v, want FORGEJO_URL mention", err)
	}
}

func TestLoadMissingToken(t *testing.T) {
	t.Setenv("FORGEJO_URL", "https://git.example.dev")
	t.Setenv("FORGEJO_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when FORGEJO_TOKEN is empty")
	} else if !strings.Contains(err.Error(), "FORGEJO_TOKEN") {
		t.Errorf("error = %v, want FORGEJO_TOKEN mention", err)
	}
}
