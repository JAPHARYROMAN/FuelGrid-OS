// Package database provides the Postgres connection pool used by the API
// service and a thin Ping helper for readiness probes.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds Postgres pool tuning. Zero values fall back to pgx defaults.
type Config struct {
	URL             string
	MaxOpenConns    int32
	MinIdleConns    int32
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// Pool is the shared connection pool type used across the service.
type Pool = pgxpool.Pool

// Connect creates the pool, applies tuning, and verifies the database is
// reachable with a short timeout. The caller is responsible for Close().
func Connect(ctx context.Context, cfg Config) (*Pool, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("database url is required")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse pool config: %w", err)
	}

	if cfg.MaxOpenConns > 0 {
		poolCfg.MaxConns = cfg.MaxOpenConns
	}
	if cfg.MinIdleConns > 0 {
		poolCfg.MinConns = cfg.MinIdleConns
	}
	if cfg.ConnMaxLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.ConnMaxLifetime
	}
	if cfg.ConnMaxIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.ConnMaxIdleTime
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("initial ping: %w", err)
	}

	return pool, nil
}
