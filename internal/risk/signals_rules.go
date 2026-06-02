package risk

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// BackfillSignals derives idempotent risk signals from posted source facts —
// stock-variance reconciliations, cash-variance reconciliations, and
// procurement discrepancies — keyed by source event so replays don't
// duplicate. Returns the number of new signals.
func (r *Repo) BackfillSignals(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (int, error) {
	created := 0
	stmts := []string{
		`INSERT INTO risk_signals (tenant_id, signal_type, source_event_id, station_id, litres, occurred_at)
		 SELECT tr.tenant_id, 'stock_variance', tr.id, t.station_id, tr.variance_litres, tr.created_at
		 FROM tank_reconciliations tr JOIN tanks t ON t.id = tr.tank_id AND t.tenant_id = tr.tenant_id
		 WHERE tr.tenant_id = $1
		 ON CONFLICT (tenant_id, signal_type, source_event_id) DO NOTHING`,
		`INSERT INTO risk_signals (tenant_id, signal_type, source_event_id, station_id, amount, occurred_at)
		 SELECT tenant_id, 'cash_variance', id, station_id, variance, created_at
		 FROM cash_reconciliations WHERE tenant_id = $1
		 ON CONFLICT (tenant_id, signal_type, source_event_id) DO NOTHING`,
		`INSERT INTO risk_signals (tenant_id, signal_type, source_event_id, amount, litres, occurred_at)
		 SELECT tenant_id, 'delivery_discrepancy', id, variance_amount, variance_litres, raised_at
		 FROM procurement_discrepancies WHERE tenant_id = $1
		 ON CONFLICT (tenant_id, signal_type, source_event_id) DO NOTHING`,
	}
	for _, s := range stmts {
		tag, err := tx.Exec(ctx, s, tenantID)
		if err != nil {
			return 0, err
		}
		created += int(tag.RowsAffected())
	}
	return created, nil
}

func (r *Repo) ListSignals(ctx context.Context, tenantID uuid.UUID, signalType string) ([]map[string]any, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, signal_type, source_event_id, station_id, amount::text, litres::text, occurred_at
		FROM risk_signals WHERE tenant_id = $1 AND ($2 = '' OR signal_type = $2)
		ORDER BY occurred_at DESC LIMIT 500
	`, tenantID, signalType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, src uuid.UUID
		var st string
		var station *uuid.UUID
		var amount, litres *string
		var occurred any
		if err := rows.Scan(&id, &st, &src, &station, &amount, &litres, &occurred); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "signal_type": st, "source_event_id": src, "station_id": station, "amount": amount, "litres": litres})
	}
	return out, rows.Err()
}

// ListSignalsPage is the paginated variant of ListSignals (REL-REPO). occurred_at
// is not unique, so id is appended as a deterministic tiebreaker; the prior
// hard LIMIT 500 is replaced by the caller-supplied limit/offset window.
func (r *Repo) ListSignalsPage(ctx context.Context, tenantID uuid.UUID, signalType string, limit, offset int) ([]map[string]any, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, signal_type, source_event_id, station_id, amount::text, litres::text, occurred_at
		FROM risk_signals WHERE tenant_id = $1 AND ($2 = '' OR signal_type = $2)
		ORDER BY occurred_at DESC, id
		LIMIT $3 OFFSET $4
	`, tenantID, signalType, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, src uuid.UUID
		var st string
		var station *uuid.UUID
		var amount, litres *string
		var occurred any
		if err := rows.Scan(&id, &st, &src, &station, &amount, &litres, &occurred); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "signal_type": st, "source_event_id": src, "station_id": station, "amount": amount, "litres": litres})
	}
	return out, rows.Err()
}

// ---- Rules ----

// RuleInput is the configurable surface for creating or fully updating a rule in
// the Rules & Insights Engine. Threshold is a decimal string ("" => NULL) so it
// stays off the float path; ComparisonPeriodDays <= 0 leaves the column NULL.
type RuleInput struct {
	Code                 string
	Name                 string
	RuleType             string
	Category             string
	Condition            string
	Severity             string
	Description          string
	MessageTemplate      string
	RecommendedAction    string
	Threshold            string
	LookbackDays         int
	ComparisonPeriodDays int
	Status               string
	Enabled              *bool
}

func nullableDays(d int) *int {
	if d <= 0 {
		return nil
	}
	return &d
}

