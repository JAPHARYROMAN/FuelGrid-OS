// Command seed inserts a small demo dataset into a fresh FuelGrid OS
// database: one tenant, one company, one region, one station.
//
// Idempotent: re-running it does nothing if the demo slug already exists.
//
// Usage:
//
//	DATABASE_URL=postgres://... go run ./services/api/cmd/seed
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/services/api/internal/database"
)

const demoTenantSlug = "demo"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		logger.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := database.Connect(ctx, database.Config{URL: url})
	if err != nil {
		return err
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var existing string
	err = tx.QueryRow(ctx,
		`SELECT id FROM tenants WHERE slug = $1`,
		demoTenantSlug,
	).Scan(&existing)
	if err == nil {
		slog.Info("demo tenant already present, skipping", "tenant_id", existing)
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	var tenantID, companyID, regionID, stationID string

	if err := tx.QueryRow(ctx, `
		INSERT INTO tenants (name, slug)
		VALUES ('FuelGrid Demo Co.', $1)
		RETURNING id
	`, demoTenantSlug).Scan(&tenantID); err != nil {
		return err
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO companies (tenant_id, name, legal_name, currency, timezone)
		VALUES ($1, 'Demo Petroleum Ltd', 'Demo Petroleum Limited', 'USD', 'Africa/Dar_es_Salaam')
		RETURNING id
	`, tenantID).Scan(&companyID); err != nil {
		return err
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO regions (tenant_id, company_id, name, code)
		VALUES ($1, $2, 'Dar es Salaam', 'DAR')
		RETURNING id
	`, tenantID, companyID).Scan(&regionID); err != nil {
		return err
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO stations (tenant_id, company_id, region_id, name, code,
		                     city, country, latitude, longitude, timezone)
		VALUES ($1, $2, $3, 'Mikocheni Service Station', 'MIK-01',
		        'Dar es Salaam', 'Tanzania', -6.7700000, 39.2400000, 'Africa/Dar_es_Salaam')
		RETURNING id
	`, tenantID, companyID, regionID).Scan(&stationID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	slog.Info("seeded demo data",
		"tenant_id", tenantID,
		"company_id", companyID,
		"region_id", regionID,
		"station_id", stationID,
	)
	return nil
}
