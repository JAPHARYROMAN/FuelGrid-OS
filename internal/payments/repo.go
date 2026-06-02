// Package payments is the data layer for shift tender records (Phase 6,
// Stage 5) — discrete per-tender payments reconciled against recognized
// revenue. Money is carried as decimal strings.
package payments

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

type Payment struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	StationID  uuid.UUID
	ShiftID    *uuid.UUID
	CustomerID *uuid.UUID
	TenderType string
	Amount     string
	Reference  *string
	ReceivedBy uuid.UUID
	ReceivedAt time.Time
	Status     string
	Notes      *string
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
    reference, received_by, received_at, status, notes
`

func scan(row pgx.Row, p *Payment) error {
	return row.Scan(
		&p.ID, &p.TenantID, &p.StationID, &p.ShiftID, &p.CustomerID, &p.TenderType, &p.Amount,
		&p.Reference, &p.ReceivedBy, &p.ReceivedAt, &p.Status, &p.Notes,
	)
}

func (r *Repo) Record(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in RecordInput) (*Payment, error) {
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
	return &p, nil
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
