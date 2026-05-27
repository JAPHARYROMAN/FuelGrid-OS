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

	// Optional deps. Leaving DatabaseURL / RedisURL unset is supported
	// for ultra-thin smoke tests; the readiness probe simply skips probes
	// for un-configured dependencies.
	DatabaseURL          string        `envconfig:"DATABASE_URL"`
	DatabaseMaxOpenConns int32         `envconfig:"DATABASE_MAX_OPEN_CONNS" default:"25"`
	DatabaseMinIdleConns int32         `envconfig:"DATABASE_MIN_IDLE_CONNS" default:"5"`
	DatabaseConnLifetime time.Duration `envconfig:"DATABASE_CONN_MAX_LIFETIME" default:"30m"`
	DatabaseConnIdleTime time.Duration `envconfig:"DATABASE_CONN_MAX_IDLE_TIME" default:"5m"`

	RedisURL string `envconfig:"REDIS_URL"`

	// Auth. AUTH_PASSWORD_PEPPER is a base64-or-text secret mixed into
	// every password hash. Empty in dev is fine; production deployments
	// must set it from a secret store.
	AuthPasswordPepper   string        `envconfig:"AUTH_PASSWORD_PEPPER"`
	AuthSessionTTL       time.Duration `envconfig:"AUTH_SESSION_TTL" default:"12h"`
	AuthRefreshTTL       time.Duration `envconfig:"AUTH_REFRESH_TTL" default:"720h"`
	AuthLoginRateMax     int64         `envconfig:"AUTH_LOGIN_RATE_LIMIT" default:"5"`
	AuthLoginRateWindow  time.Duration `envconfig:"AUTH_LOGIN_RATE_WINDOW" default:"15m"`
	AuthLoginLockAfter   int           `envconfig:"AUTH_LOGIN_LOCK_AFTER" default:"10"`
	AuthLoginLockFor     time.Duration `envconfig:"AUTH_LOGIN_LOCK_FOR" default:"30m"`
	AuthPasswordResetTTL time.Duration `envconfig:"AUTH_PASSWORD_RESET_TTL" default:"1h"`
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
