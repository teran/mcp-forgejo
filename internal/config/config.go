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
	ForgejoToken string `envconfig:"FORGEJO_TOKEN"`
	Host         string `envconfig:"HOST" default:"0.0.0.0"`
	Port         string `envconfig:"PORT" default:"8080"`
	LogLevel     string `envconfig:"LOG_LEVEL"`
	LogFilename  string `envconfig:"LOG_FILENAME" default:"/tmp/mcp-forgejo.log"`
	LogFormat    string `envconfig:"LOG_FORMAT" default:"text"`
	Transport    string `envconfig:"TRANSPORT" default:"stdio"`
}

// Load reads configuration from the environment and validates it. ForgejoURL
// and ForgejoToken are required; the remaining fields have sensible defaults.
func Load() (Config, error) {
	var c Config
	if err := envconfig.Process("", &c); err != nil {
		return Config{}, err
	}
	if c.ForgejoURL == "" {
		return Config{}, errors.New("FORGEJO_URL is required")
	}
	if c.ForgejoToken == "" {
		return Config{}, errors.New("FORGEJO_TOKEN is required")
	}
	return c, nil
}
