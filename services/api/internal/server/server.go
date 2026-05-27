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

	"github.com/japharyroman/fuelgrid-os/services/api/internal/config"
)

// Server owns the chi router and the embedded *http.Server. It is the
// composition root for every middleware and route the API exposes.
type Server struct {
	cfg    config.Config
	logger *slog.Logger
	http   *http.Server
}

// New wires the router, middleware stack, and route table for the API.
// It does not start the listener — call Start for that.
func New(cfg config.Config, logger *slog.Logger) *Server {
	s := &Server{cfg: cfg, logger: logger}

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
