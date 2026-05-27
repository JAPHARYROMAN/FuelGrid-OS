// Command api is the entry point for the FuelGrid OS API service.
// It loads config, builds the HTTP server, and runs until SIGTERM/SIGINT.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

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

	srv := server.New(cfg, logger)

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
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			return err
		}
		// Wait for Start to return after Shutdown was issued.
		if err := <-errCh; err != nil {
			return err
		}
		return nil
	}
}
