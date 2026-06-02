package risk

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// alertAllowedFrom is the alert-status transition table: for each target
// status, the set of statuses it may be reached from (RISK-003). 'open' is the
// creation state, not a transition target; 'resolved' and 'dismissed' are
// terminal — they appear only as targets, never as a from-state, so an alert
// cannot be reopened or have its disposition overwritten.
var alertAllowedFrom = map[string][]string{
	"acknowledged":  {"open"},
	"investigating": {"open", "acknowledged", "escalated"},
	"escalated":     {"open", "acknowledged", "investigating"},
	"resolved":      {"open", "acknowledged", "investigating", "escalated"},
	"dismissed":     {"open", "acknowledged", "investigating", "escalated"},
}

type Alert struct {
	ID                uuid.UUID
	RuleCode          *string
	RuleID            *uuid.UUID
	AlertType         string
	Severity          string
	Status            string
	StationID         *uuid.UUID
	SubjectType       *string
	SubjectID         *uuid.UUID
	Detail            *string
	Amount            *string
	RecommendedAction *string
	Score             int
	CreatedAt         time.Time
}

const alertColumns = `
    id, rule_code, rule_id, alert_type, severity, status, station_id, subject_type, subject_id, detail, amount::text, recommended_action, score, created_at
`

func scanAlert(row pgx.Row, a *Alert) error {
	return row.Scan(&a.ID, &a.RuleCode, &a.RuleID, &a.AlertType, &a.Severity, &a.Status, &a.StationID,
		&a.SubjectType, &a.SubjectID, &a.Detail, &a.Amount, &a.RecommendedAction, &a.Score, &a.CreatedAt)
}

// severityScore maps a configured severity to the alert score used for
// ranking. Tuning a rule's severity therefore moves both the alert severity
// and its score together.
func severityScore(sev string) int {
	switch sev {
	case "critical":
		return 90
	case "high":
		return 75
	case "low":
		return 40
	case "info":
		return 20
	default: // medium
		return 55
	}
}

// enabledRule is one configured rule loaded for a detection run. condition
// names the evaluator; the rest is the operator-configurable surface.
type enabledRule struct {
	id                   uuid.UUID
	code                 string
	condition            string
	severity             string
	threshold            string // "" when NULL
	comparisonPeriodDays int    // 0 => evaluator default
	messageTemplate      string
	recommendedAction    string
}

