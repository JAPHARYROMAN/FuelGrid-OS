// Package database provides the Postgres connection pool used by the API
// service and a thin Ping helper for readiness probes.
package database

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds Postgres pool tuning. Zero values fall back to pgx defaults
// (or, for the two server-side timeouts, to the conservative defaults below).
type Config struct {
	URL             string
	MaxOpenConns    int32
	MinIdleConns    int32
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	// StatementTimeout bounds any single query; IdleInTxTimeout bounds how
	// long a transaction may sit idle. Both protect the pool from a single
	// runaway query/transaction pinning a connection. Zero -> default.
	StatementTimeout time.Duration
	IdleInTxTimeout  time.Duration
}

// Pool is the shared connection pool type used across the service.
type Pool = pgxpool.Pool

// Querier is the read/write surface shared by *pgxpool.Pool and pgx.Tx.
// Repo methods take a Querier so the same code runs either auto-committed
// (pass the pool) or inside a caller's transaction (pass the tx). This is
// what lets the identity service wrap a state change + its audit + outbox
// rows in a single transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

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

	// Server-side timeouts, sent as startup parameters on every pooled
	// connection. A single runaway query (statement_timeout) or an
	// abandoned open transaction (idle_in_transaction_session_timeout) can
	// otherwise pin a connection indefinitely and exhaust the pool.
	stmtTimeout := cfg.StatementTimeout
	if stmtTimeout <= 0 {
		stmtTimeout = 30 * time.Second
	}
	idleTxTimeout := cfg.IdleInTxTimeout
	if idleTxTimeout <= 0 {
		idleTxTimeout = 60 * time.Second
	}
	if poolCfg.ConnConfig.RuntimeParams == nil {
		poolCfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	poolCfg.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(stmtTimeout.Milliseconds(), 10)
	poolCfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = strconv.FormatInt(idleTxTimeout.Milliseconds(), 10)

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
