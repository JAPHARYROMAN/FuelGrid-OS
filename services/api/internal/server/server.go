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
	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
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
}

// Server owns the chi router and the embedded *http.Server. It is the
// composition root for every middleware and route the API exposes.
type Server struct {
	cfg      config.Config
	logger   *slog.Logger
	deps     Deps
	identity *identity.Service
	policy   *policy.Service
	http     *http.Server
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
	}

	r := chi.NewRouter()

	// Order matters: RequestID first so every later middleware can log it;
	// Recoverer near the top so panics in handlers don't take down the
	// process; CORS late enough that preflight failures still get logged.
	r.Use(chimiddleware.RequestID)
	r.Use(echoRequestID)
	// NOTE: chi's RealIP middleware was deliberately omitted — it blindly
	// trusts X-Forwarded-For / X-Real-IP, which is vulnerable to client
	// spoofing in front of any proxy that doesn't strip them. When we
	// deploy behind a controlled ingress we'll wire a tighter version
	// that only honors these headers from trusted ranges.
	r.Use(s.logRequests)
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

	r.Route("/api/v1", func(r chi.Router) {
		if s.identity != nil {
			r.Route("/auth", func(r chi.Router) {
				r.Post("/login", s.handleLogin)
				r.Post("/logout", s.handleLogout)
				r.Post("/refresh", s.handleRefresh)
				r.Post("/password-reset/request", s.handlePasswordResetRequest)
				r.Post("/password-reset/confirm", s.handlePasswordResetConfirm)

				// MFA enrollment is an authenticated action — the user
				// must already be logged in (with or without MFA).
				r.Group(func(r chi.Router) {
					r.Use(s.requireAuth)
					r.Post("/mfa/enroll", s.handleMfaEnroll)
					r.Post("/mfa/verify", s.handleMfaVerify)
				})
			})

			r.Group(func(r chi.Router) {
				r.Use(s.requireAuth)
				r.Get("/me", s.handleMe)
				if s.policy != nil {
					r.Get("/me/permissions", s.handleMePermissions)
				}
			})

			if s.policy != nil {
				r.Group(func(r chi.Router) {
					r.Use(s.requireAuth)
					r.With(s.requirePermission("station.read", stationFromURLParam("stationID"))).
						Get("/stations/{stationID}", s.handleGetStation)

					// Auditor-only: read audit_logs scoped to the actor's tenant.
					r.With(s.requirePermission("audit.read", nil)).
						Get("/audit-logs", s.handleListAuditLogs)

					// Admin actions: grant a system role to a user.
					r.With(s.requirePermission("users.assign_roles", nil)).
						Post("/admin/users/{userID}/roles", s.handleGrantRole)
				})
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
