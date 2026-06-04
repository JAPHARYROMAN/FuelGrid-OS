// Package payments is the data layer for shift tender records (Phase 6,
// Stage 5) — discrete per-tender payments reconciled against recognized
// revenue. Money is carried as decimal strings.
package payments

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

type Payment struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	StationID      uuid.UUID
	ShiftID        *uuid.UUID
	CustomerID     *uuid.UUID
	TenderType     string
	Amount         string
	Reference      *string
	ReceivedBy     uuid.UUID
	ReceivedAt     time.Time
	Status         string
	Notes          *string
	IdempotencyKey *string
}

type RecordInput struct {
	StationID  uuid.UUID
	ShiftID    *uuid.UUID
	CustomerID *uuid.UUID
	TenderType string
	Amount     string
	Reference  *string
	ReceivedBy uuid.UUID
	Notes      *string
	// IdempotencyKey is an optional client-supplied key (SR-M2). When set, a
	// replay carrying the same key for the same tenant returns the
	// already-recorded payment instead of inserting a duplicate. When nil, the
	// prior behaviour (always insert) is preserved.
	IdempotencyKey *string
}

// ShiftReconciliation compares a shift's recorded tenders to its recognized
// revenue.
type ShiftReconciliation struct {
	Tendered   string
	Recognized string
	Variance   string
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const columns = `
    id, tenant_id, station_id, shift_id, customer_id, tender_type, amount::text,
    reference, received_by, received_at, status, notes, idempotency_key
`

func scan(row pgx.Row, p *Payment) error {
	return row.Scan(
		&p.ID, &p.TenantID, &p.StationID, &p.ShiftID, &p.CustomerID, &p.TenderType, &p.Amount,
		&p.Reference, &p.ReceivedBy, &p.ReceivedAt, &p.Status, &p.Notes, &p.IdempotencyKey,
	)
}

// RecordResult is the outcome of Record. Replayed reports whether the returned
// payment is a pre-existing row matched by idempotency key (true) rather than a
// freshly inserted one (false). The handler uses it to skip side effects (AR
// charge, GL journal, audit event) on a replay so the amount is not applied
// twice — the whole point of SR-M2.
type RecordResult struct {
	Payment  *Payment
	Replayed bool
}

// Record inserts a payment. When in.IdempotencyKey is supplied and a payment
// already exists for (tenant_id, idempotency_key), the existing row is returned
// with Replayed=true instead of inserting a duplicate (SR-M2). The dedup is
// tenant-scoped: the same key under a different tenant inserts normally.
//
// It relies on the partial unique index uq_payments_tenant_idempotency_key
// (migration 0096) via INSERT ... ON CONFLICT, so the dedup is enforced by the
// database under concurrency, not by a check-then-insert race.
func (r *Repo) Record(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in RecordInput) (*RecordResult, error) {
	// No key supplied: preserve the prior always-insert behaviour.
	if in.IdempotencyKey == nil {
		var p Payment
		if err := scan(tx.QueryRow(ctx, `
			INSERT INTO payments
			    (tenant_id, station_id, shift_id, customer_id, tender_type, amount, reference, received_by, notes)
			VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8, $9)
			RETURNING `+columns,
			tenantID, in.StationID, in.ShiftID, in.CustomerID, in.TenderType, in.Amount,
			in.Reference, in.ReceivedBy, in.Notes,
		), &p); err != nil {
			return nil, err
		}
		return &RecordResult{Payment: &p, Replayed: false}, nil
	}

	// Key supplied: insert, but on a (tenant_id, idempotency_key) conflict do
	// nothing so we can return the already-recorded row unchanged. ON CONFLICT
	// DO NOTHING yields no RETURNING row, so an empty result means the key
	// already existed — we then SELECT the original.
	var p Payment
	err := scan(tx.QueryRow(ctx, `
		INSERT INTO payments
		    (tenant_id, station_id, shift_id, customer_id, tender_type, amount, reference, received_by, notes, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8, $9, $10)
		ON CONFLICT (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL
		DO NOTHING
		RETURNING `+columns,
		tenantID, in.StationID, in.ShiftID, in.CustomerID, in.TenderType, in.Amount,
		in.Reference, in.ReceivedBy, in.Notes, *in.IdempotencyKey,
	), &p)
	if err == nil {
		return &RecordResult{Payment: &p, Replayed: false}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	// Conflict: a payment with this key already exists for the tenant. Return it
	// so the caller responds idempotently rather than erroring.
	var existing Payment
	if err := scan(tx.QueryRow(ctx, `
		SELECT `+columns+` FROM payments
		WHERE tenant_id = $1 AND idempotency_key = $2
	`, tenantID, *in.IdempotencyKey), &existing); err != nil {
		return nil, err
	}
	return &RecordResult{Payment: &existing, Replayed: true}, nil
}

func (r *Repo) ListForShift(ctx context.Context, tenantID, shiftID uuid.UUID) ([]Payment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+` FROM payments
		WHERE tenant_id = $1 AND shift_id = $2 ORDER BY received_at
	`, tenantID, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Payment{}
	for rows.Next() {
		var p Payment
		if err := scan(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListForShiftPage returns a page of a shift's payments ordered by received_at
// (with id as a stable tiebreaker for consistent paging), applying the supplied
// limit and offset.
func (r *Repo) ListForShiftPage(ctx context.Context, tenantID, shiftID uuid.UUID, limit, offset int) ([]Payment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+` FROM payments
		WHERE tenant_id = $1 AND shift_id = $2 ORDER BY received_at, id
		LIMIT $3 OFFSET $4
	`, tenantID, shiftID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Payment{}
	for rows.Next() {
		var p Payment
		if err := scan(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ReconcileShift totals a shift's recorded tenders against recognized revenue
// (sales gross), all in SQL so the variance is exact.
func (r *Repo) ReconcileShift(ctx context.Context, q database.Querier, tenantID, shiftID uuid.UUID) (ShiftReconciliation, error) {
	var rec ShiftReconciliation
	err := q.QueryRow(ctx, `
		SELECT t.tendered::text, s.recognized::text, (t.tendered - s.recognized)::text
		FROM (SELECT COALESCE(SUM(amount), 0) AS tendered FROM payments
		        WHERE tenant_id = $1 AND shift_id = $2 AND status = 'recorded') t,
		     (SELECT COALESCE(SUM(gross_amount), 0) AS recognized FROM sales
		        WHERE tenant_id = $1 AND shift_id = $2) s
	`, tenantID, shiftID).Scan(&rec.Tendered, &rec.Recognized, &rec.Variance)
	return rec, err
}
