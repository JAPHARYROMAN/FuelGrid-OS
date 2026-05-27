// Package companies is the data layer for the `companies` table.
package companies

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// Company is the row shape consumed by handlers and the SDK.
type Company struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	Name           string
	LegalName      *string
	RegistrationNo *string
	TaxID          *string
	Currency       string
	Timezone       string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreateInput captures the writable fields for POST.
type CreateInput struct {
	Name           string
	LegalName      *string
	RegistrationNo *string
	TaxID          *string
	Currency       string // empty -> default "USD"
	Timezone       string // empty -> default "UTC"
}

// UpdateInput captures PATCH semantics: nil pointer means "leave unchanged".
type UpdateInput struct {
	Name           *string
	LegalName      *string
	RegistrationNo *string
	TaxID          *string
	Currency       *string
	Timezone       *string
	Status         *string
}

// Repo is the Postgres-backed companies repository.
type Repo struct {
	pool *database.Pool
}

// New wires a Repo against the supplied pool.
func New(pool *database.Pool) *Repo {
	return &Repo{pool: pool}
}

const columns = `
    id, tenant_id, name, legal_name, registration_no, tax_id,
    currency, timezone, status, created_at, updated_at
`

func scan(row pgx.Row, c *Company) error {
	return row.Scan(
		&c.ID, &c.TenantID, &c.Name, &c.LegalName, &c.RegistrationNo, &c.TaxID,
		&c.Currency, &c.Timezone, &c.Status, &c.CreatedAt, &c.UpdatedAt,
	)
}

// List returns active + suspended companies for a tenant, newest first.
func (r *Repo) List(ctx context.Context, tenantID uuid.UUID) ([]Company, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+`
		FROM companies
		WHERE tenant_id = $1 AND status <> 'deleted'
		ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Company
	for rows.Next() {
		var c Company
		if err := scan(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Get returns a single non-deleted company.
func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Company, error) {
	var c Company
	if err := scan(r.pool.QueryRow(ctx, `
		SELECT `+columns+`
		FROM companies
		WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
	`, id, tenantID), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Create inserts a company within a transaction. Caller composes the tx
// with the matching audit + outbox writes.
func (r *Repo) Create(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CreateInput) (*Company, error) {
	currency := in.Currency
	if currency == "" {
		currency = "USD"
	}
	tz := in.Timezone
	if tz == "" {
		tz = "UTC"
	}

	var c Company
	if err := scan(tx.QueryRow(ctx, `
		INSERT INTO companies (tenant_id, name, legal_name, registration_no, tax_id, currency, timezone)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+columns,
		tenantID, in.Name, in.LegalName, in.RegistrationNo, in.TaxID, currency, tz,
	), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Update applies a PATCH. Returns ErrNotFound when the row is missing
// (or already deleted).
func (r *Repo) Update(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, in UpdateInput) (*Company, error) {
	var c Company
	err := scan(tx.QueryRow(ctx, `
		UPDATE companies
		SET name             = COALESCE($3, name),
		    legal_name       = COALESCE($4, legal_name),
		    registration_no  = COALESCE($5, registration_no),
		    tax_id           = COALESCE($6, tax_id),
		    currency         = COALESCE($7, currency),
		    timezone         = COALESCE($8, timezone),
		    status           = COALESCE($9, status)
		WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
		RETURNING `+columns,
		id, tenantID,
		in.Name, in.LegalName, in.RegistrationNo, in.TaxID,
		in.Currency, in.Timezone, in.Status,
	), &c)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// SoftDelete sets status='deleted'.
func (r *Repo) SoftDelete(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE companies SET status = 'deleted'
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

// ErrNotFound is the sentinel handlers translate to 404.
var ErrNotFound = errors.New("companies: not found")
