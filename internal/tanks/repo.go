// Package tanks is the data layer for the `tanks` table — physical tank
// inventory bound to a station and a product.
package tanks

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

type Tank struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	StationID uuid.UUID
	ProductID uuid.UUID
	Name      string
	Code      string
	// Litre limits are exact decimal STRINGS (numeric(14,3) read ::text);
	// arithmetic is done in SQL, never Go float64.
	CapacityLitres   string
	SafeMinLitres    string
	SafeMaxLitres    string
	DeadStockLitres  string
	HasWaterSensor   bool
	HasTempSensor    bool
	Status           string
	InstallationDate *time.Time
	DecommissionDate *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateInput struct {
	StationID        uuid.UUID
	ProductID        uuid.UUID
	Name             string
	Code             string
	CapacityLitres   string // numeric(14,3), bound $N::numeric
	SafeMinLitres    string // numeric(14,3)
	SafeMaxLitres    string // numeric(14,3)
	DeadStockLitres  string // numeric(14,3)
	HasWaterSensor   bool
	HasTempSensor    bool
	InstallationDate *time.Time
}

type UpdateInput struct {
	ProductID        *uuid.UUID
	Name             *string
	Code             *string
	CapacityLitres   *string // numeric(14,3)
	SafeMinLitres    *string // numeric(14,3)
	SafeMaxLitres    *string // numeric(14,3)
	DeadStockLitres  *string // numeric(14,3)
	HasWaterSensor   *bool
	HasTempSensor    *bool
	Status           *string
	InstallationDate *time.Time
	DecommissionDate *time.Time
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const columns = `
    id, tenant_id, station_id, product_id, name, code,
    capacity_litres::text, safe_min_litres::text, safe_max_litres::text, dead_stock_litres::text,
    has_water_sensor, has_temp_sensor, status,
    installation_date, decommission_date, created_at, updated_at
`

func scan(row pgx.Row, t *Tank) error {
	return row.Scan(
		&t.ID, &t.TenantID, &t.StationID, &t.ProductID, &t.Name, &t.Code,
		&t.CapacityLitres, &t.SafeMinLitres, &t.SafeMaxLitres, &t.DeadStockLitres,
		&t.HasWaterSensor, &t.HasTempSensor, &t.Status,
		&t.InstallationDate, &t.DecommissionDate, &t.CreatedAt, &t.UpdatedAt,
	)
}

// List returns the tenant's tanks. When stationIDs is non-empty the result
// is restricted to those stations; nil/empty means no station filter.
func (r *Repo) List(ctx context.Context, tenantID uuid.UUID, stationIDs []uuid.UUID) ([]Tank, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+`
		FROM tanks
		WHERE tenant_id = $1
		  AND ($2::uuid[] IS NULL OR station_id = ANY($2::uuid[]))
		  AND status <> 'deleted'
		ORDER BY code
	`, tenantID, database.UUIDStrings(stationIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Tank
	for rows.Next() {
		var t Tank
		if err := scan(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Tank, error) {
	var t Tank
	if err := scan(r.pool.QueryRow(ctx, `
		SELECT `+columns+`
		FROM tanks WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
	`, id, tenantID), &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repo) Create(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CreateInput) (*Tank, error) {
	var t Tank
	if err := scan(tx.QueryRow(ctx, `
		INSERT INTO tanks
		    (tenant_id, station_id, product_id, name, code,
		     capacity_litres, safe_min_litres, safe_max_litres, dead_stock_litres,
		     has_water_sensor, has_temp_sensor, installation_date)
		VALUES ($1, $2, $3, $4, $5, $6::numeric, $7::numeric, $8::numeric, $9::numeric, $10, $11, $12)
		RETURNING `+columns,
		tenantID, in.StationID, in.ProductID, in.Name, in.Code,
		in.CapacityLitres, in.SafeMinLitres, in.SafeMaxLitres, in.DeadStockLitres,
		in.HasWaterSensor, in.HasTempSensor, in.InstallationDate,
	), &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repo) Update(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, in UpdateInput) (*Tank, error) {
	var t Tank
	err := scan(tx.QueryRow(ctx, `
		UPDATE tanks
		SET product_id        = COALESCE($3,  product_id),
		    name              = COALESCE($4,  name),
		    code              = COALESCE($5,  code),
		    capacity_litres   = COALESCE($6::numeric,  capacity_litres),
		    safe_min_litres   = COALESCE($7::numeric,  safe_min_litres),
		    safe_max_litres   = COALESCE($8::numeric,  safe_max_litres),
		    dead_stock_litres = COALESCE($9::numeric,  dead_stock_litres),
		    has_water_sensor  = COALESCE($10, has_water_sensor),
		    has_temp_sensor   = COALESCE($11, has_temp_sensor),
		    status            = COALESCE($12, status),
		    installation_date = COALESCE($13, installation_date),
		    decommission_date = COALESCE($14, decommission_date)
		WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
		RETURNING `+columns,
		id, tenantID,
		in.ProductID, in.Name, in.Code,
		in.CapacityLitres, in.SafeMinLitres, in.SafeMaxLitres, in.DeadStockLitres,
		in.HasWaterSensor, in.HasTempSensor, in.Status,
		in.InstallationDate, in.DecommissionDate,
	), &t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// CountActiveForProduct counts non-deleted tanks bound to a product — used
// to block soft-deleting a product that tanks still reference.
func (r *Repo) CountActiveForProduct(ctx context.Context, tenantID, productID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM tanks
		WHERE tenant_id = $1 AND product_id = $2 AND status <> 'deleted'
	`, tenantID, productID).Scan(&n)
	return n, err
}

func (r *Repo) SoftDelete(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE tanks SET status = 'deleted'
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

var ErrNotFound = errors.New("tanks: not found")
