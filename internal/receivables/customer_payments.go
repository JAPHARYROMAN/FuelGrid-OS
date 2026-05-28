package receivables

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

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
