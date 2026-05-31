// Package payables is the data layer for accounts payable and supplier
// payments (Phase 7, Stages 7-8). Payables are created once per approved
// Phase-5 supplier invoice; supplier payments draw them down. Journal posting
// is orchestrated by the API layer (which holds the accounting repo). Money is
// carried as decimal strings; arithmetic and guards run in SQL.
package payables

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

var (
	ErrNotFound      = errors.New("payables: not found")
	ErrOverAllocated = errors.New("payables: allocation exceeds outstanding balance")
)

type Payable struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	SupplierID        uuid.UUID
	SourceInvoiceID   uuid.UUID
	InvoiceNumber     *string
	InvoiceDate       *time.Time
	DueDate           *time.Time
	Amount            string
	OutstandingAmount string
	StationID         *uuid.UUID
	Status            string
	JournalEntryID    *uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const columns = `
    id, tenant_id, supplier_id, source_invoice_id, invoice_number, invoice_date, due_date,
    amount::text, outstanding_amount::text, station_id, status, journal_entry_id, created_at, updated_at
`

func scan(row pgx.Row, p *Payable) error {
	return row.Scan(
		&p.ID, &p.TenantID, &p.SupplierID, &p.SourceInvoiceID, &p.InvoiceNumber, &p.InvoiceDate, &p.DueDate,
		&p.Amount, &p.OutstandingAmount, &p.StationID, &p.Status, &p.JournalEntryID, &p.CreatedAt, &p.UpdatedAt,
	)
}

// ImportApprovedInvoices creates a payable for every approved Phase-5 supplier
// invoice that doesn't yet have one (idempotent), and returns those created so
// the caller can post their AP journal entries.
func (r *Repo) ImportApprovedInvoices(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) ([]Payable, error) {
	rows, err := tx.Query(ctx, `
		INSERT INTO payables
		    (tenant_id, supplier_id, source_invoice_id, invoice_number, invoice_date, due_date, amount, outstanding_amount, station_id)
		SELECT si.tenant_id, si.supplier_id, si.id, si.invoice_number, si.received_at::date, si.due_date,
		       si.total_amount, si.total_amount, si.station_id
		FROM supplier_invoices si
		WHERE si.tenant_id = $1 AND si.status = 'approved'
		  AND NOT EXISTS (SELECT 1 FROM payables p WHERE p.tenant_id = si.tenant_id AND p.source_invoice_id = si.id)
		RETURNING `+columns,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

func (r *Repo) List(ctx context.Context, tenantID uuid.UUID) ([]Payable, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+columns+` FROM payables WHERE tenant_id = $1 ORDER BY due_date NULLS LAST, created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// ListPage returns a page of payables for the tenant ordered by due_date
// (NULLS LAST) then created_at (with id as a tiebreaker for stable paging),
// applying the supplied limit and offset.
func (r *Repo) ListPage(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]Payable, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+columns+` FROM payables WHERE tenant_id = $1 ORDER BY due_date NULLS LAST, created_at, id LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Payable, error) {
	var p Payable
	err := scan(r.pool.QueryRow(ctx, `SELECT `+columns+` FROM payables WHERE tenant_id = $1 AND id = $2`, tenantID, id), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SetJournalEntry links a payable to the AP journal entry that posted it.
func (r *Repo) SetJournalEntry(ctx context.Context, tx pgx.Tx, tenantID, id, entryID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE payables SET journal_entry_id = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, id, entryID)
	return err
}

// ApplyPayment reduces a payable's outstanding balance inside the caller's tx,
// updating status. An amount over the outstanding balance yields
// ErrOverAllocated.
func (r *Repo) ApplyPayment(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, amount string) (*Payable, error) {
	var p Payable
	err := scan(tx.QueryRow(ctx, `
		UPDATE payables SET
		    outstanding_amount = outstanding_amount - $3::numeric,
		    status = CASE
		        WHEN outstanding_amount - $3::numeric <= 0 THEN 'paid'
		        ELSE 'partially_paid'
		    END
		WHERE tenant_id = $1 AND id = $2 AND status <> 'voided' AND outstanding_amount >= $3::numeric
		RETURNING `+columns,
		tenantID, id, amount,
	), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOverAllocated
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// AgingBucket is a supplier's outstanding payables total.
type SupplierAging struct {
	SupplierID  uuid.UUID
	Outstanding string
	OpenCount   int
}

// Aging returns suppliers with outstanding payables, largest first.
func (r *Repo) Aging(ctx context.Context, tenantID uuid.UUID) ([]SupplierAging, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT supplier_id, SUM(outstanding_amount)::text, count(*)
		FROM payables
		WHERE tenant_id = $1 AND status <> 'paid' AND status <> 'voided'
		GROUP BY supplier_id
		HAVING SUM(outstanding_amount) > 0
		ORDER BY SUM(outstanding_amount) DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierAging{}
	for rows.Next() {
		var a SupplierAging
		if err := rows.Scan(&a.SupplierID, &a.Outstanding, &a.OpenCount); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func collect(rows pgx.Rows) ([]Payable, error) {
	defer rows.Close()
	out := []Payable{}
	for rows.Next() {
		var p Payable
		if err := scan(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
