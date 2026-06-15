// Package config loads runtime parameters for the API service from
// environment variables, with defaults tuned for local development.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// redactedPlaceholder is rendered in place of any non-empty secret whenever a
// Secret is stringified or logged. An empty secret renders as "" so that
// "is this configured?" log lines stay truthful.
const redactedPlaceholder = "***redacted***"

// Secret is a string-backed type for sensitive configuration values
// (passwords, peppers, bearer tokens, and connection URLs that embed
// credentials). It exists so a secret can never accidentally leak into logs
// or error output: String and the slog.LogValuer hook both redact the value.
//
// It is a defined string type so envconfig can populate it directly from the
// environment and so call sites that only need the raw bytes can convert with
// []byte(s). When the plaintext value is genuinely needed (e.g. to build a DB
// DSN or seed a password hasher) call Reveal — that name makes the disclosure
// explicit at the call site and easy to audit.
type Secret string

// String redacts the secret. It implements fmt.Stringer, so any %s/%v
// formatting (including the default formatting Go uses when a value lands in a
// log field or error string) prints the placeholder instead of the plaintext.
func (s Secret) String() string {
	if s == "" {
		return ""
	}
	return redactedPlaceholder
}

// LogValue implements slog.LogValuer so structured logging redacts the value
// even when the secret is passed directly as a slog attribute (slog reads
// LogValue rather than calling String in that path).
func (s Secret) LogValue() slog.Value {
	return slog.StringValue(s.String())
}

