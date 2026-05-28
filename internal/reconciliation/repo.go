// Package reconciliation is the data layer for tank_reconciliations — the
// per-tank-per-operating-day book-vs-physical sign-off (Phase 4, Stages 5-6).
//
// The compute orchestration (gathering opening book, period totals, the
// closing dip, and the variance) lives in the API layer, which coordinates
// the ledger, dip, product, and shift repos. This package owns persistence:
// upserting the draft, finding the balance-forward anchor, and sealing.
package reconciliation

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// Lifecycle statuses.
const (
	StatusDraft     = "draft"
	StatusException = "exception"
	StatusSealed    = "sealed"
)

// ErrNotFound is returned when a reconciliation doesn't resolve.
var ErrNotFound = errors.New("reconciliation: not found")

// Reconciliation is one (tank, operating_day) book-vs-physical record.
type Reconciliation struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	TankID           uuid.UUID
	OperatingDayID   uuid.UUID
	OpeningBook      float64
	DeliveriesTotal  float64
	SalesTotal       float64
	AdjustmentsTotal float64
	ClosingBook      float64
	ClosingPhysical  float64
	VarianceLitres   float64
	VariancePercent  float64
	TolerancePercent float64
	ThroughSeq       int64
	Status           string
	SealedBy         *uuid.UUID
	SealedAt         *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// DraftInput carries the computed figures for an upsert.
