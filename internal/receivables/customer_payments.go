package receivables

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrPaymentNotReversible is returned when reversing a customer payment that is
// not in a reversible state (e.g. it is already voided). Reversal is
// append-only: the original payment and its allocations are preserved, the
// payment is marked 'voided', and the affected invoice balances are restored.
var ErrPaymentNotReversible = errors.New("receivables: payment is not in a reversible state")

// CustomerPaymentAllocation is one application of a customer payment to an
// invoice. Amount is the exact decimal STRING the numeric column stores.
type CustomerPaymentAllocation struct {
	ID                uuid.UUID
	CustomerInvoiceID uuid.UUID
	Amount            string
}

type CustomerPayment struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	CustomerID       uuid.UUID
	PaymentDate      time.Time
	Method           string
	Reference        *string
	Amount           string
	AllocatedAmount  string
	SourceAccountKey string
	Status           string
	JournalEntryID   *uuid.UUID
	CreatedBy        uuid.UUID
	CreatedAt        time.Time
}

type CustomerPaymentInput struct {
	CustomerID       uuid.UUID
	PaymentDate      time.Time
	Method           string
	Reference        *string
	SourceAccountKey string
	CreatedBy        uuid.UUID
}

const customerPaymentColumns = `
    id, tenant_id, customer_id, payment_date, method, reference, amount::text,
    allocated_amount::text, source_account_key, status, journal_entry_id, created_by, created_at
`

func scanCustomerPayment(row pgx.Row, p *CustomerPayment) error {
	return row.Scan(
		&p.ID, &p.TenantID, &p.CustomerID, &p.PaymentDate, &p.Method, &p.Reference, &p.Amount,
		&p.AllocatedAmount, &p.SourceAccountKey, &p.Status, &p.JournalEntryID, &p.CreatedBy, &p.CreatedAt,
	)
}

