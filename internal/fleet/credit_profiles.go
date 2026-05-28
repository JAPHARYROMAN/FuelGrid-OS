package fleet

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

type CreditProfile struct {
	ID                  uuid.UUID
	TenantID            uuid.UUID
	CustomerID          uuid.UUID
	PaymentTermsDays    int
	GraceDays           int
	StatementCycleDays  int
	RiskCategory        string
	WarningThresholdPct string
	Hold                bool
	HoldReason          *string
	ReviewDate          *time.Time
	UpdatedAt           time.Time
}

type CreditProfileInput struct {
	PaymentTermsDays    *int
	GraceDays           *int
	StatementCycleDays  *int
	RiskCategory        string
	WarningThresholdPct *string
	ReviewDate          *time.Time
}

// CreditPosition is the computed real-time credit standing for a customer.
type CreditPosition struct {
	CustomerID          uuid.UUID
	CreditLimit         string
	Exposure            string // AR balance + active authorization holds
	Available           string // limit - exposure
	Overdue             string // outstanding on past-due invoices
	Status              string // customer account status
	Hold                bool   // profile hold OR on_hold/suspended account
	HoldReason          *string
	WarningThresholdPct string
	OverLimit           bool
	Warning             bool
}

const creditProfileColumns = `
    id, tenant_id, customer_id, payment_terms_days, grace_days, statement_cycle_days,
    risk_category, warning_threshold_pct::text, hold, hold_reason, review_date, updated_at
`

func scanCreditProfile(row pgx.Row, p *CreditProfile) error {
	return row.Scan(
		&p.ID, &p.TenantID, &p.CustomerID, &p.PaymentTermsDays, &p.GraceDays, &p.StatementCycleDays,
		&p.RiskCategory, &p.WarningThresholdPct, &p.Hold, &p.HoldReason, &p.ReviewDate, &p.UpdatedAt,
	)
}

// UpsertCreditProfile creates or updates a customer's credit profile.
func (r *Repo) UpsertCreditProfile(ctx context.Context, tx pgx.Tx, tenantID, customerID uuid.UUID, in CreditProfileInput) (*CreditProfile, error) {
	var p CreditProfile
	err := scanCreditProfile(tx.QueryRow(ctx, `
		INSERT INTO customer_credit_profiles
		    (tenant_id, customer_id, payment_terms_days, grace_days, statement_cycle_days, risk_category, warning_threshold_pct, review_date)
		VALUES ($1, $2, COALESCE($3, 0), COALESCE($4, 0), COALESCE($5, 30),
		        COALESCE(NULLIF($6, ''), 'standard'), COALESCE($7::numeric, 80), $8)
		ON CONFLICT (tenant_id, customer_id) DO UPDATE SET
		    payment_terms_days    = COALESCE($3, customer_credit_profiles.payment_terms_days),
		    grace_days            = COALESCE($4, customer_credit_profiles.grace_days),
		    statement_cycle_days  = COALESCE($5, customer_credit_profiles.statement_cycle_days),
		    risk_category         = COALESCE(NULLIF($6, ''), customer_credit_profiles.risk_category),
		    warning_threshold_pct = COALESCE($7::numeric, customer_credit_profiles.warning_threshold_pct),
		    review_date           = COALESCE($8, customer_credit_profiles.review_date)
		RETURNING `+creditProfileColumns,
		tenantID, customerID, in.PaymentTermsDays, in.GraceDays, in.StatementCycleDays,
		in.RiskCategory, in.WarningThresholdPct, in.ReviewDate,
	), &p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repo) GetCreditProfile(ctx context.Context, tenantID, customerID uuid.UUID) (*CreditProfile, error) {
	var p CreditProfile
	err := scanCreditProfile(r.pool.QueryRow(ctx, `SELECT `+creditProfileColumns+` FROM customer_credit_profiles WHERE tenant_id = $1 AND customer_id = $2`, tenantID, customerID), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SetHold applies or releases a manual credit hold (profile is created if
// absent).
func (r *Repo) SetHold(ctx context.Context, tx pgx.Tx, tenantID, customerID uuid.UUID, hold bool, reason *string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO customer_credit_profiles (tenant_id, customer_id, hold, hold_reason)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, customer_id) DO UPDATE SET hold = $3, hold_reason = $4
	`, tenantID, customerID, hold, reason)
	return err
}

// CreditPosition computes a customer's real-time credit standing from their
// limit, the Phase-7 AR balance, active fuel-authorization holds, and past-due
// invoices.
func (r *Repo) CreditPosition(ctx context.Context, q database.Querier, tenantID, customerID uuid.UUID) (*CreditPosition, error) {
	var p CreditPosition
	p.CustomerID = customerID
	// exposure = AR balance + active fuel-authorization holds. The authorization
	// hold component is folded in by AuthorizationHeld once Phase-8 Stage 8
	// creates fuel_authorizations; this base query uses the AR balance.
	err := q.QueryRow(ctx, `
		WITH ar AS (
		    SELECT COALESCE(SUM(amount), 0) AS bal FROM ar_entries WHERE tenant_id = $1 AND customer_id = $2
		),
		overdue AS (
		    SELECT COALESCE(SUM(outstanding_amount), 0) AS od FROM customer_invoices
		    WHERE tenant_id = $1 AND customer_id = $2 AND status IN ('issued', 'partially_paid') AND due_date < CURRENT_DATE
		)
		SELECT c.credit_limit::text,
		       ar.bal::text,
		       (c.credit_limit - ar.bal)::text,
		       overdue.od::text,
		       c.status,
		       (COALESCE(p.hold, false) OR c.status IN ('on_hold', 'suspended')),
		       p.hold_reason,
		       COALESCE(p.warning_threshold_pct, 80)::text,
		       ar.bal > c.credit_limit,
		       c.credit_limit > 0 AND ar.bal >= c.credit_limit * COALESCE(p.warning_threshold_pct, 80) / 100
		FROM customers c
		CROSS JOIN ar CROSS JOIN overdue
		LEFT JOIN customer_credit_profiles p ON p.tenant_id = c.tenant_id AND p.customer_id = c.id
		WHERE c.tenant_id = $1 AND c.id = $2
	`, tenantID, customerID).Scan(
		&p.CreditLimit, &p.Exposure, &p.Available, &p.Overdue, &p.Status,
		&p.Hold, &p.HoldReason, &p.WarningThresholdPct, &p.OverLimit, &p.Warning,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}