// loadEnabledRules returns the rules eligible to run for a detection pass:
// enabled = true AND status not paused/retired. This is what makes
// disabling/pausing a rule actually stop its alerts (fixes RISK-002).
func (r *Repo) loadEnabledRules(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) ([]enabledRule, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, code, COALESCE(condition, ''), severity,
		       COALESCE(threshold::text, ''),
		       COALESCE(comparison_period_days, lookback_days, 0),
		       COALESCE(message_template, ''), COALESCE(recommended_action, '')
		FROM risk_rules
		WHERE tenant_id = $1 AND enabled = true AND status NOT IN ('paused', 'retired')
		ORDER BY code
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []enabledRule
	for rows.Next() {
		var e enabledRule
		if err := rows.Scan(&e.id, &e.code, &e.condition, &e.severity, &e.threshold,
			&e.comparisonPeriodDays, &e.messageTemplate, &e.recommendedAction); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// suppressed reports whether an open alert for (alertType, station) is currently
// suppressed. A suppression with NULL entity_id covers the whole type; one with
// an entity_id covers that station only. Mirrors the pre-existing pack logic.
func (r *Repo) suppressed(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, alertType string, station *uuid.UUID) (bool, error) {
	var n int
	err := tx.QueryRow(ctx, `
		SELECT count(*) FROM risk_suppressions sup
		WHERE sup.tenant_id = $1 AND sup.alert_type = $2
		  AND (sup.expires_at IS NULL OR sup.expires_at > now())
		  AND (sup.entity_id IS NULL OR sup.entity_id = $3)
	`, tenantID, alertType, station).Scan(&n)
	return n > 0, err
}

// upsertAlert inserts a rendered alert idempotently. The open-alert unique index
// (tenant_id, alert_type, subject_id) keeps a single open alert per subject, so
// re-running detection while an alert is open is a no-op. alert_type is the
// rule's condition key; subject_id is the candidate's dedupe entity.
func (r *Repo) upsertAlert(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, rule enabledRule, c Candidate, detail string) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO risk_alerts
		    (tenant_id, rule_code, rule_id, alert_type, severity, station_id,
		     subject_type, subject_id, detail, amount, recommended_action, score)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10,'')::numeric, NULLIF($11,''), $12)
		ON CONFLICT (tenant_id, alert_type, subject_id)
		    WHERE status IN ('open','acknowledged','investigating','escalated')
		    DO NOTHING
	`, tenantID, rule.code, rule.id, rule.condition, rule.severity, c.StationID,
		c.EntityType, c.EntityID, detail, c.Amount, rule.recommendedAction, severityScore(rule.severity))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// RunDetection drives the configurable Rules & Insights Engine. For each ENABLED
// rule (enabled=true AND status not paused/retired) it looks up the evaluator
// named by the rule's `condition`, runs it, renders each candidate's
// message_template into the alert detail, attaches recommended_action, rule_id
// and the rule's severity, honors active suppressions, and upserts the alert
// idempotently. An unknown condition is skipped (logged). Disabling or pausing a
// rule now actually stops its alerts (RISK-001/RISK-002). Returns new alert
// count; signature is unchanged so the handler is unaffected.
func (r *Repo) RunDetection(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (int, error) {
	rules, err := r.loadEnabledRules(ctx, tx, tenantID)
	if err != nil {
		return 0, err
	}
	asOf := time.Now()
	created := 0
	for _, rule := range rules {
		eval, ok := EvaluatorFor(rule.condition)
		if !ok {
			slog.WarnContext(ctx, "risk: skipping rule with unknown condition",
				"tenant_id", tenantID, "code", rule.code, "condition", rule.condition)
			continue
		}
		cands, err := eval(ctx, tx, tenantID, asOf, RuleConfig{
			Threshold:            rule.threshold,
			ComparisonPeriodDays: rule.comparisonPeriodDays,
			Severity:             rule.severity,
		})
		if err != nil {
			return 0, err
		}
		for _, c := range cands {
			supp, err := r.suppressed(ctx, tx, tenantID, rule.condition, c.StationID)
			if err != nil {
				return 0, err
			}
			if supp {
				continue
			}
			detail := renderTemplate(rule.messageTemplate, c.Vars)
			ins, err := r.upsertAlert(ctx, tx, tenantID, rule, c, detail)
			if err != nil {
				return 0, err
			}
			if ins {
				created++
			}
		}
	}
	return created, nil
}

func (r *Repo) ListAlerts(ctx context.Context, tenantID uuid.UUID, status, alertType string) ([]Alert, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+alertColumns+` FROM risk_alerts
		WHERE tenant_id = $1 AND ($2 = '' OR status = $2) AND ($3 = '' OR alert_type = $3)
		ORDER BY score DESC, created_at DESC
	`, tenantID, status, alertType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		var a Alert
		if err := scanAlert(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListAlertsPage is the paginated variant of ListAlerts (REL-REPO). The score
// and created_at ordering keys are not unique, so id is appended as a
// deterministic tiebreaker for stable paging.
func (r *Repo) ListAlertsPage(ctx context.Context, tenantID uuid.UUID, status, alertType string, limit, offset int) ([]Alert, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+alertColumns+` FROM risk_alerts
		WHERE tenant_id = $1 AND ($2 = '' OR status = $2) AND ($3 = '' OR alert_type = $3)
		ORDER BY score DESC, created_at DESC, id
		LIMIT $4 OFFSET $5
	`, tenantID, status, alertType, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		var a Alert
		if err := scanAlert(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repo) GetAlert(ctx context.Context, tenantID, id uuid.UUID) (*Alert, error) {
	var a Alert
	err := scanAlert(r.pool.QueryRow(ctx, `SELECT `+alertColumns+` FROM risk_alerts WHERE tenant_id = $1 AND id = $2`, tenantID, id), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// TransitionAlert moves an alert through its lifecycle and records a disposition
// on close. Returns the updated alert.
func (r *Repo) TransitionAlert(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, to string, disposition *string, assignedTo *uuid.UUID) (*Alert, error) {
	allowedFrom, ok := alertAllowedFrom[to]
	if !ok {
		// Unknown target, or one that is never a transition target (e.g. "open").
		return nil, ErrBadState
	}
	if (to == "resolved" || to == "dismissed") && (disposition == nil || strings.TrimSpace(*disposition) == "") {
		return nil, ErrDispositionRequired
	}
	var a Alert
	err := scanAlert(tx.QueryRow(ctx, `
		UPDATE risk_alerts SET status = $3, disposition = COALESCE($4, disposition), assigned_to = COALESCE($5, assigned_to)
		WHERE tenant_id = $1 AND id = $2 AND status = ANY($6::text[])
		RETURNING `+alertColumns,
		tenantID, id, to, disposition, assignedTo, allowedFrom,
	), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		// No row updated: either the alert doesn't exist (ErrNotFound) or it's
		// in a status from which `to` is not reachable (ErrBadState).
		var cur string
		switch qErr := tx.QueryRow(ctx, `SELECT status FROM risk_alerts WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&cur); {
		case errors.Is(qErr, pgx.ErrNoRows):
			return nil, ErrNotFound
		case qErr != nil:
			return nil, qErr
		default:
			return nil, ErrBadState
		}
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
