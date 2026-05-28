package payables

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SupplierPayment struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	SupplierID       uuid.UUID
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

type Allocation struct {
	PayableID uuid.UUID
	Amount    string
}

type PaymentInput struct {
	SupplierID       uuid.UUID
	PaymentDate      time.Time
	Method           string
	Reference        *string
	Amount           string
	SourceAccountKey string
	CreatedBy        uuid.UUID
}

const paymentColumns = `
    id, tenant_id, supplier_id, payment_date, method, reference, amount::text,
    allocated_amount::text, source_account_key, status, journal_entry_id, created_by, created_at
`

func scanPayment(row pgx.Row, p *SupplierPayment) error {
	return row.Scan(
		&p.ID, &p.TenantID, &p.SupplierID, &p.PaymentDate, &p.Method, &p.Reference, &p.Amount,
		&p.AllocatedAmount, &p.SourceAccountKey, &p.Status, &p.JournalEntryID, &p.CreatedBy, &p.CreatedAt,
	)
}

// CreatePayment inserts a supplier payment header inside the caller's tx.
func (r *Repo) CreatePayment(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in PaymentInput) (*SupplierPayment, error) {
	var p SupplierPayment
	if err := scanPayment(tx.QueryRow(ctx, `
		INSERT INTO supplier_payments
		    (tenant_id, supplier_id, payment_date, method, reference, amount, source_account_key, created_by)
		VALUES ($1, $2, $3, $4, $5, $6::numeric, COALESCE(NULLIF($7, ''), 'bank'), $8)
		RETURNING `+paymentColumns,
		tenantID, in.SupplierID, in.PaymentDate, in.Method, in.Reference, in.Amount, in.SourceAccountKey, in.CreatedBy,
	), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// AddAllocation records an allocation of a payment to a payable inside the tx.
func (r *Repo) AddAllocation(ctx context.Context, tx pgx.Tx, tenantID, paymentID, payableID uuid.UUID, amount string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO supplier_payment_allocations (tenant_id, supplier_payment_id, payable_id, amount)
		VALUES ($1, $2, $3, $4::numeric)
	`, tenantID, paymentID, payableID, amount); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE supplier_payments SET allocated_amount = allocated_amount + $3::numeric
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, paymentID, amount)
	return err
}

// SetPaymentJournalEntry links a payment to its journal entry.
func (r *Repo) SetPaymentJournalEntry(ctx context.Context, tx pgx.Tx, tenantID, id, entryID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE supplier_payments SET journal_entry_id = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, id, entryID)
	return err
}

func (r *Repo) ListPayments(ctx context.Context, tenantID uuid.UUID) ([]SupplierPayment, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+paymentColumns+` FROM supplier_payments WHERE tenant_id = $1 ORDER BY payment_date DESC, created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierPayment{}
	for rows.Next() {
		var p SupplierPayment
		if err := scanPayment(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
