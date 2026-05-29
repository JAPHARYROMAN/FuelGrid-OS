package expenses

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Category struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	Name       string
	AccountKey string
	Status     string
}

type Expense struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	StationID      *uuid.UUID
	CategoryID     *uuid.UUID
	Payee          *string
	ExpenseDate    time.Time
	Amount         string
	AccountKey     string
	PaymentMode    string
	Reference      *string
	Notes          *string
	Status         string
	JournalEntryID *uuid.UUID
	ApprovedBy     *uuid.UUID
	CreatedBy      uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ExpenseInput struct {
	StationID   *uuid.UUID
	CategoryID  *uuid.UUID
	Payee       *string
	ExpenseDate time.Time
	Amount      string
	AccountKey  string
	PaymentMode string
	Reference   *string
	Notes       *string
	CreatedBy   uuid.UUID
}

const categoryColumns = `id, tenant_id, name, account_key, status`

func scanCategory(row pgx.Row, c *Category) error {
	return row.Scan(&c.ID, &c.TenantID, &c.Name, &c.AccountKey, &c.Status)
}

const expenseColumns = `
    id, tenant_id, station_id, category_id, payee, expense_date, amount::text, account_key,
    payment_mode, reference, notes, status, journal_entry_id, approved_by, created_by, created_at, updated_at
`

func scanExpense(row pgx.Row, e *Expense) error {
	return row.Scan(
		&e.ID, &e.TenantID, &e.StationID, &e.CategoryID, &e.Payee, &e.ExpenseDate, &e.Amount, &e.AccountKey,
		&e.PaymentMode, &e.Reference, &e.Notes, &e.Status, &e.JournalEntryID, &e.ApprovedBy, &e.CreatedBy,
		&e.CreatedAt, &e.UpdatedAt,
	)
}

// ---- Categories ----

func (r *Repo) CreateCategory(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, name, accountKey string) (*Category, error) {
	key := accountKey
	if key == "" {
		key = "operating_expense"
	}
	var c Category
	if err := scanCategory(tx.QueryRow(ctx, `
		INSERT INTO expense_categories (tenant_id, name, account_key)
		VALUES ($1, $2, $3)
		RETURNING `+categoryColumns,
		tenantID, name, key,
	), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) ListCategories(ctx context.Context, tenantID uuid.UUID) ([]Category, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+categoryColumns+` FROM expense_categories WHERE tenant_id = $1 ORDER BY name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		if err := scanCategory(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// resolveAccountKey returns the expense account to debit: the explicit
// AccountKey, else the category's account_key, else operating_expense.
func (r *Repo) resolveAccountKey(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in ExpenseInput) string {
	if in.AccountKey != "" {
		return in.AccountKey
	}
	if in.CategoryID != nil {
		var key string
		if err := tx.QueryRow(ctx, `SELECT account_key FROM expense_categories WHERE tenant_id = $1 AND id = $2`, tenantID, *in.CategoryID).Scan(&key); err == nil && key != "" {
			return key
		}
	}
	return "operating_expense"
}

// ---- Expenses ----

func (r *Repo) CreateExpense(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in ExpenseInput) (*Expense, error) {
	accountKey := r.resolveAccountKey(ctx, tx, tenantID, in)
	mode := in.PaymentMode
	if mode == "" {
		mode = "cash"
	}
	var e Expense
	if err := scanExpense(tx.QueryRow(ctx, `
		INSERT INTO expenses
		    (tenant_id, station_id, category_id, payee, expense_date, amount, account_key, payment_mode, reference, notes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8, $9, $10, $11)
		RETURNING `+expenseColumns,
		tenantID, in.StationID, in.CategoryID, in.Payee, in.ExpenseDate, in.Amount, accountKey, mode, in.Reference, in.Notes, in.CreatedBy,
	), &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *Repo) GetExpense(ctx context.Context, tenantID, id uuid.UUID) (*Expense, error) {
	var e Expense
	err := scanExpense(r.pool.QueryRow(ctx, `SELECT `+expenseColumns+` FROM expenses WHERE tenant_id = $1 AND id = $2`, tenantID, id), &e)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *Repo) ListExpenses(ctx context.Context, tenantID uuid.UUID, status string) ([]Expense, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+expenseColumns+` FROM expenses
		WHERE tenant_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY expense_date DESC, created_at DESC
	`, tenantID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Expense{}
	for rows.Next() {
		var e Expense
		if err := scanExpense(rows, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SubmitExpense moves draft -> submitted.
func (r *Repo) SubmitExpense(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (*Expense, error) {
	return r.transition(ctx, tx, tenantID, id, `status = 'submitted'`, `status = 'draft'`)
}

// ApproveExpense moves submitted -> approved, recording the approver.
// Separation of duties: the approver must not be the expense's creator. The
// row is locked so the state + creator check and the transition are atomic
// (no TOCTOU between validating and approving).
func (r *Repo) ApproveExpense(ctx context.Context, tx pgx.Tx, tenantID, id, approverID uuid.UUID) (*Expense, error) {
	var status string
	var createdBy uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT status, created_by FROM expenses
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, id).Scan(&status, &createdBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "submitted" {
		return nil, ErrBadState
	}
	if createdBy == approverID {
		return nil, ErrSelfApproval
	}

	var e Expense
	err = scanExpense(tx.QueryRow(ctx, `
		UPDATE expenses SET status = 'approved', approved_by = $3
		WHERE tenant_id = $1 AND id = $2 AND status = 'submitted'
		RETURNING `+expenseColumns,
		tenantID, id, approverID,
	), &e)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBadState
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// MarkExpensePosted moves approved -> posted and links the journal entry.
func (r *Repo) MarkExpensePosted(ctx context.Context, tx pgx.Tx, tenantID, id, entryID uuid.UUID) (*Expense, error) {
	var e Expense
	err := scanExpense(tx.QueryRow(ctx, `
		UPDATE expenses SET status = 'posted', journal_entry_id = $3
		WHERE tenant_id = $1 AND id = $2 AND status = 'approved'
		RETURNING `+expenseColumns,
		tenantID, id, entryID,
	), &e)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBadState
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *Repo) transition(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, set, where string) (*Expense, error) {
	var e Expense
	err := scanExpense(tx.QueryRow(ctx,
		`UPDATE expenses SET `+set+` WHERE tenant_id = $1 AND id = $2 AND `+where+` RETURNING `+expenseColumns,
		tenantID, id), &e)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBadState
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}
