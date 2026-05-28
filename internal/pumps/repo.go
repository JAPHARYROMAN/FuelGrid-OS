// Package pumps is the data layer for the `pumps` table — dispensing units
// that sit at a station and carry nozzles.
package pumps

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

type Pump struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	StationID        uuid.UUID
	Number           int
	Name             *string
	Manufacturer     *string
	Model            *string
	SerialNumber     *string
	Status           string
	InstallationDate *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateInput struct {
	StationID        uuid.UUID
	Number           int
	Name             *string
	Manufacturer     *string
	Model            *string
	SerialNumber     *string
	InstallationDate *time.Time
}

type UpdateInput struct {
	Number           *int
	Name             *string
	Manufacturer     *string
	Model            *string
	SerialNumber     *string
	Status           *string
	InstallationDate *time.Time
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const columns = `
    id, tenant_id, station_id, number, name, manufacturer, model,
    serial_number, status, installation_date, created_at, updated_at
`

func scan(row pgx.Row, p *Pump) error {
	return row.Scan(
		&p.ID, &p.TenantID, &p.StationID, &p.Number, &p.Name, &p.Manufacturer,
		&p.Model, &p.SerialNumber, &p.Status, &p.InstallationDate,
		&p.CreatedAt, &p.UpdatedAt,
	)
}

// List returns the tenant's pumps. When stationIDs is non-empty the result
// is restricted to those stations; nil/empty means no station filter.
func (r *Repo) List(ctx context.Context, tenantID uuid.UUID, stationIDs []uuid.UUID) ([]Pump, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+`
		FROM pumps
		WHERE tenant_id = $1
		  AND ($2::uuid[] IS NULL OR station_id = ANY($2::uuid[]))
		  AND status <> 'deleted'
		ORDER BY number
	`, tenantID, database.UUIDStrings(stationIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Pump
	for rows.Next() {
		var p Pump
		if err := scan(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Pump, error) {
	var p Pump
	if err := scan(r.pool.QueryRow(ctx, `
		SELECT `+columns+`
		FROM pumps WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
	`, id, tenantID), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repo) Create(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CreateInput) (*Pump, error) {
	var p Pump
	if err := scan(tx.QueryRow(ctx, `
		INSERT INTO pumps
		    (tenant_id, station_id, number, name, manufacturer, model,
		     serial_number, installation_date)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+columns,
		tenantID, in.StationID, in.Number, in.Name, in.Manufacturer, in.Model,
		in.SerialNumber, in.InstallationDate,
	), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repo) Update(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, in UpdateInput) (*Pump, error) {
	var p Pump
	err := scan(tx.QueryRow(ctx, `
		UPDATE pumps
		SET number            = COALESCE($3, number),
		    name              = COALESCE($4, name),
		    manufacturer      = COALESCE($5, manufacturer),
		    model             = COALESCE($6, model),
		    serial_number     = COALESCE($7, serial_number),
		    status            = COALESCE($8, status),
		    installation_date = COALESCE($9, installation_date)
		WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
		RETURNING `+columns,
		id, tenantID,
		in.Number, in.Name, in.Manufacturer, in.Model,
		in.SerialNumber, in.Status, in.InstallationDate,
	), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repo) SoftDelete(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE pumps SET status = 'deleted'
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

var ErrNotFound = errors.New("pumps: not found")
