// Package server owns the HTTP composition root: router, middleware
// stack, route table, and graceful lifecycle.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/japharyroman/fuelgrid-os/internal/accounting"
	"github.com/japharyroman/fuelgrid-os/internal/attachments"
	"github.com/japharyroman/fuelgrid-os/internal/banking"
	"github.com/japharyroman/fuelgrid-os/internal/branding"
	"github.com/japharyroman/fuelgrid-os/internal/cache"
	"github.com/japharyroman/fuelgrid-os/internal/calibration"
	"github.com/japharyroman/fuelgrid-os/internal/companies"
	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/email"
	"github.com/japharyroman/fuelgrid-os/internal/enterprise"
	"github.com/japharyroman/fuelgrid-os/internal/expenses"
	"github.com/japharyroman/fuelgrid-os/internal/exportjobs"
	"github.com/japharyroman/fuelgrid-os/internal/fleet"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
	"github.com/japharyroman/fuelgrid-os/internal/identity/ratelimit"
	"github.com/japharyroman/fuelgrid-os/internal/identity/repo"
	"github.com/japharyroman/fuelgrid-os/internal/incidents"
	"github.com/japharyroman/fuelgrid-os/internal/inventory"
	"github.com/japharyroman/fuelgrid-os/internal/notifications"
	"github.com/japharyroman/fuelgrid-os/internal/nozzles"
	"github.com/japharyroman/fuelgrid-os/internal/observability"
	"github.com/japharyroman/fuelgrid-os/internal/operations"
	"github.com/japharyroman/fuelgrid-os/internal/payables"
	"github.com/japharyroman/fuelgrid-os/internal/payments"
	"github.com/japharyroman/fuelgrid-os/internal/payments/mpesa"
	"github.com/japharyroman/fuelgrid-os/internal/pricing"
	"github.com/japharyroman/fuelgrid-os/internal/procurement"
	"github.com/japharyroman/fuelgrid-os/internal/products"
	"github.com/japharyroman/fuelgrid-os/internal/pumps"
	"github.com/japharyroman/fuelgrid-os/internal/readings"
	"github.com/japharyroman/fuelgrid-os/internal/receivables"
	"github.com/japharyroman/fuelgrid-os/internal/reconciliation"
	"github.com/japharyroman/fuelgrid-os/internal/regions"
	"github.com/japharyroman/fuelgrid-os/internal/retention"
	"github.com/japharyroman/fuelgrid-os/internal/revenue"
	"github.com/japharyroman/fuelgrid-os/internal/risk"
	"github.com/japharyroman/fuelgrid-os/internal/scheduler"
	setupdomain "github.com/japharyroman/fuelgrid-os/internal/setup"
	"github.com/japharyroman/fuelgrid-os/internal/stations"
	"github.com/japharyroman/fuelgrid-os/internal/tanks"
	"github.com/japharyroman/fuelgrid-os/internal/workforce"
	"github.com/japharyroman/fuelgrid-os/services/api/internal/config"
)

// Deps groups the backing services the API depends on. DB and Redis may be
// nil for thin smoke tests — the readiness probe skips probes for nil deps.
// Identity and Policy must be non-nil whenever auth/admin routes are
// reachable.
type Deps struct {
	DB *database.Pool
	// AppDB is the pool request-scoped queries run against. When it is the
	// non-owner fuelgrid_app pool (DATABASE_APP_URL set) Postgres RLS enforces
	// tenant isolation per request; when nil or equal to DB, RLS is bypassed
	// (the owner pool — current behaviour). DB always stays the owner pool for
	// pre-auth identity reads and cross-tenant background jobs.
	AppDB    *database.Pool
	Redis    *cache.Client
	Identity *identity.Service
	Policy   *policy.Service
	Metrics  *observability.Metrics
	// Email delivers transactional mail (password reset, invite). May be nil in
	// thin smoke tests; the handlers treat it as best-effort and skip when nil.
	Email email.Sender
}

