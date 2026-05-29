// Package stations is the data layer for the `stations` table.
package stations

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

type Station struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	CompanyID    uuid.UUID
	RegionID     *uuid.UUID
	Name         string
	Code         string
	AddressLine1 *string
	AddressLine2 *string
	City         *string
	State        *string
	Country      *string
	Latitude     *float64
	Longitude    *float64
	Timezone     string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CreateInput struct {
	CompanyID    uuid.UUID
	RegionID     *uuid.UUID
	Name         string
	Code         string
	AddressLine1 *string
	AddressLine2 *string
	City         *string
	State        *string
	Country      *string
	Latitude     *float64
	Longitude    *float64
	Timezone     string
}

type UpdateInput struct {
	RegionID     *uuid.UUID // pointer to pointer would be ideal for "unset", but PATCH is rare-case
	Name         *string
	Code         *string
	AddressLine1 *string
	AddressLine2 *string
	City         *string
	State        *string
	Country      *string
	Latitude     *float64
	Longitude    *float64
	Timezone     *string
	Status       *string
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const columns = `
    id, tenant_id, company_id, region_id, name, code,
    address_line1, address_line2, city, state, country,
    latitude, longitude, timezone, status, created_at, updated_at
`

func scan(row pgx.Row, s *Station) error {
	return row.Scan(
		&s.ID, &s.TenantID, &s.CompanyID, &s.RegionID, &s.Name, &s.Code,
		&s.AddressLine1, &s.AddressLine2, &s.City, &s.State, &s.Country,
		&s.Latitude, &s.Longitude, &s.Timezone, &s.Status,
		&s.CreatedAt, &s.UpdatedAt,
	)
}

// List returns the tenant's stations, optionally filtered by region and
// restricted to a set of station ids. A nil stationIDs slice means "no
// station-scope restriction" (caller is tenant-wide); a non-nil slice limits
// the result to those ids — this is the station-read scope enforced for
// station-restricted actors (ORG-01). An empty (non-nil) slice yields no rows.
func (r *Repo) List(ctx context.Context, tenantID uuid.UUID, regionID *uuid.UUID, stationIDs []uuid.UUID) ([]Station, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+`
		FROM stations
		WHERE tenant_id = $1
		  AND ($2::uuid IS NULL OR region_id = $2::uuid)
		  AND ($3::uuid[] IS NULL OR id = ANY($3::uuid[]))
		  AND status <> 'deleted'
		ORDER BY name
	`, tenantID, regionID, database.UUIDStrings(stationIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Station
	for rows.Next() {
		var s Station
		if err := scan(rows, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Station, error) {
	var s Station
	if err := scan(r.pool.QueryRow(ctx, `
		SELECT `+columns+`
		FROM stations WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
	`, id, tenantID), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repo) Create(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CreateInput) (*Station, error) {
	tz := in.Timezone
	if tz == "" {
		tz = "UTC"
	}
	var s Station
	if err := scan(tx.QueryRow(ctx, `
		INSERT INTO stations
		    (tenant_id, company_id, region_id, name, code,
		     address_line1, address_line2, city, state, country,
		     latitude, longitude, timezone)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING `+columns,
		tenantID, in.CompanyID, in.RegionID, in.Name, in.Code,
		in.AddressLine1, in.AddressLine2, in.City, in.State, in.Country,
		in.Latitude, in.Longitude, tz,
	), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repo) Update(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, in UpdateInput) (*Station, error) {
	var s Station
	err := scan(tx.QueryRow(ctx, `
		UPDATE stations
		SET region_id      = COALESCE($3,  region_id),
		    name           = COALESCE($4,  name),
		    code           = COALESCE($5,  code),
		    address_line1  = COALESCE($6,  address_line1),
		    address_line2  = COALESCE($7,  address_line2),
		    city           = COALESCE($8,  city),
		    state          = COALESCE($9,  state),
		    country        = COALESCE($10, country),
		    latitude       = COALESCE($11, latitude),
		    longitude      = COALESCE($12, longitude),
		    timezone       = COALESCE($13, timezone),
		    status         = COALESCE($14, status)
		WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
		RETURNING `+columns,
		id, tenantID,
		in.RegionID, in.Name, in.Code,
		in.AddressLine1, in.AddressLine2, in.City, in.State, in.Country,
		in.Latitude, in.Longitude, in.Timezone, in.Status,
	), &s)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repo) SoftDelete(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE stations SET status = 'deleted'
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

var ErrNotFound = errors.New("stations: not found")
