// Package server owns the HTTP composition root: router, middleware
// stack, route table, and graceful lifecycle.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/japharyroman/fuelgrid-os/internal/cache"
	"github.com/japharyroman/fuelgrid-os/internal/companies"
	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
	"github.com/japharyroman/fuelgrid-os/internal/identity/repo"
	"github.com/japharyroman/fuelgrid-os/internal/observability"
	"github.com/japharyroman/fuelgrid-os/internal/regions"
	"github.com/japharyroman/fuelgrid-os/internal/stations"
	"github.com/japharyroman/fuelgrid-os/services/api/internal/config"
)

// Deps groups the backing services the API depends on. DB and Redis may be
// nil for thin smoke tests — the readiness probe skips probes for nil deps.
// Identity and Policy must be non-nil whenever auth/admin routes are
// reachable.
type Deps struct {
	DB       *database.Pool
	Redis    *cache.Client
	Identity *identity.Service
	Policy   *policy.Service
	Metrics  *observability.Metrics
}

// Server owns the chi router and the embedded *http.Server. It is the
// composition root for every middleware and route the API exposes.
type Server struct {
	cfg      config.Config
	logger   *slog.Logger
	deps     Deps
	identity *identity.Service
	policy   *policy.Service
	metrics  *observability.Metrics

	companies   *companies.Repo
	regions     *regions.Repo
	stations    *stations.Repo
	userRepo    *repo.UserRepo
	sessionRepo *repo.SessionRepo

	http *http.Server
}

// New wires the router, middleware stack, and route table for the API.
// It does not start the listener — call Start for that.
func New(cfg config.Config, logger *slog.Logger, deps Deps) *Server {
	s := &Server{
		cfg:      cfg,
		logger:   logger,
		deps:     deps,
		identity: deps.Identity,
		policy:   deps.Policy,
		metrics:  deps.Metrics,
	}

	// Admin / domain repos only get built when the pool is up. Handlers
	// gate themselves on s.deps.DB == nil checks at registration time.
	if deps.DB != nil {
		s.companies = companies.New(deps.DB)
		s.regions = regions.New(deps.DB)
		s.stations = stations.New(deps.DB)
		s.userRepo = repo.NewUserRepo(deps.DB)
		s.sessionRepo = repo.NewSessionRepo(deps.DB)
	}

	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(echoRequestID)
	r.Use(s.logRequests)
	r.Use(s.recordMetrics)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(30 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOrigins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Request-Id"},
		ExposedHeaders:   []string{"X-Request-Id"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	r.Get("/metrics", s.handleMetrics)

	r.Route("/api/v1", func(r chi.Router) {
		// Platform provisioning — its own static-token auth, not user
		// sessions. Mounted regardless of identity wiring; the middleware
		// 404s when PLATFORM_ADMIN_TOKEN is unset.
		r.With(s.requirePlatformAdmin).Post("/platform/tenants", s.handleCreateTenant)

		if s.identity != nil {
			r.Route("/auth", func(r chi.Router) {
				r.Post("/login", s.handleLogin)
				r.Post("/logout", s.handleLogout)
				r.Post("/refresh", s.handleRefresh)
				r.Post("/password-reset/request", s.handlePasswordResetRequest)
				r.Post("/password-reset/confirm", s.handlePasswordResetConfirm)

				r.Group(func(r chi.Router) {
					r.Use(s.requireAuth)
					r.Post("/mfa/enroll", s.handleMfaEnroll)
					r.Post("/mfa/verify", s.handleMfaVerify)
				})
			})

			// Authenticated routes (no specific permission gate beyond
			// having a session).
			r.Group(func(r chi.Router) {
				r.Use(s.requireAuth)
				r.Get("/me", s.handleMe)
				if s.policy != nil {
					r.Get("/me/permissions", s.handleMePermissions)
				}
				if s.sessionRepo != nil {
					r.Get("/me/sessions", s.handleListMySessions)
					r.Delete("/me/sessions/{sessionID}", s.handleRevokeMySession)
					r.Post("/me/password", s.handleChangeMyPassword)
				}
			})

			if s.policy != nil {
				// Station read (existing Stage-5 endpoint, now backed by
				// the proper stations repo).
				r.Group(func(r chi.Router) {
					r.Use(s.requireAuth)
					r.With(s.requirePermission("station.read", stationFromURLParam("stationID"))).
						Get("/stations/{stationID}", s.handleGetStation)

					r.With(s.requirePermission("audit.read", nil)).
						Get("/audit-logs", s.handleListAuditLogs)

					r.With(s.requirePermission("users.assign_roles", nil)).
						Post("/admin/users/{userID}/roles", s.handleGrantRole)
				})

				// Admin console surface. Everything beyond this point
				// is tenant-wide and writes audit + outbox via the
				// audit.WriteWithOutbox helper.
				if s.companies != nil {
					r.Group(func(r chi.Router) {
						r.Use(s.requireAuth)

						r.With(s.requirePermission("station.read", nil)).
							Get("/companies", s.handleListCompanies)
						r.With(s.requirePermission("companies.manage", nil)).Group(func(r chi.Router) {
							r.Post("/companies", s.handleCreateCompany)
							r.Patch("/companies/{id}", s.handleUpdateCompany)
							r.Delete("/companies/{id}", s.handleDeleteCompany)
						})

						r.With(s.requirePermission("station.read", nil)).
							Get("/regions", s.handleListRegions)
						r.With(s.requirePermission("regions.manage", nil)).Group(func(r chi.Router) {
							r.Post("/regions", s.handleCreateRegion)
							r.Patch("/regions/{id}", s.handleUpdateRegion)
							r.Delete("/regions/{id}", s.handleDeleteRegion)
						})

						r.With(s.requirePermission("station.read", nil)).
							Get("/stations", s.handleListStations)
						r.With(s.requirePermission("station.manage", nil)).Group(func(r chi.Router) {
							r.Post("/stations", s.handleCreateStation)
							r.Patch("/stations/{stationID}", s.handleUpdateStation)
							r.Delete("/stations/{stationID}", s.handleDeleteStation)
						})

						r.With(s.requirePermission("users.manage", nil)).
							Get("/users", s.handleListUsers)
						r.With(s.requirePermission("users.invite", nil)).
							Post("/admin/users", s.handleInviteUser)
						r.With(s.requirePermission("users.manage", nil)).
							Patch("/admin/users/{userID}/status", s.handleUpdateUserStatus)
						r.With(s.requirePermission("users.assign_roles", nil)).
							Delete("/admin/users/{userID}/roles/{roleCode}", s.handleRevokeUserRole)
						r.With(s.requirePermission("users.assign_roles", nil)).Group(func(r chi.Router) {
							r.Post("/admin/users/{userID}/station-access", s.handleGrantStationAccess)
							r.Delete("/admin/users/{userID}/station-access/{stationID}", s.handleRevokeStationAccess)
						})

						r.With(s.requirePermission("users.manage", nil)).
							Get("/roles", s.handleListRoles)
					})
				}
			}
		}
	})

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

// Start blocks on http.ListenAndServe. It returns nil on graceful
// shutdown via Shutdown; any other error is propagated.
func (s *Server) Start() error {
	s.logger.Info("api listening", "addr", s.cfg.Addr())
	if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown drains in-flight requests within the deadline carried by ctx.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("api shutting down")
	return s.http.Shutdown(ctx)
}