// Server owns the chi router and the embedded *http.Server. It is the
// composition root for every middleware and route the API exposes.
type Server struct {
	cfg        config.Config
	logger     *slog.Logger
	deps       Deps
	appDB      *database.Pool // request-scoped query pool (fuelgrid_app when RLS on, else owner)
	rlsEnabled bool           // true when appDB is the non-owner pool, so requests are tenant-scoped
	identity   *identity.Service
	policy     *policy.Service
	metrics    *observability.Metrics
	email      email.Sender
	rateLimit  *rateLimiter

	// Readiness-aware load shedding (REL-5). dbHealthy is the cached DB health
	// observed by the background checker; it defaults to true so the shedding
	// middleware passes traffic through until an actual failed ping flips it.
	// The checker is started by Start() and stopped by Shutdown(); stopHealthcheck
	// cancels its context and healthcheckDone is closed when its goroutine exits.
	dbHealthy       atomic.Bool
	stopHealthcheck context.CancelFunc
	healthcheckDone chan struct{}

	accounting     *accounting.Repo
	attachments    *attachments.Repo
	banking        *banking.Repo
	enterprise     *enterprise.Repo
	expenses       *expenses.Repo
	exportJobs     *exportjobs.Repo
	fleet          *fleet.Repo
	companies      *companies.Repo
	branding       *branding.Repo
	regions        *regions.Repo
	stations       *stations.Repo
	products       *products.Repo
	tanks          *tanks.Repo
	pumps          *pumps.Repo
	nozzles        *nozzles.Repo
	calibration    *calibration.Repo
	incidents      *incidents.Repo
	operations     *operations.Repo
	readings       *readings.Repo
	inventory      *inventory.Repo
	payables       *payables.Repo
	payments       *payments.Repo
	mpesa          *mpesa.Client
	pricing        *pricing.Repo
	procurement    *procurement.Repo
	receivables    *receivables.Repo
	reconciliation *reconciliation.Repo
	retention      *retention.Repo
	revenue        *revenue.Repo
	risk           *risk.Repo
	notifications  *notifications.Repo
	notifPrefs     *notifications.PreferenceRepo
	jobRuns        *scheduler.ReadRepo
	setup          *setupdomain.Repo
	workforce      *workforce.Repo
	userRepo       *repo.UserRepo
	sessionRepo    *repo.SessionRepo

	router chi.Router
	http   *http.Server
}