type DraftInput struct {
	TankID           uuid.UUID
	OperatingDayID   uuid.UUID
	OpeningBook      float64
	DeliveriesTotal  float64
	SalesTotal       float64
	AdjustmentsTotal float64
	ClosingBook      float64
	ClosingPhysical  float64
	VarianceLitres   float64
	VariancePercent  float64
	TolerancePercent float64
	ThroughSeq       int64
	Status           string // draft | exception
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const columns = `
    id, tenant_id, tank_id, operating_day_id, opening_book, deliveries_total,
    sales_total, adjustments_total, closing_book, closing_physical,
    variance_litres, variance_percent, tolerance_percent, through_seq,
    status, sealed_by, sealed_at, created_at, updated_at
`

func scan(row pgx.Row, r *Reconciliation) error {
	return row.Scan(
		&r.ID, &r.TenantID, &r.TankID, &r.OperatingDayID, &r.OpeningBook, &r.DeliveriesTotal,
		&r.SalesTotal, &r.AdjustmentsTotal, &r.ClosingBook, &r.ClosingPhysical,
		&r.VarianceLitres, &r.VariancePercent, &r.TolerancePercent, &r.ThroughSeq,
		&r.Status, &r.SealedBy, &r.SealedAt, &r.CreatedAt, &r.UpdatedAt,
	)
}

// Get returns a reconciliation by id within the tenant, or ErrNotFound.
func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Reconciliation, error) {
	var rec Reconciliation
	err := scan(r.pool.QueryRow(ctx, `
		SELECT `+columns+` FROM tank_reconciliations WHERE id = $1 AND tenant_id = $2
	`, id, tenantID), &rec)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// GetForTankDay returns the reconciliation for a (tank, operating_day), or
// ErrNotFound.
func (r *Repo) GetForTankDay(ctx context.Context, tenantID, tankID, dayID uuid.UUID) (*Reconciliation, error) {
	var rec Reconciliation
	err := scan(r.pool.QueryRow(ctx, `
		SELECT `+columns+`
		FROM tank_reconciliations
		WHERE tenant_id = $1 AND tank_id = $2 AND operating_day_id = $3
	`, tenantID, tankID, dayID), &rec)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// LastSealedForTank returns the tank's most recently sealed reconciliation —
// the balance-forward anchor whose closing_physical opens the next period — or
// ErrNotFound when the tank has never been reconciled.
func (r *Repo) LastSealedForTank(ctx context.Context, tenantID, tankID uuid.UUID) (*Reconciliation, error) {
	var rec Reconciliation
	err := scan(r.pool.QueryRow(ctx, `
		SELECT `+columns+`
		FROM tank_reconciliations
		WHERE tenant_id = $1 AND tank_id = $2 AND status = 'sealed'
		ORDER BY through_seq DESC
		LIMIT 1
	`, tenantID, tankID), &rec)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// ListForStationDay returns the day's reconciliations for every tank at a
// station (joined through tanks), tank code order.
func (r *Repo) ListForStationDay(ctx context.Context, tenantID, stationID, dayID uuid.UUID) ([]Reconciliation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+prefixedColumns+`
		FROM tank_reconciliations rec
		JOIN tanks t ON t.id = rec.tank_id AND t.tenant_id = rec.tenant_id
		WHERE rec.tenant_id = $1 AND t.station_id = $2 AND rec.operating_day_id = $3
		ORDER BY t.code
	`, tenantID, stationID, dayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reconciliation
	for rows.Next() {
		var rec Reconciliation
		if err := scan(rows, &rec); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// RecentReconciliation is a lightweight reconciliation row joined to its
// operating day's business date — the variance history the inventory
// dashboard renders.
type RecentReconciliation struct {
	OperatingDayID   uuid.UUID
	BusinessDate     time.Time
	VarianceLitres   float64
	VariancePercent  float64
	TolerancePercent float64
	ClosingBook      float64
	Status           string
	SealedAt         *time.Time
}

// RecentForTank returns a tank's most recent reconciliations (newest business
// date first, up to limit) with their business dates — the first element is
// the last reconciliation, the slice is the recent variance trend.
func (r *Repo) RecentForTank(ctx context.Context, tenantID, tankID uuid.UUID, limit int) ([]RecentReconciliation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT rec.operating_day_id, od.business_date, rec.variance_litres, rec.variance_percent,
		       rec.tolerance_percent, rec.closing_book, rec.status, rec.sealed_at
		FROM tank_reconciliations rec
		JOIN operating_days od ON od.id = rec.operating_day_id AND od.tenant_id = rec.tenant_id
		WHERE rec.tenant_id = $1 AND rec.tank_id = $2
		ORDER BY od.business_date DESC, rec.created_at DESC
		LIMIT $3
	`, tenantID, tankID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecentReconciliation
	for rows.Next() {
		var rr RecentReconciliation
		if err := rows.Scan(&rr.OperatingDayID, &rr.BusinessDate, &rr.VarianceLitres, &rr.VariancePercent,
			&rr.TolerancePercent, &rr.ClosingBook, &rr.Status, &rr.SealedAt); err != nil {
			return nil, err
		}
		out = append(out, rr)
	}
	return out, rows.Err()
}

const prefixedColumns = `
    rec.id, rec.tenant_id, rec.tank_id, rec.operating_day_id, rec.opening_book, rec.deliveries_total,
    rec.sales_total, rec.adjustments_total, rec.closing_book, rec.closing_physical,
    rec.variance_litres, rec.variance_percent, rec.tolerance_percent, rec.through_seq,
    rec.status, rec.sealed_by, rec.sealed_at, rec.created_at, rec.updated_at
`

// UpsertDraft inserts or refreshes the draft reconciliation for a (tank, day)
// inside the caller's tx. A sealed row is never overwritten — that yields
// ErrSealed so the caller can refuse re-running a frozen reconciliation.
func (r *Repo) UpsertDraft(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in DraftInput) (*Reconciliation, error) {
	var rec Reconciliation
	err := scan(tx.QueryRow(ctx, `
		INSERT INTO tank_reconciliations
		    (tenant_id, tank_id, operating_day_id, opening_book, deliveries_total,
		     sales_total, adjustments_total, closing_book, closing_physical,
		     variance_litres, variance_percent, tolerance_percent, through_seq, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (tank_id, operating_day_id) DO UPDATE SET
		    opening_book      = EXCLUDED.opening_book,
		    deliveries_total  = EXCLUDED.deliveries_total,
		    sales_total       = EXCLUDED.sales_total,
		    adjustments_total = EXCLUDED.adjustments_total,
		    closing_book      = EXCLUDED.closing_book,
		    closing_physical  = EXCLUDED.closing_physical,
		    variance_litres   = EXCLUDED.variance_litres,
		    variance_percent  = EXCLUDED.variance_percent,
		    tolerance_percent = EXCLUDED.tolerance_percent,
		    through_seq       = EXCLUDED.through_seq,
		    status            = EXCLUDED.status
		WHERE tank_reconciliations.status <> 'sealed'
		RETURNING `+columns,
		tenantID, in.TankID, in.OperatingDayID, in.OpeningBook, in.DeliveriesTotal,
		in.SalesTotal, in.AdjustmentsTotal, in.ClosingBook, in.ClosingPhysical,
		in.VarianceLitres, in.VariancePercent, in.TolerancePercent, in.ThroughSeq, in.Status,
	), &rec)
	if errors.Is(err, pgx.ErrNoRows) {
		// The ON CONFLICT WHERE guard matched a sealed row, so no row was
		// updated or returned.
		return nil, ErrSealed
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// ErrSealed is returned when an operation targets an already-sealed
// reconciliation.
var ErrSealed = errors.New("reconciliation: already sealed")

// Seal freezes a reconciliation with its final figures inside the caller's tx,
// stamping sealed_by/at and the through_seq watermark. Only a non-sealed row
// seals; a sealed one yields ErrSealed.
func (r *Repo) Seal(ctx context.Context, tx pgx.Tx, tenantID, id, sealedBy uuid.UUID, in DraftInput) (*Reconciliation, error) {
	var rec Reconciliation
	err := scan(tx.QueryRow(ctx, `
		UPDATE tank_reconciliations SET
		    opening_book      = $3,
		    deliveries_total  = $4,
		    sales_total       = $5,
		    adjustments_total = $6,
		    closing_book      = $7,
		    closing_physical  = $8,
		    variance_litres   = $9,
		    variance_percent  = $10,
		    tolerance_percent = $11,
		    through_seq       = $12,
		    status            = 'sealed',
		    sealed_by         = $13,
		    sealed_at         = now()
		WHERE id = $1 AND tenant_id = $2 AND status <> 'sealed'
		RETURNING `+columns,
		id, tenantID, in.OpeningBook, in.DeliveriesTotal, in.SalesTotal, in.AdjustmentsTotal,
		in.ClosingBook, in.ClosingPhysical, in.VarianceLitres, in.VariancePercent,
		in.TolerancePercent, in.ThroughSeq, sealedBy,
	), &rec)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSealed
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}
