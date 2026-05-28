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
	// ErrInvoiceState is returned when an invoice transition is not allowed
	// from its current status (e.g. issuing one that is not a draft).
	ErrInvoiceState = errors.New("receivables: invalid invoice state")
	// ErrOverAllocated is returned when a payment allocation exceeds an
	// invoice's outstanding balance.
	ErrOverAllocated = errors.New("receivables: allocation exceeds outstanding balance")
)

type CustomerInvoice struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	CustomerID        uuid.UUID
	InvoiceNumber     *string
	InvoiceDate       time.Time
	DueDate           *time.Time
	Amount            string
	OutstandingAmount string
	SourceType        string
	SourceID          *uuid.UUID
	StationID         *uuid.UUID
	Status            string
	JournalEntryID    *uuid.UUID
	CreatedBy         uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type InvoiceInput struct {
	CustomerID    uuid.UUID
	InvoiceNumber *string
	InvoiceDate   time.Time
	DueDate       *time.Time
	SourceType    string
	SourceID      *uuid.UUID
	StationID     *uuid.UUID
	CreatedBy     uuid.UUID
}

// RevenueGroup is a credit line for posting: a revenue account key and the
// total amount billed to it across the invoice's lines.
type RevenueGroup struct {
	AccountKey string
	Amount     string
}

const invoiceColumns = `
    id, tenant_id, customer_id, invoice_number, invoice_date, due_date, amount::text,
    outstanding_amount::text, source_type, source_id, station_id, status, journal_entry_id,
    created_by, created_at, updated_at
`

func scanInvoice(row pgx.Row, i *CustomerInvoice) error {
	return row.Scan(
		&i.ID, &i.TenantID, &i.CustomerID, &i.InvoiceNumber, &i.InvoiceDate, &i.DueDate, &i.Amount,
		&i.OutstandingAmount, &i.SourceType, &i.SourceID, &i.StationID, &i.Status, &i.JournalEntryID,
		&i.CreatedBy, &i.CreatedAt, &i.UpdatedAt,
	)
}

// CreateInvoice inserts a draft invoice header (amount set later from lines).
func (r *Repo) CreateInvoice(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in InvoiceInput) (*CustomerInvoice, error) {
	src := in.SourceType
	if src == "" {
		src = "manual"
	}
	var i CustomerInvoice
	if err := scanInvoice(tx.QueryRow(ctx, `
		INSERT INTO customer_invoices
		    (tenant_id, customer_id, invoice_number, invoice_date, due_date, amount, outstanding_amount, source_type, source_id, station_id, created_by)
		VALUES ($1, $2, $3, $4, $5, 0, 0, $6, $7, $8, $9)
		RETURNING `+invoiceColumns,
		tenantID, in.CustomerID, in.InvoiceNumber, in.InvoiceDate, in.DueDate, src, in.SourceID, in.StationID, in.CreatedBy,
	), &i); err != nil {
		return nil, err
	}
	return &i, nil
}

