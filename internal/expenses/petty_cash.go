package expenses

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Float struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	StationID uuid.UUID
	Name      string
	Balance   string
	Status    string
	CreatedBy uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

type PettyTransaction struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	FloatID        uuid.UUID
	TxnType        string
	Amount         string
	BalanceAfter   string
	Description    *string
	AccountKey     *string
	Overdraw       bool
	JournalEntryID *uuid.UUID
	CreatedBy      uuid.UUID
	CreatedAt      time.Time
}

type PettyReconciliation struct {
	ID              uuid.UUID
	FloatID         uuid.UUID
	ExpectedBalance string
	CountedCash     string
	Variance        string
	ShortAmount     string // debit cash over/short when counted < expected
	OverAmount      string // credit cash over/short when counted > expected
}

const floatColumns = `id, tenant_id, station_id, name, balance::text, status, created_by, created_at, updated_at`

func scanFloat(row pgx.Row, f *Float) error {
	return row.Scan(&f.ID, &f.TenantID, &f.StationID, &f.Name, &f.Balance, &f.Status, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt)
}

const pettyTxnColumns = `
    id, tenant_id, float_id, txn_type, amount::text, balance_after::text, description,
    account_key, overdraw, journal_entry_id, created_by, created_at
`

func scanPettyTxn(row pgx.Row, t *PettyTransaction) error {
	return row.Scan(
		&t.ID, &t.TenantID, &t.FloatID, &t.TxnType, &t.Amount, &t.BalanceAfter, &t.Description,
		&t.AccountKey, &t.Overdraw, &t.JournalEntryID, &t.CreatedBy, &t.CreatedAt,
	)
}

func (r *Repo) CreateFloat(ctx context.Context, tx pgx.Tx, tenantID, stationID uuid.UUID, name string, createdBy uuid.UUID) (*Float, error) {
	var f Float
	if err := scanFloat(tx.QueryRow(ctx, `
		INSERT INTO petty_cash_floats (tenant_id, station_id, name, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING `+floatColumns,
		tenantID, stationID, name, createdBy,
	), &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *Repo) GetFloat(ctx context.Context, tenantID, id uuid.UUID) (*Float, error) {
	var f Float
	err := scanFloat(r.pool.QueryRow(ctx, `SELECT `+floatColumns+` FROM petty_cash_floats WHERE tenant_id = $1 AND id = $2`, tenantID, id), &f)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *Repo) ListFloats(ctx context.Context, tenantID uuid.UUID) ([]Float, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+floatColumns+` FROM petty_cash_floats WHERE tenant_id = $1 ORDER BY name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Float{}
	for rows.Next() {
		var f Float
		if err := scanFloat(rows, &f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// increases lists the transaction types that add to a float's balance; all
// others subtract.
func increases(txnType string) bool {
	switch txnType {
	case "topup", "reimbursement", "adjustment":
		return true
	default: // spend, transfer
		return false
	}
}

// RecordTransaction adjusts the float balance and records the transaction. A
// decreasing transaction that would overdraw the float is rejected with
// ErrOverdraw unless overdraw is set.
func (r *Repo) RecordTransaction(ctx context.Context, tx pgx.Tx, tenantID, floatID uuid.UUID, txnType, amount string, description, accountKey *string, overdraw bool, createdBy uuid.UUID) (*PettyTransaction, error) {
	// Float must exist and be active.
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM petty_cash_floats WHERE tenant_id = $1 AND id = $2`, tenantID, floatID).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if status != "active" {
		return nil, ErrFloatBusy
	}

	sign := 1
	if !increases(txnType) {
		sign = -1
	}
	var balanceAfter string
	err := tx.QueryRow(ctx, `
		UPDATE petty_cash_floats
		SET balance = balance + ($3::numeric * $4::int)
		WHERE tenant_id = $1 AND id = $2
		  AND (balance + ($3::numeric * $4::int) >= 0 OR $5)
		RETURNING balance::text
	`, tenantID, floatID, amount, sign, overdraw).Scan(&balanceAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOverdraw
	}
	if err != nil {
		return nil, err
	}

	var t PettyTransaction
	if err := scanPettyTxn(tx.QueryRow(ctx, `
		INSERT INTO petty_cash_transactions
		    (tenant_id, float_id, txn_type, amount, balance_after, description, account_key, overdraw, created_by)
		VALUES ($1, $2, $3, $4::numeric, $5::numeric, $6, $7, $8, $9)
		RETURNING `+pettyTxnColumns,
		tenantID, floatID, txnType, amount, balanceAfter, description, accountKey, overdraw, createdBy,
	), &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repo) SetTransactionJournalEntry(ctx context.Context, tx pgx.Tx, tenantID, id, entryID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE petty_cash_transactions SET journal_entry_id = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, id, entryID)
	return err
}

func (r *Repo) ListTransactions(ctx context.Context, tenantID, floatID uuid.UUID) ([]PettyTransaction, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+pettyTxnColumns+` FROM petty_cash_transactions WHERE tenant_id = $1 AND float_id = $2 ORDER BY created_at`, tenantID, floatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PettyTransaction{}
	for rows.Next() {
		var t PettyTransaction
		if err := scanPettyTxn(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ReconcileFloat records a count against the float's expected balance, inserts
// the reconciliation, and sets the float balance to the counted amount. The
// returned over/short magnitudes drive the variance journal entry.
func (r *Repo) ReconcileFloat(ctx context.Context, tx pgx.Tx, tenantID, floatID uuid.UUID, counted string, reconciledBy uuid.UUID) (*PettyReconciliation, error) {
	var expected string
	if err := tx.QueryRow(ctx, `SELECT balance::text FROM petty_cash_floats WHERE tenant_id = $1 AND id = $2`, tenantID, floatID).Scan(&expected); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}

	var rec PettyReconciliation
	rec.FloatID = floatID
	if err := tx.QueryRow(ctx, `
		INSERT INTO petty_cash_reconciliations (tenant_id, float_id, expected_balance, counted_cash, variance, reconciled_by)
		VALUES ($1, $2, $3::numeric, $4::numeric, $4::numeric - $3::numeric, $5)
		RETURNING id, expected_balance::text, counted_cash::text, variance::text,
		          GREATEST($3::numeric - $4::numeric, 0)::text, GREATEST($4::numeric - $3::numeric, 0)::text
	`, tenantID, floatID, expected, counted, reconciledBy).Scan(
		&rec.ID, &rec.ExpectedBalance, &rec.CountedCash, &rec.Variance, &rec.ShortAmount, &rec.OverAmount,
	); err != nil {
		return nil, err
	}
	// The physical count becomes the new book balance.
	if _, err := tx.Exec(ctx, `UPDATE petty_cash_floats SET balance = $3::numeric WHERE tenant_id = $1 AND id = $2`, tenantID, floatID, counted); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *Repo) SetReconciliationJournalEntry(ctx context.Context, tx pgx.Tx, tenantID, id, entryID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE petty_cash_reconciliations SET journal_entry_id = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, id, entryID)
	return err
}
