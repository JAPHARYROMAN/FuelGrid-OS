// Package receivables is the data layer for credit customers and the
// accounts-receivable ledger (Phase 6, Stage 6). The AR ledger is append-only:
// a customer's balance is the sum of its entries (charge +, payment −). Money
// is carried as decimal strings; arithmetic and the credit-limit check run in
// SQL.
package receivables

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

var (
	ErrNotFound    = errors.New("receivables: customer not found")
	ErrCreditLimit = errors.New("receivables: charge would exceed credit limit")
)

type Customer struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	Code         string
	Name         string
	ContactName  *string
	ContactPhone *string
	ContactEmail *string
	CreditLimit  string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AREntry struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	CustomerID    uuid.UUID
	EntryType     string
	Amount        string
	BalanceAfter  string
	SourceRefType *string
	SourceRefID   *uuid.UUID
	RecordedBy    uuid.UUID
	RecordedAt    time.Time
	Notes         *string
}

type CustomerInput struct {
	Code         string
	Name         string
	ContactName  *string
	ContactPhone *string
	ContactEmail *string
	CreditLimit  string
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const customerColumns = `
    id, tenant_id, code, name, contact_name, contact_phone, contact_email,
    credit_limit::text, status, created_at, updated_at
`

func scanCustomer(row pgx.Row, c *Customer) error {
	return row.Scan(
		&c.ID, &c.TenantID, &c.Code, &c.Name, &c.ContactName, &c.ContactPhone, &c.ContactEmail,
		&c.CreditLimit, &c.Status, &c.CreatedAt, &c.UpdatedAt,
	)
}

const arColumns = `
    id, tenant_id, customer_id, entry_type, amount::text, balance_after::text,
    source_ref_type, source_ref_id, recorded_by, recorded_at, notes
`

func scanAR(row pgx.Row, e *AREntry) error {
	return row.Scan(
		&e.ID, &e.TenantID, &e.CustomerID, &e.EntryType, &e.Amount, &e.BalanceAfter,
		&e.SourceRefType, &e.SourceRefID, &e.RecordedBy, &e.RecordedAt, &e.Notes,
	)
}

func (r *Repo) ListCustomers(ctx context.Context, tenantID uuid.UUID) ([]Customer, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+customerColumns+` FROM customers
		WHERE tenant_id = $1 AND status <> 'deleted' ORDER BY name
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Customer{}
	for rows.Next() {
		var c Customer
		if err := scanCustomer(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repo) GetCustomer(ctx context.Context, tenantID, id uuid.UUID) (*Customer, error) {
	var c Customer
	err := scanCustomer(r.pool.QueryRow(ctx, `
		SELECT `+customerColumns+` FROM customers WHERE tenant_id = $1 AND id = $2 AND status <> 'deleted'
	`, tenantID, id), &c)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) CreateCustomer(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CustomerInput) (*Customer, error) {
	var c Customer
	if err := scanCustomer(tx.QueryRow(ctx, `
		INSERT INTO customers (tenant_id, code, name, contact_name, contact_phone, contact_email, credit_limit)
		VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7::numeric, 0))
		RETURNING `+customerColumns,
		tenantID, in.Code, in.Name, in.ContactName, in.ContactPhone, in.ContactEmail, nullableMoney(in.CreditLimit),
	), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) UpdateCustomer(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, in CustomerInput) (*Customer, error) {
	var c Customer
	err := scanCustomer(tx.QueryRow(ctx, `
		UPDATE customers SET
		    name          = COALESCE(NULLIF($3, ''), name),
		    contact_name  = $4,
		    contact_phone = $5,
		    contact_email = $6,
		    credit_limit  = COALESCE($7::numeric, credit_limit)
		WHERE tenant_id = $1 AND id = $2 AND status <> 'deleted'
		RETURNING `+customerColumns,
		tenantID, id, in.Name, in.ContactName, in.ContactPhone, in.ContactEmail, nullableMoney(in.CreditLimit),
	), &c)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// PostCharge appends an AR charge (increasing the balance) inside the caller's
// tx. Unless allowOverLimit is set, a charge that would push the balance over
// the credit limit is refused with ErrCreditLimit.
func (r *Repo) PostCharge(ctx context.Context, tx pgx.Tx, tenantID, customerID uuid.UUID, amount string, srcType *string, srcID *uuid.UUID, recordedBy uuid.UUID, notes *string, allowOverLimit bool) (*AREntry, error) {
	var e AREntry
	err := scanAR(tx.QueryRow(ctx, `
		INSERT INTO ar_entries
		    (tenant_id, customer_id, entry_type, amount, balance_after, source_ref_type, source_ref_id, recorded_by, notes)
		SELECT $1, $2, 'charge', $3::numeric,
		    (SELECT COALESCE(SUM(amount), 0) FROM ar_entries WHERE tenant_id = $1 AND customer_id = $2) + $3::numeric,
		    $4, $5, $6, $7
		WHERE $8 OR (
		    (SELECT COALESCE(SUM(amount), 0) FROM ar_entries WHERE tenant_id = $1 AND customer_id = $2) + $3::numeric
		    <= (SELECT credit_limit FROM customers WHERE tenant_id = $1 AND id = $2)
		)
		RETURNING `+arColumns,
		tenantID, customerID, amount, srcType, srcID, recordedBy, notes, allowOverLimit,
	), &e)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCreditLimit
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// PostPayment appends an AR payment (reducing the balance) inside the tx.
func (r *Repo) PostPayment(ctx context.Context, tx pgx.Tx, tenantID, customerID uuid.UUID, amount string, srcType *string, srcID *uuid.UUID, recordedBy uuid.UUID, notes *string) (*AREntry, error) {
	var e AREntry
	if err := scanAR(tx.QueryRow(ctx, `
		INSERT INTO ar_entries
		    (tenant_id, customer_id, entry_type, amount, balance_after, source_ref_type, source_ref_id, recorded_by, notes)
		VALUES ($1, $2, 'payment', -$3::numeric,
		    (SELECT COALESCE(SUM(amount), 0) FROM ar_entries WHERE tenant_id = $1 AND customer_id = $2) - $3::numeric,
		    $4, $5, $6, $7)
		RETURNING `+arColumns,
		tenantID, customerID, amount, srcType, srcID, recordedBy, notes,
	), &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// Balance returns a customer's current AR balance (what they owe).
func (r *Repo) Balance(ctx context.Context, tenantID, customerID uuid.UUID) (string, error) {
	var bal string
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0)::text FROM ar_entries WHERE tenant_id = $1 AND customer_id = $2
	`, tenantID, customerID).Scan(&bal)
	return bal, err
}

// Statement returns a customer's AR ledger, newest first.
func (r *Repo) Statement(ctx context.Context, tenantID, customerID uuid.UUID) ([]AREntry, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+arColumns+` FROM ar_entries
		WHERE tenant_id = $1 AND customer_id = $2 ORDER BY recorded_at DESC, id
	`, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AREntry{}
	for rows.Next() {
		var e AREntry
		if err := scanAR(rows, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func nullableMoney(s string) any {
	if s == "" {
		return nil
	}
	return s
}
