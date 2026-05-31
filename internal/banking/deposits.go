package banking

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type BankAccount struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Name          string
	AccountNumber *string
	Currency      string
	Status        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type BankDeposit struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	StationID        uuid.UUID
	BankAccountID    uuid.UUID
	SlipNumber       *string
	Amount           string
	Reference        *string
	ExpectedBankDate *time.Time
	ActualBankDate   *time.Time
	Status           string
	PreparedEntryID  *uuid.UUID
	ConfirmedEntryID *uuid.UUID
	CreatedBy        uuid.UUID
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

const bankAccountColumns = `id, tenant_id, name, account_number, currency, status, created_at, updated_at`

func scanBankAccount(row pgx.Row, a *BankAccount) error {
	return row.Scan(&a.ID, &a.TenantID, &a.Name, &a.AccountNumber, &a.Currency, &a.Status, &a.CreatedAt, &a.UpdatedAt)
}

const depositColumns = `
    id, tenant_id, station_id, bank_account_id, slip_number, amount::text, reference,
    expected_bank_date, actual_bank_date, status, prepared_entry_id, confirmed_entry_id,
    created_by, created_at, updated_at
`

func scanDeposit(row pgx.Row, d *BankDeposit) error {
	return row.Scan(
		&d.ID, &d.TenantID, &d.StationID, &d.BankAccountID, &d.SlipNumber, &d.Amount, &d.Reference,
		&d.ExpectedBankDate, &d.ActualBankDate, &d.Status, &d.PreparedEntryID, &d.ConfirmedEntryID,
		&d.CreatedBy, &d.CreatedAt, &d.UpdatedAt,
	)
}

// ---- Bank accounts ----

func (r *Repo) CreateBankAccount(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, name string, accountNumber *string, currency string) (*BankAccount, error) {
	if currency == "" {
		currency = "NGN"
	}
	var a BankAccount
	if err := scanBankAccount(tx.QueryRow(ctx, `
		INSERT INTO bank_accounts (tenant_id, name, account_number, currency)
		VALUES ($1, $2, $3, $4)
		RETURNING `+bankAccountColumns,
		tenantID, name, accountNumber, currency,
	), &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repo) ListBankAccounts(ctx context.Context, tenantID uuid.UUID) ([]BankAccount, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+bankAccountColumns+` FROM bank_accounts WHERE tenant_id = $1 ORDER BY name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BankAccount{}
	for rows.Next() {
		var a BankAccount
		if err := scanBankAccount(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListBankAccountsPage returns a page of bank accounts for the tenant ordered by
// name (with id as a tiebreaker for stable paging), applying the supplied limit
// and offset.
func (r *Repo) ListBankAccountsPage(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]BankAccount, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+bankAccountColumns+` FROM bank_accounts WHERE tenant_id = $1 ORDER BY name, id LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BankAccount{}
	for rows.Next() {
		var a BankAccount
		if err := scanBankAccount(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- Deposits ----

type DepositInput struct {
	StationID        uuid.UUID
	BankAccountID    uuid.UUID
	SlipNumber       *string
	Reference        *string
	ExpectedBankDate *time.Time
	CreatedBy        uuid.UUID
}

func (r *Repo) CreateDeposit(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in DepositInput) (*BankDeposit, error) {
	var d BankDeposit
	if err := scanDeposit(tx.QueryRow(ctx, `
		INSERT INTO bank_deposits (tenant_id, station_id, bank_account_id, slip_number, reference, expected_bank_date, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+depositColumns,
		tenantID, in.StationID, in.BankAccountID, in.SlipNumber, in.Reference, in.ExpectedBankDate, in.CreatedBy,
	), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// AddDepositLine attaches a posted cash reconciliation to a draft deposit. A
// reconciliation already deposited is rejected with ErrDuplicate.
func (r *Repo) AddDepositLine(ctx context.Context, tx pgx.Tx, tenantID, depositID, cashReconciliationID uuid.UUID, amount string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO bank_deposit_lines (tenant_id, bank_deposit_id, cash_reconciliation_id, amount)
		VALUES ($1, $2, $3, $4::numeric)
	`, tenantID, depositID, cashReconciliationID, amount)
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	return err
}

func (r *Repo) GetDeposit(ctx context.Context, tenantID, id uuid.UUID) (*BankDeposit, error) {
	var d BankDeposit
	err := scanDeposit(r.pool.QueryRow(ctx, `SELECT `+depositColumns+` FROM bank_deposits WHERE tenant_id = $1 AND id = $2`, tenantID, id), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *Repo) ListDeposits(ctx context.Context, tenantID, stationID uuid.UUID) ([]BankDeposit, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+depositColumns+` FROM bank_deposits
		WHERE tenant_id = $1 AND ($2::uuid IS NULL OR station_id = $2)
		ORDER BY created_at DESC
	`, tenantID, nullUUID(stationID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BankDeposit{}
	for rows.Next() {
		var d BankDeposit
		if err := scanDeposit(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListDepositsPage returns a page of bank deposits for the tenant (optionally
// filtered by station), newest first by created_at (with id as a tiebreaker for
// stable paging), applying the supplied limit and offset.
func (r *Repo) ListDepositsPage(ctx context.Context, tenantID, stationID uuid.UUID, limit, offset int) ([]BankDeposit, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+depositColumns+` FROM bank_deposits
		WHERE tenant_id = $1 AND ($2::uuid IS NULL OR station_id = $2)
		ORDER BY created_at DESC, id
		LIMIT $3 OFFSET $4
	`, tenantID, nullUUID(stationID), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BankDeposit{}
	for rows.Next() {
		var d BankDeposit
		if err := scanDeposit(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// PrepareDeposit sets the deposit amount to the sum of its lines and moves
// draft -> prepared, attaching the cash-on-hand -> bank-clearing journal entry.
// It returns the deposit amount (string) so the caller can post the entry.
func (r *Repo) PrepareDeposit(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (string, error) {
	var amount string
	err := tx.QueryRow(ctx, `
		UPDATE bank_deposits
		SET amount = COALESCE((SELECT SUM(amount) FROM bank_deposit_lines WHERE tenant_id = $1 AND bank_deposit_id = $2), 0),
		    status = 'prepared'
		WHERE tenant_id = $1 AND id = $2 AND status = 'draft'
		RETURNING amount::text
	`, tenantID, id).Scan(&amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrBadState
	}
	return amount, err
}

func (r *Repo) SetDepositPreparedEntry(ctx context.Context, tx pgx.Tx, tenantID, id, entryID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE bank_deposits SET prepared_entry_id = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, id, entryID)
	return err
}

// ConfirmDeposit moves prepared -> posted, records the actual bank date, and is
// followed by the bank-clearing -> bank journal entry. Returns the amount.
func (r *Repo) ConfirmDeposit(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, actualDate time.Time, reference *string) (string, error) {
	var amount string
	err := tx.QueryRow(ctx, `
		UPDATE bank_deposits
		SET status = 'posted', actual_bank_date = $3, reference = COALESCE($4, reference)
		WHERE tenant_id = $1 AND id = $2 AND status IN ('prepared', 'in_transit', 'confirmed')
		RETURNING amount::text
	`, tenantID, id, actualDate, reference).Scan(&amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrBadState
	}
	return amount, err
}

func (r *Repo) SetDepositConfirmedEntry(ctx context.Context, tx pgx.Tx, tenantID, id, entryID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE bank_deposits SET confirmed_entry_id = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, id, entryID)
	return err
}
