// Package accounting is the double-entry finance core (Phase 7): the chart of
// accounts, accounting periods, and the journal posting engine every finance
// document posts through. Money is carried as decimal strings; balance checks
// and arithmetic run in SQL.
package accounting

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

var (
	ErrAccountNotFound = errors.New("accounting: account not found")
	ErrSystemAccount   = errors.New("accounting: system account not configured")
)

type Account struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Code          string
	Name          string
	Type          string
	NormalBalance string
	ParentID      *uuid.UUID
	SystemKey     *string
	Status        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type AccountInput struct {
	Code          string
	Name          string
	Type          string
	NormalBalance string
	ParentID      *uuid.UUID
	SystemKey     *string
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const accountColumns = `
    id, tenant_id, code, name, type, normal_balance, parent_id, system_key, status, created_at, updated_at
`

func scanAccount(row pgx.Row, a *Account) error {
	return row.Scan(
		&a.ID, &a.TenantID, &a.Code, &a.Name, &a.Type, &a.NormalBalance,
		&a.ParentID, &a.SystemKey, &a.Status, &a.CreatedAt, &a.UpdatedAt,
	)
}

// defaultChart is the seeded fuel-retail chart of accounts.
var defaultChart = []AccountInput{
	{Code: "1000", Name: "Cash on Hand", Type: "asset", NormalBalance: "debit", SystemKey: ptrStr("cash_on_hand")},
	{Code: "1005", Name: "Petty Cash", Type: "asset", NormalBalance: "debit", SystemKey: ptrStr("petty_cash")},
	{Code: "1010", Name: "Bank Clearing", Type: "asset", NormalBalance: "debit", SystemKey: ptrStr("bank_clearing")},
	{Code: "1020", Name: "Bank", Type: "asset", NormalBalance: "debit", SystemKey: ptrStr("bank")},
	{Code: "1100", Name: "Accounts Receivable", Type: "asset", NormalBalance: "debit", SystemKey: ptrStr("accounts_receivable")},
	{Code: "1200", Name: "Inventory", Type: "asset", NormalBalance: "debit", SystemKey: ptrStr("inventory")},
	{Code: "2000", Name: "Accounts Payable", Type: "liability", NormalBalance: "credit", SystemKey: ptrStr("accounts_payable")},
	{Code: "2100", Name: "Sales Clearing", Type: "liability", NormalBalance: "credit", SystemKey: ptrStr("sales_clearing")},
	{Code: "2200", Name: "Customer Credits", Type: "liability", NormalBalance: "credit", SystemKey: ptrStr("customer_credits")},
	{Code: "2300", Name: "Output VAT Payable", Type: "liability", NormalBalance: "credit", SystemKey: ptrStr("output_vat")},
	{Code: "3000", Name: "Retained Earnings", Type: "equity", NormalBalance: "credit", SystemKey: ptrStr("retained_earnings")},
	{Code: "4000", Name: "Sales Revenue", Type: "income", NormalBalance: "credit", SystemKey: ptrStr("sales_revenue")},
	{Code: "4100", Name: "Discounts", Type: "contra_income", NormalBalance: "debit", SystemKey: ptrStr("discounts")},
	{Code: "5000", Name: "Cost of Goods Sold", Type: "expense", NormalBalance: "debit", SystemKey: ptrStr("cogs")},
	{Code: "5100", Name: "Fuel Purchases", Type: "expense", NormalBalance: "debit", SystemKey: ptrStr("fuel_purchases")},
	{Code: "5200", Name: "Freight, Duty & Levies", Type: "expense", NormalBalance: "debit", SystemKey: ptrStr("freight_duty_levies")},
	{Code: "5300", Name: "Operating Expenses", Type: "expense", NormalBalance: "debit", SystemKey: ptrStr("operating_expense")},
	{Code: "5400", Name: "Cash Over/Short", Type: "expense", NormalBalance: "debit", SystemKey: ptrStr("cash_over_short")},
}

func ptrStr(s string) *string { return &s }

// SeedDefaultChart inserts the default chart for a tenant idempotently.
func (r *Repo) SeedDefaultChart(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (int, error) {
	n := 0
	for _, a := range defaultChart {
		tag, err := tx.Exec(ctx, `
			INSERT INTO accounts (tenant_id, code, name, type, normal_balance, system_key)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (tenant_id, lower(code)) DO NOTHING
		`, tenantID, a.Code, a.Name, a.Type, a.NormalBalance, a.SystemKey)
		if err != nil {
			return n, err
		}
		n += int(tag.RowsAffected())
	}
	return n, nil
}

func (r *Repo) ListAccounts(ctx context.Context, tenantID uuid.UUID) ([]Account, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+accountColumns+` FROM accounts WHERE tenant_id = $1 ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		var a Account
		if err := scanAccount(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repo) GetAccount(ctx context.Context, tenantID, id uuid.UUID) (*Account, error) {
	var a Account
	err := scanAccount(r.pool.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts WHERE tenant_id = $1 AND id = $2`, tenantID, id), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAccountNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repo) CreateAccount(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in AccountInput) (*Account, error) {
	var a Account
	if err := scanAccount(tx.QueryRow(ctx, `
		INSERT INTO accounts (tenant_id, code, name, type, normal_balance, parent_id, system_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+accountColumns,
		tenantID, in.Code, in.Name, in.Type, in.NormalBalance, in.ParentID, in.SystemKey,
	), &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repo) UpdateAccount(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, name string, status string) (*Account, error) {
	var a Account
	err := scanAccount(tx.QueryRow(ctx, `
		UPDATE accounts SET name = COALESCE(NULLIF($3, ''), name), status = COALESCE(NULLIF($4, ''), status)
		WHERE tenant_id = $1 AND id = $2
		RETURNING `+accountColumns,
		tenantID, id, name, status,
	), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAccountNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// AccountHasPostings reports whether an account has any journal lines — the
// guard against deactivating an in-use account.
func (r *Repo) AccountHasPostings(ctx context.Context, tenantID, id uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM journal_lines WHERE tenant_id = $1 AND account_id = $2)
	`, tenantID, id).Scan(&exists)
	return exists, err
}

// resolveSystemAccount returns the account id mapped to a system_key, within
// the caller's querier.
func (r *Repo) resolveSystemAccount(ctx context.Context, q database.Querier, tenantID uuid.UUID, key string) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT id FROM accounts WHERE tenant_id = $1 AND system_key = $2 AND status = 'active'
	`, tenantID, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrSystemAccount
	}
	return id, err
}