func (r *Repo) CreateRule(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in RuleInput) (uuid.UUID, error) {
	lookback := in.LookbackDays
	if lookback <= 0 {
		lookback = 30
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO risk_rules
		    (tenant_id, code, name, rule_type, status, category, condition,
		     severity, description, message_template, recommended_action,
		     threshold, lookback_days, comparison_period_days, enabled)
		VALUES ($1, $2, $3, COALESCE(NULLIF($4,''),'threshold'),
		        COALESCE(NULLIF($5,''),'draft'), COALESCE(NULLIF($6,''),'general'),
		        NULLIF($7,''), COALESCE(NULLIF($8,''),'medium'), $9,
		        NULLIF($10,''), NULLIF($11,''), $12::numeric, $13, $14, $15)
		RETURNING id
	`, tenantID, in.Code, in.Name, in.RuleType, in.Status, in.Category, in.Condition,
		in.Severity, in.Description, in.MessageTemplate, in.RecommendedAction,
		nullableMoney(in.Threshold), lookback, nullableDays(in.ComparisonPeriodDays), enabled).Scan(&id)
	return id, err
}

// UpdateRule fully updates a rule's configurable fields (RISK / Workstream D).
func (r *Repo) UpdateRule(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, in RuleInput) error {
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	tag, err := tx.Exec(ctx, `
		UPDATE risk_rules SET
		    name = COALESCE(NULLIF($3,''), name),
		    rule_type = COALESCE(NULLIF($4,''), rule_type),
		    status = COALESCE(NULLIF($5,''), status),
		    category = COALESCE(NULLIF($6,''), category),
		    condition = COALESCE(NULLIF($7,''), condition),
		    severity = COALESCE(NULLIF($8,''), severity),
		    description = $9,
		    message_template = COALESCE(NULLIF($10,''), message_template),
		    recommended_action = COALESCE(NULLIF($11,''), recommended_action),
		    threshold = $12::numeric,
		    comparison_period_days = COALESCE($13, comparison_period_days),
		    enabled = $14
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id, in.Name, in.RuleType, in.Status, in.Category, in.Condition,
		in.Severity, in.Description, in.MessageTemplate, in.RecommendedAction,
		nullableMoney(in.Threshold), nullableDays(in.ComparisonPeriodDays), enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetRuleEnabled flips a rule's enabled flag — the per-rule kill switch that
// makes disabling a rule actually stop its alerts (RISK-002).
func (r *Repo) SetRuleEnabled(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, enabled bool) error {
	tag, err := tx.Exec(ctx, `UPDATE risk_rules SET enabled = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, id, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const ruleColumns = `
	id, code, name, rule_type, status, category, condition, threshold::text,
	lookback_days, comparison_period_days, severity, description,
	message_template, recommended_action, enabled
`

func scanRule(rows pgx.Rows) (map[string]any, error) {
	var id uuid.UUID
	var code, name, rt, status, category, severity string
	var condition, threshold, desc, msgTmpl, recAction *string
	var lookback int
	var compPeriod *int
	var enabled bool
	if err := rows.Scan(&id, &code, &name, &rt, &status, &category, &condition, &threshold,
		&lookback, &compPeriod, &severity, &desc, &msgTmpl, &recAction, &enabled); err != nil {
		return nil, err
	}
	return map[string]any{
		"id": id, "code": code, "name": name, "rule_type": rt, "status": status,
		"category": category, "condition": condition, "threshold": threshold,
		"lookback_days": lookback, "comparison_period_days": compPeriod,
		"severity": severity, "description": desc, "message_template": msgTmpl,
		"recommended_action": recAction, "enabled": enabled,
	}, nil
}

func (r *Repo) ListRules(ctx context.Context, tenantID uuid.UUID) ([]map[string]any, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+ruleColumns+` FROM risk_rules WHERE tenant_id = $1 ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		m, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListRulesPage is the paginated variant of ListRules (REL-REPO). code is unique
// per tenant, so it alone is a stable ordering key.
func (r *Repo) ListRulesPage(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]map[string]any, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+ruleColumns+` FROM risk_rules WHERE tenant_id = $1 ORDER BY code LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		m, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetRuleStatus transitions a rule (draft/active/paused/retired).
func (r *Repo) SetRuleStatus(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, status string) error {
	tag, err := tx.Exec(ctx, `UPDATE risk_rules SET status = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