// Reveal returns the underlying plaintext. Use it only at the boundary where
// the raw secret is actually required; never pass the result to a logger.
func (s Secret) Reveal() string {
	return string(s)
}

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
	// DatabaseURL / DatabaseAppURL / RedisURL are Secrets: the DSNs embed the
	// connection password, so they must never reach a log line.
	DatabaseURL Secret `envconfig:"DATABASE_URL"`
	// DatabaseAppURL, when set, connects request-scoped queries as the
	// non-owner `fuelgrid_app` role so Postgres RLS enforces tenant isolation.
	// Leave empty to keep connecting as the owner (RLS bypassed — the default).
	DatabaseAppURL       Secret        `envconfig:"DATABASE_APP_URL"`
	DatabaseMaxOpenConns int32         `envconfig:"DATABASE_MAX_OPEN_CONNS" default:"25"`
	DatabaseMinIdleConns int32         `envconfig:"DATABASE_MIN_IDLE_CONNS" default:"5"`
	DatabaseConnLifetime time.Duration `envconfig:"DATABASE_CONN_MAX_LIFETIME" default:"30m"`
	DatabaseConnIdleTime time.Duration `envconfig:"DATABASE_CONN_MAX_IDLE_TIME" default:"5m"`

	RedisURL Secret `envconfig:"REDIS_URL"`

	// Auth. AUTH_PASSWORD_PEPPER is a base64-or-text secret mixed into
	// every password hash. Empty in dev is fine; production deployments
	// must set it from a secret store.
	AuthPasswordPepper   Secret        `envconfig:"AUTH_PASSWORD_PEPPER"`
	AuthSessionTTL       time.Duration `envconfig:"AUTH_SESSION_TTL" default:"12h"`
	AuthRefreshTTL       time.Duration `envconfig:"AUTH_REFRESH_TTL" default:"720h"`
	AuthLoginRateMax     int64         `envconfig:"AUTH_LOGIN_RATE_LIMIT" default:"5"`
	AuthLoginRateWindow  time.Duration `envconfig:"AUTH_LOGIN_RATE_WINDOW" default:"15m"`
	AuthLoginLockAfter   int           `envconfig:"AUTH_LOGIN_LOCK_AFTER" default:"10"`
	AuthLoginLockFor     time.Duration `envconfig:"AUTH_LOGIN_LOCK_FOR" default:"30m"`
	AuthPasswordResetTTL time.Duration `envconfig:"AUTH_PASSWORD_RESET_TTL" default:"1h"`

	// SR-L3: per-IP HTTP-layer rate limit on the public password-reset request and
	// confirm endpoints. These routes sit outside the per-tenant limiter (no actor
	// yet) and outside the identity service's login buckets, so without this guard
	// an attacker who already holds a reset token could replay /confirm without any
	// HTTP-layer throttle. We reuse the Redis-backed ratelimit.Limiter under a
	// dedicated IP prefix. The window is kept lenient so a legitimate user fat-
	// fingering a token a few times is never locked out. A non-positive max
	// disables the guard (the Go zero value), so a Config built without Load() —
	// the integration harness — never throttles unless a test opts in.
	AuthPasswordResetRateMax    int64         `envconfig:"AUTH_PASSWORD_RESET_RATE_LIMIT" default:"10"`
	AuthPasswordResetRateWindow time.Duration `envconfig:"AUTH_PASSWORD_RESET_RATE_WINDOW" default:"15m"`

	// AuthEnforceMfaForPrivilegedRoles gates the requireMFASatisfied middleware
	// (SR-M1): when true, an actor whose role mandates a second factor
	// (identity.RoleRequiresMfa) is refused the sensitive admin-console routes
	// with 403 mfa_required unless their session satisfied MFA. The MFA
	// enrollment routes, /me and /auth stay reachable so an unenrolled
	// privileged user can still set up a second factor (no lockout).
	//
	// It defaults to true via Load() (the production path). A Config built
	// directly as config.Config{} (the integration harness) gets the Go zero
	// value false, so the gate is OFF there — keeping the many multi-privileged-
	// user maker-checker tests, which seed second approvers without MFA, working
	// unchanged. The dedicated SR-M1 test opts the flag back on to prove
	// enforcement.
	AuthEnforceMfaForPrivilegedRoles bool `envconfig:"AUTH_ENFORCE_MFA_FOR_PRIVILEGED_ROLES" default:"true"`

	// Request throttling (REL-4). Two independent guards layered on top of the
	// login limiter, both fail-open when Redis is unavailable:
	//
	//   - TenantRateLimit / TenantRateWindow: a per-tenant sliding(-ish) fixed
	//     window quota on authenticated requests, keyed by tenant_id. A
	//     non-positive limit disables the per-tenant guard entirely. The default
	//     is deliberately generous so ordinary traffic (and the integration
	//     harness, which fires many requests fast) never trips it; production
	//     tunes it down per its own load profile.
	//   - MaxInflight: a process-wide cap on concurrently in-flight requests; the
	//     N+1th request is shed with 503 to protect the service under overload.
	//     0 (the default) means unlimited — the cap is opt-in.
	//
	// All three default such that a Config built without going through Load
	// (e.g. a test that constructs config.Config{} directly) leaves them at
	// their Go zero value, which the middleware treats as "disabled" — so test
	// traffic is never throttled. envconfig only applies the `default` tags when
	// Load() runs, which is the production path.
	TenantRateLimit  int64         `envconfig:"API_TENANT_RATE_LIMIT" default:"600"`
	TenantRateWindow time.Duration `envconfig:"API_TENANT_RATE_WINDOW" default:"1m"`
	MaxInflight      int64         `envconfig:"API_MAX_INFLIGHT" default:"256"`

	// Readiness-aware load shedding (REL-5). A lightweight background checker
	// Pings the database on this interval and caches the result in an atomic
	// flag; when the DB has been observed unreachable the shedding middleware
	// returns 503 + Retry-After fast (no per-request DB call) for ordinary API
	// traffic, while the operational probes and recovery paths stay reachable.
	// A non-positive interval (the Go zero value of a Config built without
	// Load(), e.g. the integration harness) falls back to the in-code default
	// when the checker is started; the checker is only started by Start(), so a
	// harness that never calls Start() leaves the flag at its default-healthy
	// value and nothing is ever shed.
	HealthcheckInterval time.Duration `envconfig:"API_HEALTHCHECK_INTERVAL" default:"3s"`

	// Platform admin. A static bearer used by the tenant-provisioning
	// endpoint (POST /api/v1/platform/tenants). Empty disables the route
	// entirely. Distinct from user sessions — it's an operator/IaC token,
	// not a logged-in principal.
	PlatformAdminToken Secret `envconfig:"PLATFORM_ADMIN_TOKEN"`

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

	// Background scheduler (internal/scheduler). Each knob is the interval
	// between runs of one recurring job; the runner is multi-instance-safe via a
	// per-job Postgres advisory lock so only one API replica runs a given job per
	// tick. A non-positive interval DISABLES that job — which is exactly what a
	// Config built without Load() (the integration harness, or a test that
	// constructs config.Config{} directly) gets, so the harness runs no jobs.
	// envconfig only applies these `default` tags on the Load() (production)
	// path. The defaults below are tuned for production cadence, not test
	// latency. SchedulerEnabled is a master switch: false leaves the whole runner
	// unstarted regardless of the per-job intervals.
	SchedulerEnabled                bool          `envconfig:"SCHEDULER_ENABLED" default:"true"`
	SchedulerRevenueComputeInterval time.Duration `envconfig:"SCHEDULER_REVENUE_COMPUTE_INTERVAL" default:"1h"`
	SchedulerAgingRefreshInterval   time.Duration `envconfig:"SCHEDULER_AGING_REFRESH_INTERVAL" default:"1h"`
	SchedulerRiskDetectInterval     time.Duration `envconfig:"SCHEDULER_RISK_DETECT_INTERVAL" default:"30m"`
	SchedulerProjectionInterval     time.Duration `envconfig:"SCHEDULER_PROJECTION_INTERVAL" default:"15m"`
	SchedulerOutboxSweepInterval    time.Duration `envconfig:"SCHEDULER_OUTBOX_SWEEP_INTERVAL" default:"5m"`
	SchedulerSessionCleanupInterval time.Duration `envconfig:"SCHEDULER_SESSION_CLEANUP_INTERVAL" default:"1h"`
	// SchedulerRetentionSweepInterval is the cadence for the data-lifecycle
	// retention sweep (Feature 13.2): it reads the per-tenant retention policies
	// and records the audit-purge candidate count. It is a dry-run in this slice
	// (it does not purge — the audit ledger is append-only), so a once-a-day tick
	// is plenty. <= 0 disables it.
	SchedulerRetentionSweepInterval time.Duration `envconfig:"SCHEDULER_RETENTION_SWEEP_INTERVAL" default:"24h"`
	// SchedulerScheduledReportsInterval is the tick cadence for the per-tenant
	// Scheduled Reports dispatcher (Reports Center Phase 12). Each tick claims
	// schedules whose next_run_at <= now and atomically advances next_run_at, so a
	// frequent tick delivers exactly once per period (the advance + per-period
	// ledger guard collapse duplicates). A sub-period cadence (default 1m) keeps a
	// schedule's actual delivery close to its configured time. <= 0 disables it.
	// This SUPERSEDES the removed global env-digest jobs.
	SchedulerScheduledReportsInterval time.Duration `envconfig:"SCHEDULER_SCHEDULED_REPORTS_INTERVAL" default:"1m"`
	// SchedulerSessionRetention is how long a session row is kept after it
	// expires or is revoked before the cleanup job prunes it; the durable
	// sessions table is an audit trail, so we keep terminated rows for a window
	// rather than deleting on expiry. SchedulerOutboxRequeueAfter is how long a
	// dead-lettered outbox row must have been parked before the sweep requeues it
	// for one more drain attempt; SchedulerJobRunRetention prunes old job_runs
	// ledger rows. SchedulerLockTimeout bounds any single job's work so a stuck
	// job can't hold its advisory lock forever.
	SchedulerSessionRetention   time.Duration `envconfig:"SCHEDULER_SESSION_RETENTION" default:"720h"`
	SchedulerOutboxRequeueAfter time.Duration `envconfig:"SCHEDULER_OUTBOX_REQUEUE_AFTER" default:"1h"`
	SchedulerJobRunRetention    time.Duration `envconfig:"SCHEDULER_JOB_RUN_RETENTION" default:"720h"`
	SchedulerLockTimeout        time.Duration `envconfig:"SCHEDULER_LOCK_TIMEOUT" default:"10m"`
	// Transactional email. When SMTP_HOST is empty the email package falls back
	// to a console (log-only) sender so local development never sends real mail.
	// SMTP_PASSWORD is a Secret so it never reaches a log line. SMTP_FROM is the
	// envelope/header From; it defaults inside the email package when blank.
	SMTPHost     string `envconfig:"SMTP_HOST"`
	SMTPPort     int    `envconfig:"SMTP_PORT" default:"587"`
	SMTPUsername string `envconfig:"SMTP_USERNAME"`
	SMTPPassword Secret `envconfig:"SMTP_PASSWORD"`
	SMTPFrom     string `envconfig:"SMTP_FROM"`
	// AppBaseURL is the public web origin used to build links in transactional
	// email (e.g. the password-reset URL). Falls back to localhost in dev.
	AppBaseURL string `envconfig:"APP_BASE_URL" default:"http://localhost:3000"`

	// ScheduledReportsWebhookAllowHosts is an OPTIONAL allowlist of exact hostnames
	// the per-tenant Scheduled Reports webhook channel may POST to (Phase 12). When
	// non-empty ONLY these hosts are permitted (in addition to the always-on SSRF
	// guard: https required, private/loopback/link-local addresses rejected). When
	// empty the SSRF guard alone applies — any public https host is allowed. Set
	// this to lock webhook delivery down to known integration endpoints.
	ScheduledReportsWebhookAllowHosts []string `envconfig:"SCHEDULED_REPORTS_WEBHOOK_ALLOW_HOSTS"`

	// M-Pesa (Safaricom Daraja) mobile-money collections. When
	// MPESA_CONSUMER_KEY / MPESA_CONSUMER_SECRET are unset the client is a
	// disabled no-op (like SMTP/Sentry): the endpoints mount but initiating a
	// push returns 503 rather than dialing Safaricom — so dev/CI never need
	// credentials. The key + secret are Secrets so they never reach a log line.
	// MPESA_ENV selects sandbox|production; MPESA_SHORTCODE / MPESA_PASSKEY /
	// MPESA_CALLBACK_URL are needed only to actually initiate an STK push.
	MpesaConsumerKey    Secret `envconfig:"MPESA_CONSUMER_KEY"`
	MpesaConsumerSecret Secret `envconfig:"MPESA_CONSUMER_SECRET"`
	MpesaShortcode      string `envconfig:"MPESA_SHORTCODE"`
	MpesaPasskey        Secret `envconfig:"MPESA_PASSKEY"`
	MpesaEnv            string `envconfig:"MPESA_ENV" default:"sandbox"`
	MpesaCallbackURL    string `envconfig:"MPESA_CALLBACK_URL"`

	// Observability.
	MetricsObserveInterval time.Duration `envconfig:"METRICS_OBSERVE_INTERVAL" default:"15s"`
	OtelExporter           string        `envconfig:"OTEL_EXPORTER" default:"none"`
	OtelServiceName        string        `envconfig:"OTEL_SERVICE_NAME" default:"fuelgrid-api"`
	// OtelExporterOTLPEndpoint is the OTLP/gRPC collector address used when
	// OTEL_EXPORTER=otlp (e.g. "tempo:4317" or "https://otlp.example.com:443").
	// A bare host:port or https:// uses TLS; an http:// prefix forces an
	// insecure/plaintext connection for a local collector. It MUST be set when
	// the exporter is "otlp": a configured-but-broken endpoint is a fatal boot
	// error so traces the operator asked for never disappear silently.
	OtelExporterOTLPEndpoint string  `envconfig:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	SentryDSN                string  `envconfig:"SENTRY_DSN"`
	SentryTracesSampleRate   float64 `envconfig:"SENTRY_TRACES_SAMPLE_RATE" default:"0.05"`
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