// AddInvoiceLine appends a billed line to a draft invoice.
func (r *Repo) AddInvoiceLine(ctx context.Context, tx pgx.Tx, tenantID, invoiceID uuid.UUID, description *string, amount, revenueAccountKey string) error {
	key := revenueAccountKey
	if key == "" {
		key = "sales_revenue"
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO customer_invoice_lines (tenant_id, customer_invoice_id, description, amount, revenue_account_key)
		VALUES ($1, $2, $3, $4::numeric, $5)
	`, tenantID, invoiceID, description, amount, key)
	return err
}

// FinalizeInvoiceAmount sets the invoice amount (and outstanding) to the sum of
// its lines and returns the total as a decimal string.
func (r *Repo) FinalizeInvoiceAmount(ctx context.Context, tx pgx.Tx, tenantID, invoiceID uuid.UUID) (string, error) {
	var total string
	err := tx.QueryRow(ctx, `
		UPDATE customer_invoices
		SET amount = COALESCE((SELECT SUM(amount) FROM customer_invoice_lines WHERE tenant_id = $1 AND customer_invoice_id = $2), 0),
		    outstanding_amount = COALESCE((SELECT SUM(amount) FROM customer_invoice_lines WHERE tenant_id = $1 AND customer_invoice_id = $2), 0)
		WHERE tenant_id = $1 AND id = $2
		RETURNING amount::text
	`, tenantID, invoiceID).Scan(&total)
	return total, err
}

// IssueInvoice moves a draft invoice to issued, ready for AR posting.
func (r *Repo) IssueInvoice(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (*CustomerInvoice, error) {
	var i CustomerInvoice
	err := scanInvoice(tx.QueryRow(ctx, `
		UPDATE customer_invoices SET status = 'issued'
		WHERE tenant_id = $1 AND id = $2 AND status = 'draft'
		RETURNING `+invoiceColumns,
		tenantID, id,
	), &i)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvoiceState
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}

// RevenueBreakdown returns the per-revenue-account credit lines for posting an
// issued invoice (debit AR for the total, credit each revenue group).
func (r *Repo) RevenueBreakdown(ctx context.Context, q database.Querier, tenantID, invoiceID uuid.UUID) ([]RevenueGroup, error) {
	rows, err := q.Query(ctx, `
		SELECT revenue_account_key, SUM(amount)::text
		FROM customer_invoice_lines
		WHERE tenant_id = $1 AND customer_invoice_id = $2
		GROUP BY revenue_account_key
		ORDER BY revenue_account_key
	`, tenantID, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RevenueGroup{}
	for rows.Next() {
		var g RevenueGroup
		if err := rows.Scan(&g.AccountKey, &g.Amount); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// SetInvoiceJournalEntry links an issued invoice to the AR journal entry.
func (r *Repo) SetInvoiceJournalEntry(ctx context.Context, tx pgx.Tx, tenantID, id, entryID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE customer_invoices SET journal_entry_id = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, id, entryID)
	return err
}

func (r *Repo) GetInvoice(ctx context.Context, tenantID, id uuid.UUID) (*CustomerInvoice, error) {
	var i CustomerInvoice
	err := scanInvoice(r.pool.QueryRow(ctx, `SELECT `+invoiceColumns+` FROM customer_invoices WHERE tenant_id = $1 AND id = $2`, tenantID, id), &i)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func (r *Repo) ListInvoices(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID) ([]CustomerInvoice, error) {
	var custFilter *uuid.UUID
	if customerID != uuid.Nil {
		custFilter = &customerID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+invoiceColumns+` FROM customer_invoices
		WHERE tenant_id = $1 AND ($2::uuid IS NULL OR customer_id = $2)
		ORDER BY invoice_date DESC, created_at DESC
	`, tenantID, custFilter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomerInvoice{}
	for rows.Next() {
		var i CustomerInvoice
		if err := scanInvoice(rows, &i); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// ApplyInvoicePayment reduces an issued invoice's outstanding balance, updating
// status. An amount over the outstanding balance yields ErrOverAllocated.
func (r *Repo) ApplyInvoicePayment(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, amount string) (*CustomerInvoice, error) {
	var i CustomerInvoice
	err := scanInvoice(tx.QueryRow(ctx, `
		UPDATE customer_invoices SET
		    outstanding_amount = outstanding_amount - $3::numeric,
		    status = CASE
		        WHEN outstanding_amount - $3::numeric <= 0 THEN 'paid'
		        ELSE 'partially_paid'
		    END
		WHERE tenant_id = $1 AND id = $2 AND status IN ('issued', 'partially_paid') AND outstanding_amount >= $3::numeric
		RETURNING `+invoiceColumns,
		tenantID, id, amount,
	), &i)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOverAllocated
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}

// InvoiceAging returns customers with outstanding issued invoices, largest
// first — the finance-ledger AR aging (distinct from the operational
// ar_entries balance).
func (r *Repo) InvoiceAging(ctx context.Context, tenantID uuid.UUID) ([]CustomerBalance, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT ci.customer_id, c.code, c.name, SUM(ci.outstanding_amount)::text
		FROM customer_invoices ci
		JOIN customers c ON c.id = ci.customer_id AND c.tenant_id = ci.tenant_id
		WHERE ci.tenant_id = $1 AND ci.status IN ('issued', 'partially_paid')
		GROUP BY ci.customer_id, c.code, c.name
		HAVING SUM(ci.outstanding_amount) > 0
		ORDER BY SUM(ci.outstanding_amount) DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomerBalance{}
	for rows.Next() {
		var b CustomerBalance
		if err := rows.Scan(&b.CustomerID, &b.Code, &b.Name, &b.Balance); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
