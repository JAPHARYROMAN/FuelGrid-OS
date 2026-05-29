package risk

import (
	"context"
	"errors"
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
	ID          uuid.UUID
	RuleCode    *string
	AlertType   string
	Severity    string
	Status      string
	StationID   *uuid.UUID
	SubjectType *string
	SubjectID   *uuid.UUID
	Detail      *string
	Amount      *string
	Score       int
	CreatedAt   time.Time
}

const alertColumns = `
    id, rule_code, alert_type, severity, status, station_id, subject_type, subject_id, detail, amount::text, score, created_at
`

func scanAlert(row pgx.Row, a *Alert) error {
	return row.Scan(&a.ID, &a.RuleCode, &a.AlertType, &a.Severity, &a.Status, &a.StationID,
		&a.SubjectType, &a.SubjectID, &a.Detail, &a.Amount, &a.Score, &a.CreatedAt)
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

type ruleConfig struct {
	status       string
	severity     string
	threshold    string // "" when the rule's threshold is NULL
	lookbackDays int
}

// loadRuleConfigs reads the configured rules for the given codes in one query.
func (r *Repo) loadRuleConfigs(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, codes []string) (map[string]ruleConfig, error) {
	rows, err := tx.Query(ctx, `
		SELECT code, status, severity, COALESCE(threshold::text, ''), lookback_days
		FROM risk_rules WHERE tenant_id = $1 AND code = ANY($2)
	`, tenantID, codes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ruleConfig{}
	for rows.Next() {
		var code string
		var c ruleConfig
		if err := rows.Scan(&code, &c.status, &c.severity, &c.threshold, &c.lookbackDays); err != nil {
			return nil, err
		}
		out[code] = c
	}
	return out, rows.Err()
}

// Detection packs. Each raises idempotent alerts linked to immutable source
// facts. Bind args: $1 tenant, $2 severity, $3 score, $4 threshold (” => no
// magnitude floor), and (where the source table has a timestamp) $5
// lookback_days (<= 0 => no time bound).
const fuelLossPack = `
	INSERT INTO risk_alerts (tenant_id, rule_code, alert_type, severity, station_id, subject_type, subject_id, detail, score)
	SELECT tr.tenant_id, 'fuel_loss', 'fuel_loss', $2, t.station_id, 'tank_reconciliation', tr.id,
	       'stock variance over tolerance: ' || tr.variance_litres || ' L', $3
	FROM tank_reconciliations tr JOIN tanks t ON t.id = tr.tank_id AND t.tenant_id = tr.tenant_id
	WHERE tr.tenant_id = $1 AND tr.status = 'exception'
	  AND abs(tr.variance_litres) >= COALESCE(NULLIF($4, '')::numeric, 0)
	  AND ($5 <= 0 OR tr.created_at >= now() - make_interval(days => $5))
	  AND NOT EXISTS (SELECT 1 FROM risk_suppressions sup WHERE sup.tenant_id = tr.tenant_id AND sup.alert_type = 'fuel_loss'
	                  AND (sup.expires_at IS NULL OR sup.expires_at > now()) AND (sup.entity_id IS NULL OR sup.entity_id = t.station_id))
	ON CONFLICT (tenant_id, alert_type, subject_id) WHERE status IN ('open','acknowledged','investigating','escalated') DO NOTHING`

const cashShortagePack = `
	INSERT INTO risk_alerts (tenant_id, rule_code, alert_type, severity, station_id, subject_type, subject_id, detail, amount, score)
	SELECT tenant_id, 'cash_shortage', 'cash_shortage', $2, station_id, 'cash_reconciliation', id,
	       'counted cash short by ' || abs(variance), abs(variance), $3
	FROM cash_reconciliations WHERE tenant_id = $1 AND status = 'posted' AND variance < 0
	  AND abs(variance) >= COALESCE(NULLIF($4, '')::numeric, 0)
	  AND ($5 <= 0 OR created_at >= now() - make_interval(days => $5))
	  AND NOT EXISTS (SELECT 1 FROM risk_suppressions sup WHERE sup.tenant_id = cash_reconciliations.tenant_id AND sup.alert_type = 'cash_shortage'
	                  AND (sup.expires_at IS NULL OR sup.expires_at > now()) AND (sup.entity_id IS NULL OR sup.entity_id = cash_reconciliations.station_id))
	ON CONFLICT (tenant_id, alert_type, subject_id) WHERE status IN ('open','acknowledged','investigating','escalated') DO NOTHING`

// procurement_discrepancies has no timestamp column, so this pack honors the
// threshold but not lookback (bind args $1..$4). Wiring a lookback here is a
// tracked follow-up once the table carries a detected/created timestamp.
const deliveryDiscrepancyPack = `
	INSERT INTO risk_alerts (tenant_id, rule_code, alert_type, severity, subject_type, subject_id, detail, amount, score)
	SELECT tenant_id, 'delivery_discrepancy', 'delivery_discrepancy', $2, 'procurement_discrepancy', id,
	       'open procurement discrepancy', variance_amount, $3
	FROM procurement_discrepancies WHERE tenant_id = $1 AND status = 'open'
	  AND abs(variance_amount) >= COALESCE(NULLIF($4, '')::numeric, 0)
	  AND NOT EXISTS (SELECT 1 FROM risk_suppressions sup WHERE sup.tenant_id = procurement_discrepancies.tenant_id AND sup.alert_type = 'delivery_discrepancy'
	                  AND (sup.expires_at IS NULL OR sup.expires_at > now()) AND sup.entity_id IS NULL)
	ON CONFLICT (tenant_id, alert_type, subject_id) WHERE status IN ('open','acknowledged','investigating','escalated') DO NOTHING`

// RunDetection runs the built-in detection packs, now driven by the configured
// rules instead of running unconditionally (audit RISK "pause is a no-op"):
//
//   - a pack whose rule is paused or retired does not run — pause is honored,
//     including the PauseAllRules kill switch;
//   - a pack with a configured rule uses that rule's threshold, severity (and,
//     where the source table supports it, lookback window);
//   - a pack with no configured rule runs with built-in defaults, so detection
//     stays fail-safe (on by default) for tenants that have not tuned it.
//
// Returns the number of new alerts.
func (r *Repo) RunDetection(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (int, error) {
	packs := []struct {
		code        string
		defSeverity string
		sql         string
		lookback    bool
	}{
		{"fuel_loss", "high", fuelLossPack, true},
		{"cash_shortage", "medium", cashShortagePack, true},
		{"delivery_discrepancy", "medium", deliveryDiscrepancyPack, false},
	}
	codes := make([]string, len(packs))
	for i := range packs {
		codes[i] = packs[i].code
	}
	rules, err := r.loadRuleConfigs(ctx, tx, tenantID, codes)
	if err != nil {
		return 0, err
	}

	created := 0
	for _, p := range packs {
		severity, threshold, lookback := p.defSeverity, "", 0
		if cfg, ok := rules[p.code]; ok {
			if cfg.status == "paused" || cfg.status == "retired" {
				continue // honor pause: a disabled rule stops its pack entirely
			}
			if cfg.severity != "" {
				severity = cfg.severity
			}
			threshold, lookback = cfg.threshold, cfg.lookbackDays
		}
		args := []any{tenantID, severity, severityScore(severity), threshold}
		if p.lookback {
			args = append(args, lookback)
		}
		tag, err := tx.Exec(ctx, p.sql, args...)
		if err != nil {
			return 0, err
		}
		created += int(tag.RowsAffected())
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
