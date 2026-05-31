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
//
// Per the house money/litre rule, every litre and percent figure is carried as
// a decimal STRING (the DB column is numeric); arithmetic on them happens in
// SQL, never in Go float64, so float residue can never corrupt the seal
// write-off that carries forward as the next day's opening balance.
type Reconciliation struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	TankID           uuid.UUID
	OperatingDayID   uuid.UUID
	OpeningBook      string
	DeliveriesTotal  string
	SalesTotal       string
	AdjustmentsTotal string
	ClosingBook      string
	ClosingPhysical  string
	VarianceLitres   string
	VariancePercent  string
	TolerancePercent string
	ThroughSeq       int64
	Status           string
	SealedBy         *uuid.UUID
	SealedAt         *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// DraftInput carries the computed figures for an upsert. All litre/percent
// figures are decimal strings, bound into the numeric columns as ::numeric.
type DraftInput struct {
	TankID           uuid.UUID
	OperatingDayID   uuid.UUID
	OpeningBook      string
	DeliveriesTotal  string
	SalesTotal       string
	AdjustmentsTotal string
	ClosingBook      string
	ClosingPhysical  string
	VarianceLitres   string
	VariancePercent  string
	TolerancePercent string
	ThroughSeq       int64
	Status           string // draft | exception
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

// columns reads every numeric litre/percent figure as ::text so it round-trips
// exactly into the Go decimal strings — no float64 ever materializes.
const columns = `
    id, tenant_id, tank_id, operating_day_id,
    opening_book::text, deliveries_total::text,
    sales_total::text, adjustments_total::text, closing_book::text, closing_physical::text,
    variance_litres::text, variance_percent::text, tolerance_percent::text, through_seq,
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

// Computed is the live book-vs-physical result for a (tank, day), computed
// entirely in SQL numeric and carried as decimal strings. WriteOff is the
// residual the seal must post so the ledger lands exactly on ClosingPhysical
// (= physical − book); CarriedForwardOpening is the next period's opening book
// (= ClosingPhysical). WithinTolerance is an exact numeric comparison
// (abs(variance) <= tolerance_litres), never a float epsilon.
type Computed struct {
	OpeningBook      string
	DeliveriesTotal  string
	SalesTotal       string
	AdjustmentsTotal string
	ClosingBook      string
	ClosingPhysical  string
	VarianceLitres   string
	VariancePercent  string
	TolerancePercent string
	ToleranceLitres  string
	WithinTolerance  bool
	WriteOff         string // physical − book; the seal write-off litres
	WriteOffNonZero  bool   // exact numeric test: write-off <> 0
	ClosingForward   string // == ClosingPhysical; opening book for the next day
	ThroughSeq       int64
}

// ComputeInput names the exact decimal inputs to a live reconciliation compute.
// openingBook and closingPhysical are decimal strings sourced directly from the
// numeric DB columns (the prior sealed recon's closing_physical or the genesis
// opening movement; the closing dip's volume_litres) — never float64.
type ComputeInput struct {
	TankID           uuid.UUID
	OpeningBook      string // decimal string
	ClosingPhysical  string // decimal string
	TolerancePercent string // decimal string
	FromSeq          int64
}

// Compute sums the period ledger and derives every reconciliation figure in one
// SQL statement, in numeric, returning decimal strings. The within-tolerance
// decision and the write-off non-zero test are exact numeric comparisons. invQ
// reads the ledger — pass a tx to see movements posted earlier in the same
// transaction.
//
// variance_litres is closing_book − closing_physical (preserving the existing
// sign convention the API and tests rely on); the seal write-off is the
// negation, physical − book.
func (r *Repo) Compute(ctx context.Context, invQ database.Querier, tenantID uuid.UUID, in ComputeInput) (*Computed, error) {
	var c Computed
	err := invQ.QueryRow(ctx, `
		WITH period AS (
		    SELECT
		        COALESCE(SUM(litres) FILTER (WHERE movement_type = 'opening'), 0)   AS opening_total,
		        COALESCE(SUM(litres) FILTER (WHERE movement_type = 'delivery'), 0)  AS deliveries_total,
		        COALESCE(-SUM(litres) FILTER (WHERE movement_type = 'sales'), 0)    AS sales_total,
		        COALESCE(SUM(litres) FILTER (WHERE movement_type = 'adjustment'), 0) AS adjustments_total,
		        COALESCE(MAX(seq), $5)                                              AS through_seq
		    FROM stock_movements
		    WHERE tenant_id = $1 AND tank_id = $2 AND seq > $5
		),
		fig AS (
		    SELECT
		        $3::numeric                                                                   AS opening_book,
		        deliveries_total,
		        sales_total,
		        adjustments_total,
		        ($3::numeric + opening_total + deliveries_total - sales_total + adjustments_total) AS closing_book,
		        $4::numeric                                                                   AS closing_physical,
		        $6::numeric                                                                   AS tolerance_percent,
		        through_seq
		    FROM period
		)
		SELECT
		    opening_book::text,
		    deliveries_total::text,
		    sales_total::text,
		    adjustments_total::text,
		    closing_book::text,
		    closing_physical::text,
		    (closing_book - closing_physical)::text                          AS variance_litres,
		    CASE WHEN closing_book = 0 THEN 0
		         ELSE (closing_book - closing_physical) / closing_book * 100
		    END::text                                                        AS variance_percent,
		    tolerance_percent::text,
		    (abs(closing_book) * tolerance_percent / 100)::text              AS tolerance_litres,
		    (abs(closing_book - closing_physical) <= abs(closing_book) * tolerance_percent / 100) AS within_tolerance,
		    (closing_physical - closing_book)::text                          AS write_off,
		    (closing_physical - closing_book <> 0)                           AS write_off_nonzero,
		    closing_physical::text                                           AS closing_forward,
		    through_seq
		FROM fig
	`, tenantID, in.TankID, in.OpeningBook, in.ClosingPhysical, in.FromSeq, in.TolerancePercent).Scan(
		&c.OpeningBook, &c.DeliveriesTotal, &c.SalesTotal, &c.AdjustmentsTotal,
		&c.ClosingBook, &c.ClosingPhysical, &c.VarianceLitres, &c.VariancePercent,
		&c.TolerancePercent, &c.ToleranceLitres, &c.WithinTolerance,
		&c.WriteOff, &c.WriteOffNonZero, &c.ClosingForward, &c.ThroughSeq,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// WriteOffMovement is the seal write-off ledger row, with its litres and
// balance_after as exact decimal strings (read back as ::text in the same
// statement so no float64 ever touches them).
type WriteOffMovement struct {
	ID           uuid.UUID
	TankID       uuid.UUID
	SourceRefID  uuid.UUID
	Litres       string
	BalanceAfter string
	Notes        string
	RecordedBy   uuid.UUID
	RecordedAt   time.Time
}

// PostWriteOff appends the seal's variance write-off adjustment to a tank's
// ledger inside the caller's tx, binding the litres as a decimal string into
// the numeric column so no float64 ever touches the figure. It mirrors the
// inventory ledger's balance_after computation and returns the row (litres /
// balance_after as ::text) so the caller can audit the exact decimals.
func (r *Repo) PostWriteOff(ctx context.Context, tx pgx.Tx, tenantID, tankID, reconID, recordedBy uuid.UUID, litres, notes string) (*WriteOffMovement, error) {
	// Serialize against concurrent posts to this tank (INV-003) — same
	// transaction-scoped advisory lock key (the tank id) inventory.PostMovement
	// uses, so balance_after stays consistent regardless of which path writes.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, tankID); err != nil {
		return nil, err
	}
	var m WriteOffMovement
	err := tx.QueryRow(ctx, `
		INSERT INTO stock_movements
		    (tenant_id, tank_id, movement_type, source_ref_type, source_ref_id,
		     litres, balance_after, recorded_by, notes)
		VALUES ($1, $2, 'adjustment', 'reconciliation', $3,
		    $4::numeric,
		    (SELECT COALESCE(SUM(litres), 0) FROM stock_movements
		        WHERE tenant_id = $1 AND tank_id = $2) + $4::numeric,
		    $5, $6)
		RETURNING id, tank_id, source_ref_id, litres::text, balance_after::text, notes, recorded_by, recorded_at
	`, tenantID, tankID, reconID, litres, recordedBy, notes).Scan(
		&m.ID, &m.TankID, &m.SourceRefID, &m.Litres, &m.BalanceAfter, &m.Notes, &m.RecordedBy, &m.RecordedAt,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// GenesisOpeningLitres returns the tank's genesis opening-movement litres as a
// decimal string, plus its seq — the balance-forward anchor when the tank has
// never been reconciled. Mirrors inventory.OpeningBalance's selection (a posted
// 'opening' that is not a reversal contra), but returns the exact numeric as
// text so no float64 enters the reconciliation. Returns ok=false (and "0", 0)
// when the tank has no opening yet.
func (r *Repo) GenesisOpeningLitres(ctx context.Context, tenantID, tankID uuid.UUID) (litres string, seq int64, ok bool, err error) {
	scanErr := r.pool.QueryRow(ctx, `
		SELECT litres::text, seq FROM stock_movements
		WHERE tenant_id = $1 AND tank_id = $2 AND movement_type = 'opening' AND status = 'posted'
		  AND (source_ref_type IS NULL OR source_ref_type <> 'correction')
		ORDER BY seq DESC LIMIT 1
	`, tenantID, tankID).Scan(&litres, &seq)
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return "0", 0, false, nil
	}
	if scanErr != nil {
		return "", 0, false, scanErr
	}
	return litres, seq, true, nil
}

// ClosingDipVolumeText returns the tank's most recent active closing-dip volume
// for an operating day as a decimal string — the physical figure, read as exact
// numeric text rather than float64. Returns ok=false when there is no closing
// dip that day.
func (r *Repo) ClosingDipVolumeText(ctx context.Context, q database.Querier, tenantID, tankID, operatingDayID uuid.UUID) (volume string, ok bool, err error) {
	scanErr := q.QueryRow(ctx, `
		SELECT d.volume_litres::text
		FROM tank_dip_readings d
		JOIN shifts sh ON sh.id = d.shift_id AND sh.tenant_id = d.tenant_id
		WHERE d.tenant_id = $1 AND d.tank_id = $2 AND d.reading_type = 'closing'
		  AND d.status = 'active' AND sh.operating_day_id = $3
		ORDER BY d.recorded_at DESC, d.created_at DESC
		LIMIT 1
	`, tenantID, tankID, operatingDayID).Scan(&volume)
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return "", false, nil
	}
	if scanErr != nil {
		return "", false, scanErr
	}
	return volume, true, nil
}

// ProductTolerancePercentText returns a product's loss-tolerance percent as a
// decimal string (exact numeric text), for the tank's product.
func (r *Repo) ProductTolerancePercentText(ctx context.Context, tenantID, productID uuid.UUID) (string, error) {
	var tol string
	err := r.pool.QueryRow(ctx, `
		SELECT loss_tolerance_percent::text FROM products
		WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
	`, productID, tenantID).Scan(&tol)
	return tol, err
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

// ListForStationDayPage returns a page of the day's reconciliations for the
// station's tanks (joined through tanks) in tank-code order, with rec.id as a
// deterministic tiebreaker so paging is stable.
func (r *Repo) ListForStationDayPage(ctx context.Context, tenantID, stationID, dayID uuid.UUID, limit, offset int) ([]Reconciliation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+prefixedColumns+`
		FROM tank_reconciliations rec
		JOIN tanks t ON t.id = rec.tank_id AND t.tenant_id = rec.tenant_id
		WHERE rec.tenant_id = $1 AND t.station_id = $2 AND rec.operating_day_id = $3
		ORDER BY t.code, rec.id
		LIMIT $4 OFFSET $5
	`, tenantID, stationID, dayID, limit, offset)
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
	VarianceLitres   string
	VariancePercent  string
	TolerancePercent string
	ClosingBook      string
	Status           string
	SealedAt         *time.Time
}

// RecentForTank returns a tank's most recent reconciliations (newest business
// date first, up to limit) with their business dates — the first element is
// the last reconciliation, the slice is the recent variance trend.
func (r *Repo) RecentForTank(ctx context.Context, tenantID, tankID uuid.UUID, limit int) ([]RecentReconciliation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT rec.operating_day_id, od.business_date,
		       rec.variance_litres::text, rec.variance_percent::text,
		       rec.tolerance_percent::text, rec.closing_book::text, rec.status, rec.sealed_at
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
    rec.id, rec.tenant_id, rec.tank_id, rec.operating_day_id,
    rec.opening_book::text, rec.deliveries_total::text,
    rec.sales_total::text, rec.adjustments_total::text, rec.closing_book::text, rec.closing_physical::text,
    rec.variance_litres::text, rec.variance_percent::text, rec.tolerance_percent::text, rec.through_seq,
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
		VALUES ($1, $2, $3, $4::numeric, $5::numeric, $6::numeric, $7::numeric, $8::numeric,
		    $9::numeric, $10::numeric, $11::numeric, $12::numeric, $13, $14)
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
		    opening_book      = $3::numeric,
		    deliveries_total  = $4::numeric,
		    sales_total       = $5::numeric,
		    adjustments_total = $6::numeric,
		    closing_book      = $7::numeric,
		    closing_physical  = $8::numeric,
		    variance_litres   = $9::numeric,
		    variance_percent  = $10::numeric,
		    tolerance_percent = $11::numeric,
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
