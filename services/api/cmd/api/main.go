// Command api is the entry point for the FuelGrid OS API service.
// It loads config, connects backing services, builds the HTTP server,
// and runs until SIGTERM/SIGINT.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/cache"
	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/events"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
	"github.com/japharyroman/fuelgrid-os/internal/identity/ratelimit"
	"github.com/japharyroman/fuelgrid-os/internal/identity/repo"
	"github.com/japharyroman/fuelgrid-os/internal/identity/session"
	"github.com/japharyroman/fuelgrid-os/services/api/internal/config"
	"github.com/japharyroman/fuelgrid-os/services/api/internal/logging"
	"github.com/japharyroman/fuelgrid-os/services/api/internal/server"
)

// Build-time metadata injected via -ldflags from the Makefile/Dockerfile.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := logging.New(cfg.LogLevel, cfg.LogFormat).With(
		"service", "fuelgrid-api",
		"version", version,
		"commit", commit,
	)
	slog.SetDefault(logger)
	logger.Info("starting", "env", cfg.Env, "addr", cfg.Addr())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps, cleanup, err := wireDeps(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer cleanup()

	srv := server.New(cfg, logger, deps)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-sigCh:
		logger.Info("signal received, shutting down", "signal", sig.String())
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancelShutdown()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-errCh; err != nil {
			return err
		}
		return nil
	}
}

// wireDeps connects backing services in parallel-friendly order and returns
// a cleanup function that releases them in reverse. Connection failures
// during boot are fatal — degraded mode is not the right default.
func wireDeps(ctx context.Context, cfg config.Config, logger *slog.Logger) (server.Deps, func(), error) {
	var deps server.Deps
	var cleanups []func()

	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	if cfg.DatabaseURL != "" {
		pool, err := database.Connect(ctx, database.Config{
			URL:             cfg.DatabaseURL,
			MaxOpenConns:    cfg.DatabaseMaxOpenConns,
			MinIdleConns:    cfg.DatabaseMinIdleConns,
			ConnMaxLifetime: cfg.DatabaseConnLifetime,
			ConnMaxIdleTime: cfg.DatabaseConnIdleTime,
		})
		if err != nil {
			cleanup()
			return deps, nil, errors.New("postgres connect: " + err.Error())
		}
		deps.DB = pool
		cleanups = append(cleanups, pool.Close)
		logger.Info("postgres connected")
	} else {
		logger.Warn("postgres skipped — DATABASE_URL is empty")
	}

	if cfg.RedisURL != "" {
		client, err := cache.Connect(ctx, cfg.RedisURL)
		if err != nil {
			cleanup()
			return deps, nil, errors.New("redis connect: " + err.Error())
		}
		deps.Redis = client
		cleanups = append(cleanups, func() { _ = client.Close() })
		logger.Info("redis connected")
	} else {
		logger.Warn("redis skipped — REDIS_URL is empty")
	}

	// Identity service requires both Postgres and Redis. If either is
	// missing, the auth routes simply don't get wired (handy for tests
	// that boot a thin API to hit /healthz).
	if deps.DB != nil && deps.Redis != nil {
		if cfg.AuthPasswordPepper == "" && cfg.Env != "development" {
			logger.Warn("AUTH_PASSWORD_PEPPER is unset — production deployments must set this from a secret store")
		}
		hasher := password.New(password.DefaultParams, cfg.AuthPasswordPepper)
		store := session.NewRedisStore(deps.Redis, "session:")
		limiter := ratelimit.New(deps.Redis, "ratelimit:")
		userRepo := repo.NewUserRepo(deps.DB)
		sessionRepo := repo.NewSessionRepo(deps.DB)

		deps.Identity = identity.NewService(
			identity.ServiceConfig{
				SessionTTL:       cfg.AuthSessionTTL,
				LoginLockAfter:   cfg.AuthLoginLockAfter,
				LoginLockFor:     cfg.AuthLoginLockFor,
				LoginRateMax:     cfg.AuthLoginRateMax,
				LoginRateWindow:  cfg.AuthLoginRateWindow,
				PasswordResetTTL: cfg.AuthPasswordResetTTL,
			},
			hasher,
			userRepo,
			sessionRepo,
			store,
			limiter,
			deps.Redis,
			logger,
		)
		logger.Info("identity service wired")

		deps.Policy = policy.NewService(policy.NewDBLoader(deps.DB))
		logger.Info("policy service wired")
	} else {
		logger.Warn("identity service skipped — needs both DATABASE_URL and REDIS_URL")
	}

	// Outbox publisher. Needs Postgres only. The in-process bus is the
	// dispatch target today; a Kafka/NATS replacement plugs in here later
	// without touching the producers (handlers / services).
	if deps.DB != nil {
		bus := events.NewInProcessBus(logger.With("component", "events.bus"))
		// Subscribe a catch-all that logs every event. Keeps a visible
		// trail in dev and CI until concrete consumers land.
		bus.Subscribe("*", func(_ context.Context, e events.Event) error {
			logger.Info("event dispatched",
				"event_id", e.ID,
				"event_type", e.Type,
				"tenant_id", e.TenantID,
				"aggregate_type", e.AggregateType,
				"aggregate_id", e.AggregateID,
			)
			return nil
		})

		publisher := events.NewPublisher(deps.DB, bus, events.PublisherConfig{
			PollInterval: cfg.OutboxPollInterval,
			BatchSize:    cfg.OutboxBatchSize,
		}, logger.With("component", "events.publisher"))
		publisher.Start()
		cleanups = append(cleanups, func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			if err := publisher.Stop(stopCtx); err != nil {
				logger.Warn("publisher stop", "error", err)
			}
		})
	}

	return deps, cleanup, nil
}
