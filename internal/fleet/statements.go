package fleet

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Statement struct {
	ID             uuid.UUID
	CustomerID     uuid.UUID
	PeriodStart    time.Time
	PeriodEnd      time.Time
	OpeningBalance string
	Charges        string
	Payments       string
	ClosingBalance string
	Status         string
	GeneratedAt    time.Time
}

const statementColumns = `
    id, customer_id, period_start, period_end, opening_balance::text, charges::text,
    payments::text, closing_balance::text, status, generated_at
`

func scanStatement(row pgx.Row, s *Statement) error {
	return row.Scan(
		&s.ID, &s.CustomerID, &s.PeriodStart, &s.PeriodEnd, &s.OpeningBalance, &s.Charges,
		&s.Payments, &s.ClosingBalance, &s.Status, &s.GeneratedAt,
	)
}

// GenerateStatement computes a draft statement for a customer over a period
// from the Phase-7 AR ledger (opening, charges, payments, closing).
func (r *Repo) GenerateStatement(ctx context.Context, tx pgx.Tx, tenantID, customerID uuid.UUID, start, end time.Time, generatedBy uuid.UUID) (*Statement, error) {
	var s Statement
	err := scanStatement(tx.QueryRow(ctx, `
		WITH e AS (SELECT entry_type, amount, recorded_at::date AS d FROM ar_entries WHERE tenant_id = $1 AND customer_id = $2)
		INSERT INTO customer_statements
		    (tenant_id, customer_id, period_start, period_end, opening_balance, charges, payments, closing_balance, generated_by)
		VALUES (
		    $1, $2, $3, $4,
		    COALESCE((SELECT SUM(amount) FROM e WHERE d < $3), 0),
		    COALESCE((SELECT SUM(amount) FROM e WHERE d BETWEEN $3 AND $4 AND entry_type = 'charge'), 0),
		    COALESCE((SELECT -SUM(amount) FROM e WHERE d BETWEEN $3 AND $4 AND entry_type = 'payment'), 0),
		    COALESCE((SELECT SUM(amount) FROM e WHERE d <= $4), 0),
		    $5
		)
		RETURNING `+statementColumns,
		tenantID, customerID, start, end, generatedBy,
	), &s)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repo) IssueStatement(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (*Statement, error) {
	var s Statement
	err := scanStatement(tx.QueryRow(ctx, `
		UPDATE customer_statements SET status = 'issued'
		WHERE tenant_id = $1 AND id = $2 AND status = 'draft'
		RETURNING `+statementColumns, tenantID, id), &s)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBadState
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repo) ListStatements(ctx context.Context, tenantID, customerID uuid.UUID) ([]Statement, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+statementColumns+` FROM customer_statements
		WHERE tenant_id = $1 AND customer_id = $2 ORDER BY period_end DESC
	`, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Statement{}
	for rows.Next() {
		var s Statement
		if err := scanStatement(rows, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListStatementsPage is the paginated variant of ListStatements (REL-REPO).
func (r *Repo) ListStatementsPage(ctx context.Context, tenantID, customerID uuid.UUID, limit, offset int) ([]Statement, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+statementColumns+` FROM customer_statements
		WHERE tenant_id = $1 AND customer_id = $2
		ORDER BY period_end DESC, id
		LIMIT $3 OFFSET $4
	`, tenantID, customerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Statement{}
	for rows.Next() {
		var s Statement
		if err := scanStatement(rows, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---- Credit alerts (Stage 13) ----

type CreditAlert struct {
	ID         uuid.UUID
	CustomerID uuid.UUID
	AlertType  string
	Severity   string
	Status     string
	Detail     *string
}

// RaiseAlert creates an alert idempotently (an existing open/acknowledged alert
// of the same type for the customer is left in place). Returns whether created.
func (r *Repo) RaiseAlert(ctx context.Context, tx pgx.Tx, tenantID, customerID uuid.UUID, alertType, severity, detail string) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO customer_credit_alerts (tenant_id, customer_id, alert_type, severity, detail)
		VALUES ($1, $2, $3, COALESCE(NULLIF($4,''),'medium'), $5)
		ON CONFLICT (tenant_id, customer_id, alert_type) WHERE status IN ('open','acknowledged') DO NOTHING
	`, tenantID, customerID, alertType, severity, detail)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ScanCreditAlerts raises deterministic alerts for over-limit and overdue
// customers across the tenant, returning how many new alerts were created.
func (r *Repo) ScanCreditAlerts(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (int, error) {
	created := 0
	// Over-limit exposure (AR + holds beyond the limit).
	tag, err := tx.Exec(ctx, `
		INSERT INTO customer_credit_alerts (tenant_id, customer_id, alert_type, severity, detail)
		SELECT c.tenant_id, c.id, 'over_limit', 'high', 'exposure exceeds credit limit'
		FROM customers c
		WHERE c.tenant_id = $1 AND c.credit_limit > 0
		  AND (COALESCE((SELECT SUM(amount) FROM ar_entries WHERE tenant_id = c.tenant_id AND customer_id = c.id), 0)
		       + COALESCE((SELECT SUM(approved_amount) FROM fuel_authorizations WHERE tenant_id = c.tenant_id AND customer_id = c.id AND status = 'approved'), 0)) > c.credit_limit
		ON CONFLICT (tenant_id, customer_id, alert_type) WHERE status IN ('open','acknowledged') DO NOTHING
	`, tenantID)
	if err != nil {
		return 0, err
	}
	created += int(tag.RowsAffected())
	// Overdue invoices.
	tag, err = tx.Exec(ctx, `
		INSERT INTO customer_credit_alerts (tenant_id, customer_id, alert_type, severity, detail)
		SELECT tenant_id, customer_id, 'overdue', 'medium', 'has past-due invoices'
		FROM customer_invoices
		WHERE tenant_id = $1 AND status IN ('issued','partially_paid') AND due_date < CURRENT_DATE
		GROUP BY tenant_id, customer_id
		ON CONFLICT (tenant_id, customer_id, alert_type) WHERE status IN ('open','acknowledged') DO NOTHING
	`, tenantID)
	if err != nil {
		return 0, err
	}
	created += int(tag.RowsAffected())
	return created, nil
}

func (r *Repo) ListAlerts(ctx context.Context, tenantID uuid.UUID, status string) ([]CreditAlert, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, customer_id, alert_type, severity, status, detail
		FROM customer_credit_alerts WHERE tenant_id = $1 AND ($2 = '' OR status = $2) ORDER BY created_at DESC
	`, tenantID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CreditAlert{}
	for rows.Next() {
		var a CreditAlert
		if err := rows.Scan(&a.ID, &a.CustomerID, &a.AlertType, &a.Severity, &a.Status, &a.Detail); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListAlertsPage is the paginated variant of ListAlerts (REL-REPO).
func (r *Repo) ListAlertsPage(ctx context.Context, tenantID uuid.UUID, status string, limit, offset int) ([]CreditAlert, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, customer_id, alert_type, severity, status, detail
		FROM customer_credit_alerts WHERE tenant_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC, id
		LIMIT $3 OFFSET $4
	`, tenantID, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CreditAlert{}
	for rows.Next() {
		var a CreditAlert
		if err := rows.Scan(&a.ID, &a.CustomerID, &a.AlertType, &a.Severity, &a.Status, &a.Detail); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repo) TransitionAlert(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, to string, reason *string, assignedTo *uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE customer_credit_alerts SET status = $3, resolution_reason = COALESCE($4, resolution_reason), assigned_to = COALESCE($5, assigned_to)
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id, to, reason, assignedTo)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
