package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// WithTenant runs fn inside a transaction that has app.current_tenant set
// to tenantID. Any tenant-scoped row-level security policies on the tables
// fn touches are therefore active for the duration of the call.
//
// Use this when:
//
//   - The caller wants the database itself to enforce tenant scope as a
//     belt-and-braces companion to repo-layer WHERE clauses.
//   - Or the caller is going to write to multiple tenant-scoped tables
//     in one transaction and wants RLS' WITH CHECK to fail closed if any
//     row's tenant_id slips.
//
// The setting is scoped via SET LOCAL, so the new value lives only for
// the lifetime of the transaction — it cannot leak back to the shared
// connection in the pool.
//
// Errors returned by fn cause Rollback; any non-nil return aborts the tx.
// On success the transaction is committed.
func WithTenant(ctx context.Context, pool *Pool, tenantID uuid.UUID, fn func(pgx.Tx) error) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("database: WithTenant called with zero tenant ID")
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("database: begin tx: %w", err)
	}
	defer func() {
		// Rollback is safe to call after Commit (it returns ErrTxClosed,
		// which we ignore) and ensures we never leak an open transaction
		// on a panic or early return.
		_ = tx.Rollback(ctx)
	}()

	// SET LOCAL does not accept bind parameters, so we interpolate the
	// UUID. uuid.UUID.String() is a fixed 36-char hex-with-dashes format
	// — no quoting / injection vector.
	if _, err := tx.Exec(ctx,
		fmt.Sprintf("SET LOCAL app.current_tenant = '%s'", tenantID.String()),
	); err != nil {
		return fmt.Errorf("database: set tenant: %w", err)
	}

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
