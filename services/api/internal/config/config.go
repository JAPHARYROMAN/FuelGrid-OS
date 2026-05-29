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
	DatabaseURL string `envconfig:"DATABASE_URL"`
	// DatabaseAppURL, when set, connects request-scoped queries as the
	// non-owner `fuelgrid_app` role so Postgres RLS enforces tenant isolation.
	// Leave empty to keep connecting as the owner (RLS bypassed — the default).
	DatabaseAppURL       string        `envconfig:"DATABASE_APP_URL"`
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

	// Platform admin. A static bearer used by the tenant-provisioning
	// endpoint (POST /api/v1/platform/tenants). Empty disables the route
	// entirely. Distinct from user sessions — it's an operator/IaC token,
	// not a logged-in principal.
	PlatformAdminToken string `envconfig:"PLATFORM_ADMIN_TOKEN"`

	// Outbox publisher.
	OutboxPollInterval time.Duration `envconfig:"OUTBOX_POLL_INTERVAL" default:"2s"`
	OutboxBatchSize    int           `envconfig:"OUTBOX_BATCH_SIZE" default:"100"`

	// Observability.
	MetricsObserveInterval time.Duration `envconfig:"METRICS_OBSERVE_INTERVAL" default:"15s"`
	OtelExporter           string        `envconfig:"OTEL_EXPORTER" default:"none"`
	OtelServiceName        string        `envconfig:"OTEL_SERVICE_NAME" default:"fuelgrid-api"`
	SentryDSN              string        `envconfig:"SENTRY_DSN"`
	SentryTracesSampleRate float64       `envconfig:"SENTRY_TRACES_SAMPLE_RATE" default:"0.05"`
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
