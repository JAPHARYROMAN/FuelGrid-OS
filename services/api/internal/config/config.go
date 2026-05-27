// Package config loads runtime parameters for the API service from
// environment variables, with defaults tuned for local development.
package config

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Config is the full set of runtime parameters for the API service.
// Defaults are tuned for local development; production values are
// supplied via environment variables.
type Config struct {
	Env             string        `envconfig:"NODE_ENV" default:"development"`
	Host            string        `envconfig:"API_HOST" default:"0.0.0.0"`
	Port            int           `envconfig:"API_PORT" default:"8080"`
	LogLevel        string        `envconfig:"API_LOG_LEVEL" default:"info"`
	LogFormat       string        `envconfig:"API_LOG_FORMAT" default:"json"`
	CORSOrigins     []string      `envconfig:"API_CORS_ALLOWED_ORIGINS" default:"http://localhost:3000"`
	ShutdownTimeout time.Duration `envconfig:"API_SHUTDOWN_TIMEOUT" default:"15s"`

	// Optional deps — used by /readyz once Stage 3 lands. Leaving these
	// unset is fine; readiness simply skips probes that aren't configured.
	DatabaseURL string `envconfig:"DATABASE_URL"`
	RedisURL    string `envconfig:"REDIS_URL"`
}

// Load reads environment variables and returns a populated Config.
func Load() (Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

// Addr returns the host:port string for the HTTP listener.
func (c Config) Addr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}
