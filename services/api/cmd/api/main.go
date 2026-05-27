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

	"github.com/japharyroman/fuelgrid-os/services/api/internal/cache"
	"github.com/japharyroman/fuelgrid-os/services/api/internal/config"
	"github.com/japharyroman/fuelgrid-os/services/api/internal/database"
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

	return deps, cleanup, nil
}
