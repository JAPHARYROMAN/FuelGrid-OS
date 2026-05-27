// Package regions is the data layer for the `regions` table.
package regions

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

type Region struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	CompanyID uuid.UUID
	Name      string
	Code      *string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateInput struct {
	CompanyID uuid.UUID
	Name      string
	Code      *string
}

type UpdateInput struct {
	Name   *string
	Code   *string
	Status *string
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const columns = `id, tenant_id, company_id, name, code, status, created_at, updated_at`

func scan(row pgx.Row, r *Region) error {
	return row.Scan(
		&r.ID, &r.TenantID, &r.CompanyID, &r.Name, &r.Code, &r.Status, &r.CreatedAt, &r.UpdatedAt,
	)
}

// List returns all non-deleted regions for the tenant. Filter by company
// when companyID is non-nil.
func (r *Repo) List(ctx context.Context, tenantID uuid.UUID, companyID *uuid.UUID) ([]Region, error) {
	if companyID != nil {
		rows, err := r.pool.Query(ctx, `
			SELECT `+columns+`
			FROM regions
			WHERE tenant_id = $1 AND company_id = $2 AND status <> 'deleted'
			ORDER BY name
		`, tenantID, *companyID)
		if err != nil {
			return nil, err
		}
		return scanList(rows)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+`
		FROM regions
		WHERE tenant_id = $1 AND status <> 'deleted'
		ORDER BY name
	`, tenantID)
	if err != nil {
		return nil, err
	}
	return scanList(rows)
}

func scanList(rows pgx.Rows) ([]Region, error) {
	defer rows.Close()
	var out []Region
	for rows.Next() {
		var x Region
		if err := scan(rows, &x); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Region, error) {
	var x Region
	if err := scan(r.pool.QueryRow(ctx, `
		SELECT `+columns+`
		FROM regions WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
	`, id, tenantID), &x); err != nil {
		return nil, err
	}
	return &x, nil
}

func (r *Repo) Create(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CreateInput) (*Region, error) {
	var x Region
	if err := scan(tx.QueryRow(ctx, `
		INSERT INTO regions (tenant_id, company_id, name, code)
		VALUES ($1, $2, $3, $4)
		RETURNING `+columns,
		tenantID, in.CompanyID, in.Name, in.Code,
	), &x); err != nil {
		return nil, err
	}
	return &x, nil
}

func (r *Repo) Update(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, in UpdateInput) (*Region, error) {
	var x Region
	err := scan(tx.QueryRow(ctx, `
		UPDATE regions
		SET name   = COALESCE($3, name),
		    code   = COALESCE($4, code),
		    status = COALESCE($5, status)
		WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
		RETURNING `+columns,
		id, tenantID, in.Name, in.Code, in.Status,
	), &x)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &x, nil
}

func (r *Repo) SoftDelete(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE regions SET status = 'deleted'
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

var ErrNotFound = errors.New("regions: not found")
