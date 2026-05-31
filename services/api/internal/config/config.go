// Package config loads runtime parameters for the API service from
// environment variables, with defaults tuned for local development.
package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Config is the full set of runtime parameters for the API service.
// Defaults are tuned for local development; production values are
// supplied via environment variables.
type Config struct {
	Env         string   `envconfig:"NODE_ENV" default:"development"`
	Host        string   `envconfig:"API_HOST" default:"0.0.0.0"`
	Port        int      `envconfig:"API_PORT" default:"8080"`
	LogLevel    string   `envconfig:"API_LOG_LEVEL" default:"info"`
	LogFormat   string   `envconfig:"API_LOG_FORMAT" default:"json"`
	CORSOrigins []string `envconfig:"API_CORS_ALLOWED_ORIGINS" default:"http://localhost:3000"`
	// TrustedProxyDepth is the number of trusted reverse proxies in front of
	// the API. 0 (default) means trust none — the client IP is r.RemoteAddr and
	// X-Forwarded-For is ignored (it's client-spoofable). Set it to the number
	// of proxy hops (e.g. 1 behind a single load balancer) so clientIP reads
	// the address inserted by the outermost trusted proxy (AUTH-09).
	TrustedProxyDepth int           `envconfig:"API_TRUSTED_PROXY_DEPTH" default:"0"`
	ShutdownTimeout   time.Duration `envconfig:"API_SHUTDOWN_TIMEOUT" default:"15s"`

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

	// Pagination. DefaultPageSize is the limit applied to list endpoints when
	// the caller omits ?limit; MaxPageSize is the hard ceiling a caller may
	// request (larger values clamp down). A non-positive value falls back to
	// the in-code default (50 / 200) so the helper stays safe even when
	// config is partially populated.
	DefaultPageSize int `envconfig:"API_DEFAULT_PAGE_SIZE" default:"50"`
	MaxPageSize     int `envconfig:"API_MAX_PAGE_SIZE" default:"200"`

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
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate rejects unsafe configurations that must never reach production.
// Outside development the CORS allow-list must be explicit https origins:
// a wildcard would let any site drive the API, and plain http exposes
// tokens in transit (AUTH-27). Development keeps the permissive localhost
// default so the dev stack just works.
func (c Config) validate() error {
	if c.Env == "development" {
		return nil
	}
	for _, o := range c.CORSOrigins {
		switch {
		case o == "*":
			return fmt.Errorf("config: API_CORS_ALLOWED_ORIGINS must not be '*' outside development")
		case !strings.HasPrefix(o, "https://"):
			return fmt.Errorf("config: API_CORS_ALLOWED_ORIGINS entry %q must be an explicit https:// origin outside development", o)
		}
	}
	// Outside development the API must run RLS-enforced: request-scoped queries
	// go through the non-owner fuelgrid_app pool (DATABASE_APP_URL) so Postgres
	// row-level security isolates each tenant. Running on the table-owner pool
	// (DATABASE_URL) bypasses RLS entirely (INFRA-01/AUTH-25), so refuse to
	// start that way. Only enforced when a database is configured at all (a
	// thin smoke deployment with no DATABASE_URL is exempt).
	if c.DatabaseURL != "" {
		switch c.DatabaseAppURL {
		case "":
			return fmt.Errorf("config: DATABASE_APP_URL is required outside development — point it at the non-owner fuelgrid_app role so Postgres RLS enforces tenant isolation; the API must not run request queries on the table-owner pool")
		case c.DatabaseURL:
			return fmt.Errorf("config: DATABASE_APP_URL must use the non-owner fuelgrid_app role, distinct from DATABASE_URL (the table owner)")
		}
	}
	return nil
}

// Addr returns the host:port string for the HTTP listener.
func (c Config) Addr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}
