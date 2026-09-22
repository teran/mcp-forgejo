// Package config loads server configuration from the environment using
// kelseyhightower/envconfig. It has no internal dependencies.
package config

import (
	"errors"

	"github.com/kelseyhightower/envconfig"
)

// Config holds all runtime configuration for mcp-forgejo.
type Config struct {
	ForgejoURL   string `envconfig:"FORGEJO_URL"`
	ForgejoToken string `envconfig:"FORGEJO_TOKEN" secret:"true"`
	ListenAddr   string `envconfig:"LISTEN_ADDR" default:":8080"`
	LogLevel     string `envconfig:"LOG_LEVEL"`
	LogFilename  string `envconfig:"LOG_FILENAME" default:"/tmp/mcp-forgejo.log"`
	LogFormat    string `envconfig:"LOG_FORMAT" default:"text"`
	Mode         string `envconfig:"MODE" default:"stdio"`
	InternalAddr string `envconfig:"INTERNAL_ADDR" default:":8081"`
}

// Load reads configuration from the environment and validates it. ForgejoURL is
// required; the remaining fields have sensible defaults. ForgejoToken is
// optional: it is required for the stdio transport (where it is the only source
// of the Forgejo PAT) and optional for the HTTP transport (where the token
// arrives per-request from the Authorization: Bearer header).
func Load() (Config, error) {
	var c Config
	if err := envconfig.Process("", &c); err != nil {
		return Config{}, err
	}
	if c.ForgejoURL == "" {
		return Config{}, errors.New("FORGEJO_URL is required")
	}
	return c, nil
}