// New wires the router, middleware stack, and route table for the API.
// It does not start the listener — call Start for that.
func New(cfg config.Config, logger *slog.Logger, deps Deps) *Server {
	// How many trusted proxies sit in front of the API — drives clientIP's
	// X-Forwarded-For handling for audit and rate-limit bucketing (AUTH-09).
	trustedProxyDepth.Store(int64(cfg.TrustedProxyDepth))

	s := &Server{
		cfg:      cfg,
		logger:   logger,
		deps:     deps,
		identity: deps.Identity,
		policy:   deps.Policy,
		metrics:  deps.Metrics,
		email:    deps.Email,
	}

	// Default to healthy so the load-shedding middleware (REL-5) passes traffic
	// through until the background checker observes a failed DB ping. This keeps
	// shedding OFF in any context that builds the Server but never starts the
	// checker (the integration harness) and in thin smoke deployments (DB nil).
	s.dbHealthy.Store(true)

	// Request throttling (REL-4). The per-tenant limiter reuses the Redis-backed
	// ratelimit.Limiter under its own key prefix; it is only built when Redis is
	// wired, so a thin smoke deployment (no Redis) keeps the per-tenant guard
	// off while the in-flight cap can still apply. Both guards self-disable on a
	// non-positive limit (the Go zero value), so a Config built without Load()
	// — as the integration harness does — never throttles test traffic.
	var tenantLimiter, pwResetLimiter *ratelimit.Limiter
	if deps.Redis != nil {
		tenantLimiter = ratelimit.New(deps.Redis, "ratelimit:tenant:")
		// SR-L3: per-IP guard on the public password-reset endpoints. Own prefix
		// so it shares no counter with the per-tenant or login buckets.
		pwResetLimiter = ratelimit.New(deps.Redis, "ratelimit:pwreset:")
	}
	s.rateLimit = newRateLimiter(tenantLimiter, cfg.TenantRateLimit, cfg.TenantRateWindow, cfg.MaxInflight).
		withPasswordReset(pwResetLimiter, cfg.AuthPasswordResetRateMax, cfg.AuthPasswordResetRateWindow)

	// M-Pesa (Daraja) client. Built unconditionally (no DB dependency) so the
	// payments handlers always have a client to call; when MPESA_CONSUMER_KEY/
	// SECRET are unset the constructor returns a disabled no-op that fails calls
	// with ErrDisabled instead of dialing Safaricom — a safe no-op in dev/CI.
	s.mpesa = mpesa.New(mpesa.Config{
		ConsumerKey:    cfg.MpesaConsumerKey.Reveal(),
		ConsumerSecret: cfg.MpesaConsumerSecret.Reveal(),
		Shortcode:      cfg.MpesaShortcode,
		Passkey:        cfg.MpesaPasskey.Reveal(),
		Env:            cfg.MpesaEnv,
		CallbackURL:    cfg.MpesaCallbackURL,
	}, logger.With("component", "mpesa"))

	// appDB backs request-scoped queries. When DATABASE_APP_URL is configured
	// it is the non-owner fuelgrid_app pool and RLS is enforced per request;
	// otherwise it falls back to the owner pool (RLS bypassed, unchanged).
	s.appDB = deps.AppDB
	if s.appDB == nil {
		s.appDB = deps.DB
	}
	s.rlsEnabled = deps.AppDB != nil && deps.AppDB != deps.DB

	// Admin / domain repos only get built when the pool is up. Handlers
	// gate themselves on s.deps.DB == nil checks at registration time.
	if deps.DB != nil {
		s.accounting = accounting.New(deps.DB)
		s.attachments = attachments.New(deps.DB)
		s.banking = banking.New(deps.DB)
		s.enterprise = enterprise.New(deps.DB)
		s.expenses = expenses.New(deps.DB)
		s.exportJobs = exportjobs.New(deps.DB)
		s.fleet = fleet.New(deps.DB, cfg.AuthPasswordPepper.Reveal())
		s.companies = companies.New(deps.DB)
		s.branding = branding.New(deps.DB)
		s.regions = regions.New(deps.DB)
		s.stations = stations.New(deps.DB)
		s.products = products.New(deps.DB)
		s.tanks = tanks.New(deps.DB)
		s.pumps = pumps.New(deps.DB)
		s.nozzles = nozzles.New(deps.DB)
		s.calibration = calibration.New(deps.DB)
		s.incidents = incidents.New(deps.DB)
		s.operations = operations.New(deps.DB)
		s.readings = readings.New(deps.DB)
		s.inventory = inventory.New(deps.DB)
		s.payables = payables.New(deps.DB)
		s.payments = payments.New(deps.DB)
		s.pricing = pricing.New(deps.DB)
		s.procurement = procurement.New(deps.DB)
		s.receivables = receivables.New(deps.DB)
		s.reconciliation = reconciliation.New(deps.DB)
		s.retention = retention.New(deps.DB)
		s.revenue = revenue.New(deps.DB)
		s.risk = risk.New(deps.DB)
		s.notifications = notifications.New(deps.DB)
		s.notifPrefs = notifications.NewPreferenceRepo(deps.DB)
		// job_runs is an owner-only SYSTEM table (no RLS); the scheduler read
		// repo always runs on the owner pool (deps.DB), never appDB.
		s.jobRuns = scheduler.NewReadRepo(deps.DB)
		s.setup = setupdomain.New(deps.DB)
		s.workforce = workforce.New(deps.DB)
		s.userRepo = repo.NewUserRepo(deps.DB)
		s.sessionRepo = repo.NewSessionRepo(deps.DB)
	}

	r := chi.NewRouter()

	// Recoverer must wrap the entire chain so a panic in *any* downstream
	// middleware (logger, metrics) — not just in a route handler — is turned
	// into a clean 500 instead of crashing the connection. RequestID and its
	// echo run first so the recovered request still carries an X-Request-Id
	// for correlation; everything after Recoverer is panic-protected (arch-05).
	r.Use(chimiddleware.RequestID)
	r.Use(echoRequestID)
	r.Use(chimiddleware.Recoverer)
	r.Use(s.captureErrors)
	// Open one OTel server span per request (OBS-5). It sits just inside
	// captureErrors and outside logRequests so the span (and its TraceID) is
	// live when logRequests stamps correlation_id, letting logs and traces
	// join on the same id. No-op when tracing is disabled (OtelExporter=none).
	r.Use(s.traceRequests)
	r.Use(s.logRequests)
	r.Use(s.recordMetrics)
	// Global concurrency guard (REL-4): shed load with 503 before doing real
	// work when too many requests are already in flight. Sits after metrics so
	// shed responses are still counted, and skips the operational probes. No-op
	// when API_MAX_INFLIGHT is unset/0.
	r.Use(s.limitInflight)
	// Readiness-aware load shedding (REL-5): when the background checker has
	// observed the database unreachable, shed regular traffic fast with 503 +
	// Retry-After (a single atomic read — no DB call in the hot path) instead of
	// letting every request block on a dead dependency and pile up. Sits beside
	// the in-flight cap and skips the operational probes so health stays visible
	// and the orchestrator can route recovery. No-op until an observed failed
	// ping flips the flag, so it never sheds when the checker isn't running.
	r.Use(s.shedWhenUnhealthy)
	r.Use(chimiddleware.Timeout(30 * time.Second))
	r.Use(limitRequestBody)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: cfg.CORSOrigins,
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders: []string{"Authorization", "Content-Type", "X-Request-Id"},
		ExposedHeaders: []string{"X-Request-Id"},
		// Sessions are carried as Authorization: Bearer tokens, not cookies,
		// so credentialed CORS is unnecessary — and enabling it alongside a
		// permissive origin list is the classic CORS footgun (AUTH-27).
		AllowCredentials: false,
		MaxAge:           300,
	}))

	s.registerRoutes(r)

	s.router = r
	s.http = &http.Server{
		Addr:              cfg.Addr(),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return s
}

// Router returns the built chi router so the registered route table can be
// walked (e.g. by the route enumerator and the route-vs-OpenAPI contract
// test). The router is fully assembled in New and registers regardless of
// whether the backing DB is wired, so it is safe to walk with nil Deps.
func (s *Server) Router() chi.Router { return s.router }

// Start blocks on http.ListenAndServe. It returns nil on graceful
// shutdown via Shutdown; any other error is propagated.
func (s *Server) Start() error {
	// Kick off the background DB health checker (REL-5) before we start
	// serving so the cached health flag is primed. No-op when deps.DB is nil.
	s.startHealthcheck()
	s.logger.Info("api listening", "addr", s.cfg.Addr())
	if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown drains in-flight requests within the deadline carried by ctx, then
// stops the background health checker so its goroutine is never leaked.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("api shutting down")
	err := s.http.Shutdown(ctx)
	s.stopHealthcheckChecker()
	return err
}
