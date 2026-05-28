package accounting

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Reports read posted journal lines so every total drills back to entries.
// Entries with status 'posted' or 'reversed' are included — a reversed entry
// and its reversal both stay on the books and net to zero. Drafts are excluded.

// TrialBalanceRow is one account's debit/credit/balance totals.
type TrialBalanceRow struct {
	AccountID     uuid.UUID
	Code          string
	Name          string
	Type          string
	NormalBalance string
	Debit         string
	Credit        string
	Balance       string
}

// TrialBalance returns per-account debit/credit/balance totals as of a date.
func (r *Repo) TrialBalance(ctx context.Context, tenantID uuid.UUID, asOf time.Time) ([]TrialBalanceRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT a.id, a.code, a.name, a.type, a.normal_balance,
		    COALESCE(SUM(l.debit), 0)::text,
		    COALESCE(SUM(l.credit), 0)::text,
		    (COALESCE(SUM(l.debit), 0) - COALESCE(SUM(l.credit), 0))::text
		FROM accounts a
		LEFT JOIN journal_lines l ON l.account_id = a.id AND l.tenant_id = a.tenant_id
		LEFT JOIN journal_entries e ON e.id = l.journal_entry_id AND e.tenant_id = l.tenant_id
		    AND e.status IN ('posted', 'reversed') AND e.entry_date <= $2
		WHERE a.tenant_id = $1
		GROUP BY a.id, a.code, a.name, a.type, a.normal_balance
		HAVING COALESCE(SUM(l.debit), 0) <> 0 OR COALESCE(SUM(l.credit), 0) <> 0
		ORDER BY a.code
	`, tenantID, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrialBalanceRow{}
	for rows.Next() {
		var t TrialBalanceRow
		if err := rows.Scan(&t.AccountID, &t.Code, &t.Name, &t.Type, &t.NormalBalance, &t.Debit, &t.Credit, &t.Balance); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// IncomeStatement is revenue, expenses, and net profit over a period.
type IncomeStatement struct {
	Revenue   string
	Expenses  string
	NetProfit string
}

func (r *Repo) IncomeStatement(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (IncomeStatement, error) {
	var s IncomeStatement
	err := r.pool.QueryRow(ctx, `
		SELECT
		    COALESCE(SUM(CASE WHEN a.type = 'income' THEN l.credit - l.debit
		                      WHEN a.type = 'contra_income' THEN -(l.debit - l.credit) END), 0)::text,
		    COALESCE(SUM(CASE WHEN a.type = 'expense' THEN l.debit - l.credit END), 0)::text,
		    (COALESCE(SUM(CASE WHEN a.type = 'income' THEN l.credit - l.debit
		                       WHEN a.type = 'contra_income' THEN -(l.debit - l.credit) END), 0)
		     - COALESCE(SUM(CASE WHEN a.type = 'expense' THEN l.debit - l.credit END), 0))::text
		FROM journal_lines l
		JOIN journal_entries e ON e.id = l.journal_entry_id AND e.tenant_id = l.tenant_id
		JOIN accounts a ON a.id = l.account_id AND a.tenant_id = l.tenant_id
		WHERE l.tenant_id = $1 AND e.status IN ('posted', 'reversed') AND e.entry_date BETWEEN $2 AND $3
	`, tenantID, from, to).Scan(&s.Revenue, &s.Expenses, &s.NetProfit)
	return s, err
}

// BalanceSheet is the asset/liability/equity position as of a date.
type BalanceSheet struct {
	Assets      string
	Liabilities string
	Equity      string
}

func (r *Repo) BalanceSheet(ctx context.Context, tenantID uuid.UUID, asOf time.Time) (BalanceSheet, error) {
	var b BalanceSheet
	err := r.pool.QueryRow(ctx, `
		SELECT
		    COALESCE(SUM(CASE WHEN a.type IN ('asset', 'contra_asset') THEN l.debit - l.credit END), 0)::text,
		    COALESCE(SUM(CASE WHEN a.type = 'liability' THEN l.credit - l.debit END), 0)::text,
		    COALESCE(SUM(CASE WHEN a.type = 'equity' THEN l.credit - l.debit END), 0)::text
		FROM journal_lines l
		JOIN journal_entries e ON e.id = l.journal_entry_id AND e.tenant_id = l.tenant_id
		JOIN accounts a ON a.id = l.account_id AND a.tenant_id = l.tenant_id
		WHERE l.tenant_id = $1 AND e.status IN ('posted', 'reversed') AND e.entry_date <= $2
	`, tenantID, asOf).Scan(&b.Assets, &b.Liabilities, &b.Equity)
	return b, err
}

// GeneralLedgerRow is one journal line in an account's ledger.
type GeneralLedgerRow struct {
	EntryID     uuid.UUID
	EntryNumber int64
	EntryDate   time.Time
	SourceType  string
	Memo        *string
	Debit       string
	Credit      string
}

// GeneralLedger returns an account's posted lines in date order.
func (r *Repo) GeneralLedger(ctx context.Context, tenantID, accountID uuid.UUID) ([]GeneralLedgerRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT e.id, e.entry_number, e.entry_date, e.source_type, e.memo, l.debit::text, l.credit::text
		FROM journal_lines l
		JOIN journal_entries e ON e.id = l.journal_entry_id AND e.tenant_id = l.tenant_id
		WHERE l.tenant_id = $1 AND l.account_id = $2 AND e.status IN ('posted', 'reversed')
		ORDER BY e.entry_date, e.entry_number
	`, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GeneralLedgerRow{}
	for rows.Next() {
		var g GeneralLedgerRow
		if err := rows.Scan(&g.EntryID, &g.EntryNumber, &g.EntryDate, &g.SourceType, &g.Memo, &g.Debit, &g.Credit); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
