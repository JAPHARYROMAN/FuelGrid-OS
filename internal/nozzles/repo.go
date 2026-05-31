// Package nozzles is the data layer for the `nozzles` table. A nozzle
// belongs to one pump, pulls from one tank, and dispenses that tank's
// product. The station/product consistency invariants are enforced by
// composite FKs in migration 0011; this layer derives those columns from
// the chosen tank so it never writes an inconsistent row.
package nozzles

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

type Nozzle struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	StationID uuid.UUID
	PumpID    uuid.UUID
	TankID    uuid.UUID
	ProductID uuid.UUID
	Number    int
	// DefaultPrice is an exact decimal STRING (default_price numeric(14,2) read
	// ::text); never a Go float64.
	DefaultPrice       string
	MeterDecimalPlaces int
	Status             string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type CreateInput struct {
	StationID          uuid.UUID
	PumpID             uuid.UUID
	TankID             uuid.UUID
	ProductID          uuid.UUID
	Number             int
	DefaultPrice       string // numeric(14,2), bound $N::numeric
	MeterDecimalPlaces int
}

// UpdateInput patches a nozzle. TankID/StationID/ProductID move together —
// the handler derives station and product from the chosen tank — so a
// reassignment can only ever land on a consistent triple.
type UpdateInput struct {
	TankID             *uuid.UUID
	StationID          *uuid.UUID
	ProductID          *uuid.UUID
	Number             *int
	DefaultPrice       *string // numeric(14,2)
	MeterDecimalPlaces *int
	Status             *string
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const columns = `
    id, tenant_id, station_id, pump_id, tank_id, product_id, number,
    default_price::text, meter_decimal_places, status, created_at, updated_at
`

func scan(row pgx.Row, n *Nozzle) error {
	return row.Scan(
		&n.ID, &n.TenantID, &n.StationID, &n.PumpID, &n.TankID, &n.ProductID,
		&n.Number, &n.DefaultPrice, &n.MeterDecimalPlaces, &n.Status,
		&n.CreatedAt, &n.UpdatedAt,
	)
}

// List filters by tenant and, optionally, a set of stations and/or a pump.
// When stationIDs is non-empty the result is restricted to those stations;
// nil/empty means no station filter.
func (r *Repo) List(ctx context.Context, tenantID uuid.UUID, stationIDs []uuid.UUID, pumpID *uuid.UUID) ([]Nozzle, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+`
		FROM nozzles
		WHERE tenant_id = $1
		  AND ($2::uuid[] IS NULL OR station_id = ANY($2::uuid[]))
		  AND ($3::uuid IS NULL OR pump_id = $3)
		  AND status <> 'deleted'
		ORDER BY pump_id, number
	`, tenantID, database.UUIDStrings(stationIDs), pumpID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Nozzle
	for rows.Next() {
		var n Nozzle
		if err := scan(rows, &n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Nozzle, error) {
	var n Nozzle
	if err := scan(r.pool.QueryRow(ctx, `
		SELECT `+columns+`
		FROM nozzles WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
	`, id, tenantID), &n); err != nil {
		return nil, err
	}
	return &n, nil
}

func (r *Repo) Create(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CreateInput) (*Nozzle, error) {
	var n Nozzle
	if err := scan(tx.QueryRow(ctx, `
		INSERT INTO nozzles
		    (tenant_id, station_id, pump_id, tank_id, product_id, number,
		     default_price, meter_decimal_places)
		VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8)
		RETURNING `+columns,
		tenantID, in.StationID, in.PumpID, in.TankID, in.ProductID, in.Number,
		in.DefaultPrice, in.MeterDecimalPlaces,
	), &n); err != nil {
		return nil, err
	}
	return &n, nil
}

func (r *Repo) Update(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, in UpdateInput) (*Nozzle, error) {
	var n Nozzle
	err := scan(tx.QueryRow(ctx, `
		UPDATE nozzles
		SET tank_id              = COALESCE($3, tank_id),
		    station_id           = COALESCE($4, station_id),
		    product_id           = COALESCE($5, product_id),
		    number               = COALESCE($6, number),
		    default_price        = COALESCE($7::numeric, default_price),
		    meter_decimal_places = COALESCE($8, meter_decimal_places),
		    status               = COALESCE($9, status)
		WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
		RETURNING `+columns,
		id, tenantID,
		in.TankID, in.StationID, in.ProductID, in.Number,
		in.DefaultPrice, in.MeterDecimalPlaces, in.Status,
	), &n)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// CountActiveForTank counts non-deleted nozzles drawing from a tank — used
// to block soft-deleting a tank that nozzles still reference.
func (r *Repo) CountActiveForTank(ctx context.Context, tenantID, tankID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM nozzles
		WHERE tenant_id = $1 AND tank_id = $2 AND status <> 'deleted'
	`, tenantID, tankID).Scan(&n)
	return n, err
}

func (r *Repo) SoftDelete(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE nozzles SET status = 'deleted'
		WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
	`, id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

var ErrNotFound = errors.New("nozzles: not found")
