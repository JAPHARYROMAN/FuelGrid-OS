package payments

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// ErrMpesaNotFound is returned when an M-Pesa transaction does not resolve.
var ErrMpesaNotFound = errors.New("payments: mpesa transaction not found")

// ErrMpesaNotPaid is returned when a reconcile targets a transaction that is
// not in the 'paid' state.
var ErrMpesaNotPaid = errors.New("payments: mpesa transaction is not paid")

// MpesaTransaction is one M-Pesa STK Push collection and its lifecycle.
type MpesaTransaction struct {
	ID                     uuid.UUID
	TenantID               uuid.UUID
	StationID              uuid.UUID
	CheckoutRequestID      string
	MerchantRequestID      *string
	Amount                 string
	Phone                  string
	Status                 string
	ResultCode             *int
	MpesaReceipt           *string
	AccountReference       *string
	Description            *string
	RawPayload             json.RawMessage
	ReconciledRevenueDayID *uuid.UUID
	ReconciledAt           *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

const mpesaColumns = `
    id, tenant_id, station_id, checkout_request_id, merchant_request_id,
    amount::text, phone, status, result_code, mpesa_receipt, account_reference,
    description, raw_payload, reconciled_revenue_day_id, reconciled_at,
    created_at, updated_at
`

func scanMpesa(row pgx.Row, m *MpesaTransaction) error {
	var raw []byte
	if err := row.Scan(
		&m.ID, &m.TenantID, &m.StationID, &m.CheckoutRequestID, &m.MerchantRequestID,
		&m.Amount, &m.Phone, &m.Status, &m.ResultCode, &m.MpesaReceipt, &m.AccountReference,
		&m.Description, &raw, &m.ReconciledRevenueDayID, &m.ReconciledAt,
		&m.CreatedAt, &m.UpdatedAt,
	); err != nil {
		return err
	}
	m.RawPayload = json.RawMessage(raw)
	return nil
}

// InitiateMpesaInput is the data captured when an STK push is acknowledged.
type InitiateMpesaInput struct {
	StationID         uuid.UUID
	CheckoutRequestID string
	MerchantRequestID string
	Amount            string
	Phone             string
	AccountReference  string
	Description       string
}

// InitiateMpesa records a freshly-initiated STK push as a 'pending' row. The
// (tenant_id, checkout_request_id) unique constraint makes a retry idempotent.
func (r *Repo) InitiateMpesa(ctx context.Context, q database.Querier, tenantID uuid.UUID, in InitiateMpesaInput) (*MpesaTransaction, error) {
	var m MpesaTransaction
	if err := scanMpesa(q.QueryRow(ctx, `
		INSERT INTO mpesa_transactions
		    (tenant_id, station_id, checkout_request_id, merchant_request_id,
		     amount, phone, account_reference, description, status)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5::numeric, $6, NULLIF($7, ''), NULLIF($8, ''), 'pending')
		RETURNING `+mpesaColumns,
		tenantID, in.StationID, in.CheckoutRequestID, in.MerchantRequestID,
		in.Amount, in.Phone, in.AccountReference, in.Description,
	), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// SettleMpesaInput carries the terminal callback result.
type SettleMpesaInput struct {
	CheckoutRequestID string
	Status            string // 'paid' | 'failed'
	ResultCode        int
	MpesaReceipt      string
	RawPayload        json.RawMessage
}

// SettleMpesaByCheckoutID applies a Daraja callback to the matching pending
// transaction, keyed by checkout_request_id. The Daraja callback is
// unauthenticated, so this runs on the owner pool (no tenant GUC) and is keyed
// solely by the globally-unique checkout id. It is idempotent: a row already in
// a terminal state (paid/failed) is returned unchanged rather than re-settled,
// so a duplicated callback can't double-apply. Returns ErrMpesaNotFound when no
// row matches.
func (r *Repo) SettleMpesaByCheckoutID(ctx context.Context, q database.Querier, in SettleMpesaInput) (*MpesaTransaction, error) {
	raw := in.RawPayload
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	var m MpesaTransaction
	err := scanMpesa(q.QueryRow(ctx, `
		UPDATE mpesa_transactions
		   SET status        = $2,
		       result_code   = $3,
		       mpesa_receipt = NULLIF($4, ''),
		       raw_payload   = $5::jsonb
		 WHERE checkout_request_id = $1
		   AND status = 'pending'
		RETURNING `+mpesaColumns,
		in.CheckoutRequestID, in.Status, in.ResultCode, in.MpesaReceipt, []byte(raw),
	), &m)
	if errors.Is(err, pgx.ErrNoRows) {
		// Either the checkout id is unknown, or the row is already terminal
		// (idempotent duplicate). Disambiguate so a true unknown is a 404 while
		// a duplicate ack is a clean no-op return of the existing row.
		existing, gErr := r.getMpesaByCheckoutID(ctx, q, in.CheckoutRequestID)
		if gErr != nil {
			return nil, gErr
		}
		return existing, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) getMpesaByCheckoutID(ctx context.Context, q database.Querier, checkoutID string) (*MpesaTransaction, error) {
	var m MpesaTransaction
	err := scanMpesa(q.QueryRow(ctx, `
		SELECT `+mpesaColumns+` FROM mpesa_transactions WHERE checkout_request_id = $1
	`, checkoutID), &m)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMpesaNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// GetMpesa loads one transaction by id, tenant-scoped.
func (r *Repo) GetMpesa(ctx context.Context, tenantID, id uuid.UUID) (*MpesaTransaction, error) {
	var m MpesaTransaction
	err := scanMpesa(r.pool.QueryRow(ctx, `
		SELECT `+mpesaColumns+` FROM mpesa_transactions WHERE tenant_id = $1 AND id = $2
	`, tenantID, id), &m)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMpesaNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// MpesaFilter narrows the list query.
type MpesaFilter struct {
	StationID *uuid.UUID
	Status    string
}

// ListMpesa returns a tenant's M-Pesa transactions, newest first, paged. It
// fetches limit rows at the given offset; the handler decides has_more.
func (r *Repo) ListMpesa(ctx context.Context, tenantID uuid.UUID, f MpesaFilter, limit, offset int) ([]MpesaTransaction, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+mpesaColumns+` FROM mpesa_transactions
		WHERE tenant_id = $1
		  AND ($2::uuid IS NULL OR station_id = $2)
		  AND ($3 = '' OR status = $3)
		ORDER BY created_at DESC
		LIMIT $4 OFFSET $5
	`, tenantID, f.StationID, f.Status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MpesaTransaction{}
	for rows.Next() {
		var m MpesaTransaction
		if err := scanMpesa(rows, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ReconcileMpesa links a paid transaction to a revenue day inside the caller's
// tx. It refuses a non-paid transaction (ErrMpesaNotPaid) and returns
// ErrMpesaNotFound for an unknown id. Re-reconciling simply repoints the day.
func (r *Repo) ReconcileMpesa(ctx context.Context, tx pgx.Tx, tenantID, id, revenueDayID uuid.UUID) (*MpesaTransaction, error) {
	var m MpesaTransaction
	err := scanMpesa(tx.QueryRow(ctx, `
		UPDATE mpesa_transactions
		   SET reconciled_revenue_day_id = $3, reconciled_at = now()
		 WHERE tenant_id = $1 AND id = $2 AND status = 'paid'
		RETURNING `+mpesaColumns,
		tenantID, id, revenueDayID,
	), &m)
	if errors.Is(err, pgx.ErrNoRows) {
		// Distinguish unknown id from wrong-state.
		if _, gErr := r.GetMpesa(ctx, tenantID, id); errors.Is(gErr, ErrMpesaNotFound) {
			return nil, ErrMpesaNotFound
		}
		return nil, ErrMpesaNotPaid
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}
