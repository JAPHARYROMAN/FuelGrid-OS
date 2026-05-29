// Package database provides the Postgres connection pool used by the API
// service and a thin Ping helper for readiness probes.
package database

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
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

// Pool wraps a pgxpool.Pool so RLS can be enforced per request. When the
// request context carries a tenant-scoped connection (see AcquireTenant /
// WithScopedConn), every Query/Exec/QueryRow/Begin issued through the pool runs
// on THAT connection — which has app.current_tenant set — so Postgres RLS
// isolates the tenant. With no scoped connection in context (login, background
// jobs, unauthenticated paths) it delegates straight to the underlying pool, so
// behaviour is identical to a bare pgxpool.Pool. The scoped connection may be
// acquired from a different pool (the non-owner fuelgrid_app pool) than the one
// the wrapper fronts; the wrapper simply prefers whatever connection the
// context provides.
type Pool struct {
	*pgxpool.Pool
}

type ctxKey int

const scopedConnKey ctxKey = iota

// WithScopedConn returns a context carrying a tenant-scoped connection. Pool
// methods run on this connection instead of acquiring a fresh one.
func WithScopedConn(ctx context.Context, conn *pgxpool.Conn) context.Context {
	return context.WithValue(ctx, scopedConnKey, conn)
}

func scopedConn(ctx context.Context) *pgxpool.Conn {
	c, _ := ctx.Value(scopedConnKey).(*pgxpool.Conn)
	return c
}

// Query runs on the context's scoped connection when present, else the pool.
func (p *Pool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if c := scopedConn(ctx); c != nil {
		return c.Query(ctx, sql, args...)
	}
	return p.Pool.Query(ctx, sql, args...)
}

// Exec runs on the context's scoped connection when present, else the pool.
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if c := scopedConn(ctx); c != nil {
		return c.Exec(ctx, sql, args...)
	}
	return p.Pool.Exec(ctx, sql, args...)
}

// QueryRow runs on the context's scoped connection when present, else the pool.
func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if c := scopedConn(ctx); c != nil {
		return c.QueryRow(ctx, sql, args...)
	}
	return p.Pool.QueryRow(ctx, sql, args...)
}

// Begin starts a transaction on the context's scoped connection when present
// (so the transaction inherits app.current_tenant), else on the pool.
func (p *Pool) Begin(ctx context.Context) (pgx.Tx, error) {
	if c := scopedConn(ctx); c != nil {
		return c.Begin(ctx)
	}
	return p.Pool.Begin(ctx)
}

// BeginTx is Begin with explicit options.
func (p *Pool) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	if c := scopedConn(ctx); c != nil {
		return c.BeginTx(ctx, opts)
	}
	return p.Pool.BeginTx(ctx, opts)
}

// AcquireTenant checks out a connection from this pool, sets app.current_tenant
// on it (session scope, so it persists across the request's queries and any
// transactions on the connection), and returns a context carrying the
// connection plus a release func. Call release when the request finishes: it
// resets the GUC and returns the connection to the pool. A connection broken
// mid-request is discarded by the pool on Release, so the tenant setting can
// never leak to another tenant's request.
func (p *Pool) AcquireTenant(ctx context.Context, tenantID uuid.UUID) (context.Context, func(), error) {
	conn, err := p.Pool.Acquire(ctx)
	if err != nil {
		return ctx, func() {}, err
	}
	// SET LOCAL needs a tx; we use a session SET and reset on release. The UUID
	// is fixed-format, so there is no injection vector in the interpolation.
	if _, err := conn.Exec(ctx, "SET app.current_tenant = '"+tenantID.String()+"'"); err != nil {
		conn.Release()
		return ctx, func() {}, err
	}
	release := func() {
		_, _ = conn.Exec(context.Background(), "RESET app.current_tenant")
		conn.Release()
	}
	return WithScopedConn(ctx, conn), release, nil
}

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

	return &Pool{Pool: pool}, nil
}
