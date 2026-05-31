// Package pricing is the data layer for the selling price book — the
// effective-dated, per-station, per-product price the forecourt charges
// (Phase 6, Stages 1-2). Prices are append-only and time-resolved: the active
// price for (station, product) is the latest row with effective_from <= now,
// so a future-dated change schedules itself.
//
// Money is carried as decimal strings (numeric in the DB), never float —
// continuing the Phase-5 discipline.
package pricing

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// ErrNotFound is returned when a price can't be resolved.
var ErrNotFound = errors.New("pricing: no price")

// PriceChange is one append-only entry in a station/product's price book.
type PriceChange struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	StationID     uuid.UUID
	ProductID     uuid.UUID
	UnitPrice     string
	EffectiveFrom time.Time
	PreviousPrice *string
	Reason        *string
	SetBy         uuid.UUID
	CreatedAt     time.Time
}

// SetPriceInput is a new price-book entry. EffectiveFrom may be in the future
// to schedule a change.
type SetPriceInput struct {
	StationID     uuid.UUID
	ProductID     uuid.UUID
	UnitPrice     string
	EffectiveFrom *time.Time
	Reason        *string
	SetBy         uuid.UUID
}

// BoardEntry is a product's current and next-scheduled price for the station
// price board.
type BoardEntry struct {
	ProductID         uuid.UUID
	ProductCode       string
	ProductName       string
	ProductColor      string
	ActivePrice       *string
	NextPrice         *string
	NextEffectiveFrom *time.Time
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const columns = `
    id, tenant_id, station_id, product_id, unit_price::text, effective_from,
    previous_price::text, reason, set_by, created_at
`

func scan(row pgx.Row, p *PriceChange) error {
	return row.Scan(
		&p.ID, &p.TenantID, &p.StationID, &p.ProductID, &p.UnitPrice, &p.EffectiveFrom,
		&p.PreviousPrice, &p.Reason, &p.SetBy, &p.CreatedAt,
	)
}

// SetPrice appends a price-book entry inside the caller's tx, snapshotting the
// price active at insert time as previous_price.
func (r *Repo) SetPrice(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in SetPriceInput) (*PriceChange, error) {
	var p PriceChange
	if err := scan(tx.QueryRow(ctx, `
		INSERT INTO price_changes
		    (tenant_id, station_id, product_id, unit_price, effective_from, previous_price, reason, set_by)
		VALUES ($1, $2, $3, $4::numeric, COALESCE($5::timestamptz, now()),
		    (SELECT unit_price FROM price_changes
		       WHERE tenant_id = $1 AND station_id = $2 AND product_id = $3 AND effective_from <= now()
		       ORDER BY effective_from DESC, created_at DESC LIMIT 1),
		    $6, $7)
		RETURNING `+columns,
		tenantID, in.StationID, in.ProductID, in.UnitPrice, in.EffectiveFrom, in.Reason, in.SetBy,
	), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ResolvePrice returns the active unit price for (station, product) at `at`.
// found is false when no price has been set effective on or before `at`.
func (r *Repo) ResolvePrice(ctx context.Context, q database.Querier, tenantID, stationID, productID uuid.UUID, at time.Time) (price string, found bool, err error) {
	err = q.QueryRow(ctx, `
		SELECT unit_price::text FROM price_changes
		WHERE tenant_id = $1 AND station_id = $2 AND product_id = $3 AND effective_from <= $4
		ORDER BY effective_from DESC, created_at DESC
		LIMIT 1
	`, tenantID, stationID, productID, at).Scan(&price)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return price, true, nil
}

// PriceBoard returns, per priced product at the station, the active price and
// the next scheduled change.
func (r *Repo) PriceBoard(ctx context.Context, tenantID, stationID uuid.UUID) ([]BoardEntry, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT pr.id, pr.code, pr.name, pr.color,
		    (SELECT pc.unit_price::text FROM price_changes pc
		       WHERE pc.tenant_id = $1 AND pc.station_id = $2 AND pc.product_id = pr.id AND pc.effective_from <= now()
		       ORDER BY pc.effective_from DESC, pc.created_at DESC LIMIT 1),
		    (SELECT pc.unit_price::text FROM price_changes pc
		       WHERE pc.tenant_id = $1 AND pc.station_id = $2 AND pc.product_id = pr.id AND pc.effective_from > now()
		       ORDER BY pc.effective_from ASC, pc.created_at ASC LIMIT 1),
		    (SELECT pc.effective_from FROM price_changes pc
		       WHERE pc.tenant_id = $1 AND pc.station_id = $2 AND pc.product_id = pr.id AND pc.effective_from > now()
		       ORDER BY pc.effective_from ASC, pc.created_at ASC LIMIT 1)
		FROM products pr
		WHERE pr.tenant_id = $1 AND pr.status <> 'deleted'
		  AND EXISTS (SELECT 1 FROM price_changes pc
		                WHERE pc.tenant_id = $1 AND pc.station_id = $2 AND pc.product_id = pr.id)
		ORDER BY pr.name
	`, tenantID, stationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BoardEntry{}
	for rows.Next() {
		var e BoardEntry
		if err := rows.Scan(&e.ProductID, &e.ProductCode, &e.ProductName, &e.ProductColor,
			&e.ActivePrice, &e.NextPrice, &e.NextEffectiveFrom); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// History returns a product's price changes at a station, newest first.
func (r *Repo) History(ctx context.Context, tenantID, stationID, productID uuid.UUID) ([]PriceChange, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+`
		FROM price_changes
		WHERE tenant_id = $1 AND station_id = $2 AND product_id = $3
		ORDER BY effective_from DESC, created_at DESC
	`, tenantID, stationID, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PriceChange{}
	for rows.Next() {
		var p PriceChange
		if err := scan(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// HistoryPage returns a page of price changes for the given station + product,
// newest first by effective_from (with id as a tiebreaker for stable paging),
// applying the supplied limit and offset.
func (r *Repo) HistoryPage(ctx context.Context, tenantID, stationID, productID uuid.UUID, limit, offset int) ([]PriceChange, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+`
		FROM price_changes
		WHERE tenant_id = $1 AND station_id = $2 AND product_id = $3
		ORDER BY effective_from DESC, created_at DESC, id
		LIMIT $4 OFFSET $5
	`, tenantID, stationID, productID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PriceChange{}
	for rows.Next() {
		var p PriceChange
		if err := scan(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
