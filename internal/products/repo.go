// Package products is the data layer for the `products` table — the
// per-tenant fuel catalogue every later operational entity references.
package products

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

type Product struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	Code     string
	Name     string
	Category string
	Unit     string
	// Money/rate/density figures are exact decimal STRINGS (numeric columns
	// read ::text); arithmetic on them is done in SQL, never Go float64.
	// default_price numeric(14,2), tax_rate numeric(5,2),
	// density_kg_m3 numeric(10,3) (nullable), loss_tolerance_percent numeric(5,2).
	DefaultPrice         string
	TaxRate              string
	DensityKgM3          *string
	LossTolerancePercent string
	Color                string
	Status               string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type CreateInput struct {
	Code                 string
	Name                 string
	Category             string
	Unit                 string
	DefaultPrice         string  // numeric(14,2), bound $N::numeric
	TaxRate              string  // numeric(5,2)
	DensityKgM3          *string // numeric(10,3), nullable
	LossTolerancePercent string  // numeric(5,2)
	Color                string
}

type UpdateInput struct {
	Code                 *string
	Name                 *string
	Category             *string
	Unit                 *string
	DefaultPrice         *string // numeric(14,2)
	TaxRate              *string // numeric(5,2)
	DensityKgM3          *string // numeric(10,3), nullable
	LossTolerancePercent *string // numeric(5,2)
	Color                *string
	Status               *string
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const columns = `
    id, tenant_id, code, name, category, unit,
    default_price::text, tax_rate::text, density_kg_m3::text, loss_tolerance_percent::text,
    color, status, created_at, updated_at
`

func scan(row pgx.Row, p *Product) error {
	return row.Scan(
		&p.ID, &p.TenantID, &p.Code, &p.Name, &p.Category, &p.Unit,
		&p.DefaultPrice, &p.TaxRate, &p.DensityKgM3, &p.LossTolerancePercent,
		&p.Color, &p.Status, &p.CreatedAt, &p.UpdatedAt,
	)
}

func (r *Repo) List(ctx context.Context, tenantID uuid.UUID) ([]Product, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+`
		FROM products
		WHERE tenant_id = $1 AND status <> 'deleted'
		ORDER BY name
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Product
	for rows.Next() {
		var p Product
		if err := scan(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListPage mirrors List with limit/offset paging and a stable (name, id)
// ordering. Callers fetch limit+1 to detect a further page.
func (r *Repo) ListPage(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]Product, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+`
		FROM products
		WHERE tenant_id = $1 AND status <> 'deleted'
		ORDER BY name, id
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Product
	for rows.Next() {
		var p Product
		if err := scan(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Product, error) {
	var p Product
	if err := scan(r.pool.QueryRow(ctx, `
		SELECT `+columns+`
		FROM products WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
	`, id, tenantID), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repo) Create(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CreateInput) (*Product, error) {
	category := in.Category
	if category == "" {
		category = "fuel"
	}
	unit := in.Unit
	if unit == "" {
		unit = "litre"
	}
	color := in.Color
	if color == "" {
		color = "#64748b"
	}
	var p Product
	if err := scan(tx.QueryRow(ctx, `
		INSERT INTO products
		    (tenant_id, code, name, category, unit,
		     default_price, tax_rate, density_kg_m3, loss_tolerance_percent, color)
		VALUES ($1, $2, $3, $4, $5, $6::numeric, $7::numeric, $8::numeric, $9::numeric, $10)
		RETURNING `+columns,
		tenantID, in.Code, in.Name, category, unit,
		in.DefaultPrice, in.TaxRate, in.DensityKgM3, in.LossTolerancePercent, color,
	), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repo) Update(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, in UpdateInput) (*Product, error) {
	var p Product
	err := scan(tx.QueryRow(ctx, `
		UPDATE products
		SET code                   = COALESCE($3,  code),
		    name                   = COALESCE($4,  name),
		    category               = COALESCE($5,  category),
		    unit                   = COALESCE($6,  unit),
		    default_price          = COALESCE($7::numeric,  default_price),
		    tax_rate               = COALESCE($8::numeric,  tax_rate),
		    density_kg_m3          = COALESCE($9::numeric,  density_kg_m3),
		    loss_tolerance_percent = COALESCE($10::numeric, loss_tolerance_percent),
		    color                  = COALESCE($11, color),
		    status                 = COALESCE($12, status)
		WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
		RETURNING `+columns,
		id, tenantID,
		in.Code, in.Name, in.Category, in.Unit,
		in.DefaultPrice, in.TaxRate, in.DensityKgM3, in.LossTolerancePercent,
		in.Color, in.Status,
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
		UPDATE products SET status = 'deleted'
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

var ErrNotFound = errors.New("products: not found")