// CreateCustomerPayment inserts a payment header (amount set from allocations).
func (r *Repo) CreateCustomerPayment(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CustomerPaymentInput) (*CustomerPayment, error) {
	var p CustomerPayment
	if err := scanCustomerPayment(tx.QueryRow(ctx, `
		INSERT INTO customer_payments
		    (tenant_id, customer_id, payment_date, method, reference, amount, source_account_key, created_by)
		VALUES ($1, $2, $3, $4, $5, 0, COALESCE(NULLIF($6, ''), 'bank'), $7)
		RETURNING `+customerPaymentColumns,
		tenantID, in.CustomerID, in.PaymentDate, in.Method, in.Reference, in.SourceAccountKey, in.CreatedBy,
	), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// AddCustomerAllocation records an allocation to an invoice and bumps the
// payment's allocated total.
func (r *Repo) AddCustomerAllocation(ctx context.Context, tx pgx.Tx, tenantID, paymentID, invoiceID uuid.UUID, amount string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO customer_payment_allocations (tenant_id, customer_payment_id, customer_invoice_id, amount)
		VALUES ($1, $2, $3, $4::numeric)
	`, tenantID, paymentID, invoiceID, amount); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE customer_payments SET allocated_amount = allocated_amount + $3::numeric, amount = amount + $3::numeric
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, paymentID, amount)
	return err
}

// SetCustomerPaymentJournalEntry links a payment to its journal entry.
func (r *Repo) SetCustomerPaymentJournalEntry(ctx context.Context, tx pgx.Tx, tenantID, id, entryID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE customer_payments SET journal_entry_id = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, id, entryID)
	return err
}

func (r *Repo) ListCustomerPayments(ctx context.Context, tenantID uuid.UUID) ([]CustomerPayment, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+customerPaymentColumns+` FROM customer_payments WHERE tenant_id = $1 ORDER BY payment_date DESC, created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomerPayment{}
	for rows.Next() {
		var p CustomerPayment
		if err := scanCustomerPayment(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetCustomerPayment returns a single customer payment by id, or ErrNotFound.
func (r *Repo) GetCustomerPayment(ctx context.Context, tenantID, id uuid.UUID) (*CustomerPayment, error) {
	var p CustomerPayment
	err := scanCustomerPayment(r.pool.QueryRow(ctx,
		`SELECT `+customerPaymentColumns+` FROM customer_payments WHERE tenant_id = $1 AND id = $2`,
		tenantID, id,
	), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// getCustomerPaymentTx reads a payment inside the caller's tx, locking the row
// FOR UPDATE so a concurrent reversal cannot double-restore balances.
func (r *Repo) getCustomerPaymentTx(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (*CustomerPayment, error) {
	var p CustomerPayment
	err := scanCustomerPayment(tx.QueryRow(ctx,
		`SELECT `+customerPaymentColumns+` FROM customer_payments WHERE tenant_id = $1 AND id = $2 FOR UPDATE`,
		tenantID, id,
	), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListCustomerAllocations returns a payment's allocations in insertion order.
func (r *Repo) ListCustomerAllocations(ctx context.Context, tenantID, paymentID uuid.UUID) ([]CustomerPaymentAllocation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, customer_invoice_id, amount::text
		FROM customer_payment_allocations
		WHERE tenant_id = $1 AND customer_payment_id = $2
		ORDER BY created_at, id
	`, tenantID, paymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomerPaymentAllocation{}
	for rows.Next() {
		var a CustomerPaymentAllocation
		if err := rows.Scan(&a.ID, &a.CustomerInvoiceID, &a.Amount); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReverseCustomerPayment voids a posted customer payment inside the caller's
// tx: it restores every allocated invoice's outstanding balance, recomputes the
// invoice status, and marks the payment 'voided'. The original payment row and
// its allocation rows are PRESERVED (append-only) — only the status flips, so
// the audit trail stays intact. The caller is responsible for reversing the
// linked journal entry.
//
// Reversing a payment that is not 'posted' (e.g. already voided) returns
// ErrPaymentNotReversible, which makes the operation idempotent-safe: a repeat
// reversal is refused rather than double-restoring balances. Returns the
// payment's allocations so the caller can audit what was restored.
func (r *Repo) ReverseCustomerPayment(ctx context.Context, tx pgx.Tx, tenantID, paymentID uuid.UUID) (*CustomerPayment, []CustomerPaymentAllocation, error) {
	pmt, err := r.getCustomerPaymentTx(ctx, tx, tenantID, paymentID)
	if err != nil {
		return nil, nil, err
	}
	if pmt.Status != "posted" {
		return nil, nil, ErrPaymentNotReversible
	}

	allocs, err := r.ListCustomerAllocations(ctx, tenantID, paymentID)
	if err != nil {
		return nil, nil, err
	}

	// Restore each invoice's outstanding balance and recompute its status. A
	// fully-restored invoice that had been settled returns to 'issued'; a
	// still-partially-paid one to 'partially_paid'. We add the allocation back
	// to outstanding without exceeding the invoice's amount.
	for _, a := range allocs {
		if _, err := tx.Exec(ctx, `
			UPDATE customer_invoices SET
			    outstanding_amount = LEAST(outstanding_amount + $3::numeric, amount),
			    status = CASE
			        WHEN LEAST(outstanding_amount + $3::numeric, amount) >= amount THEN 'issued'
			        ELSE 'partially_paid'
			    END
			WHERE tenant_id = $1 AND id = $2
		`, tenantID, a.CustomerInvoiceID, a.Amount); err != nil {
			return nil, nil, err
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE customer_payments SET status = 'voided' WHERE tenant_id = $1 AND id = $2`,
		tenantID, paymentID,
	); err != nil {
		return nil, nil, err
	}

	pmt.Status = "voided"
	return pmt, allocs, nil
}

// ListCustomerPaymentsPage returns a page of customer payments for the tenant,
// newest first by payment_date (with id as a tiebreaker for stable paging),
// applying the supplied limit and offset.
func (r *Repo) ListCustomerPaymentsPage(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]CustomerPayment, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+customerPaymentColumns+` FROM customer_payments WHERE tenant_id = $1 ORDER BY payment_date DESC, created_at DESC, id LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomerPayment{}
	for rows.Next() {
		var p CustomerPayment
		if err := scanCustomerPayment(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
