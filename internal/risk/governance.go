package risk

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TuneRule updates a rule's threshold, lookback window, and severity.
func (r *Repo) TuneRule(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, threshold string, lookbackDays int, severity string) error {
	tag, err := tx.Exec(ctx, `
		UPDATE risk_rules SET
		    threshold = COALESCE($3::numeric, threshold),
		    lookback_days = CASE WHEN $4 > 0 THEN $4 ELSE lookback_days END,
		    severity = COALESCE(NULLIF($5,''), severity)
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id, nullableMoney(threshold), lookbackDays, severity)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) CreateSuppression(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, alertType string, entityID *uuid.UUID, reason string, expiresAt *time.Time, createdBy uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO risk_suppressions (tenant_id, alert_type, entity_id, reason, expires_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
	`, tenantID, alertType, entityID, reason, expiresAt, createdBy).Scan(&id)
	return id, err
}

func (r *Repo) ListSuppressions(ctx context.Context, tenantID uuid.UUID) ([]map[string]any, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, alert_type, entity_id, reason, expires_at FROM risk_suppressions
		WHERE tenant_id = $1 ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var at string
		var entity *uuid.UUID
		var reason string
		var expires *time.Time
		if err := rows.Scan(&id, &at, &entity, &reason, &expires); err != nil {
			return nil, err
		}
		var exp *string
		if expires != nil {
			v := expires.Format(time.RFC3339)
			exp = &v
		}
		out = append(out, map[string]any{"id": id, "alert_type": at, "entity_id": entity, "reason": reason, "expires_at": exp})
	}
	return out, rows.Err()
}

// ListSuppressionsPage is the paginated variant of ListSuppressions (REL-REPO).
// created_at is not unique, so id is appended as a deterministic tiebreaker.
func (r *Repo) ListSuppressionsPage(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]map[string]any, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, alert_type, entity_id, reason, expires_at FROM risk_suppressions
		WHERE tenant_id = $1
		ORDER BY created_at DESC, id
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var at string
		var entity *uuid.UUID
		var reason string
		var expires *time.Time
		if err := rows.Scan(&id, &at, &entity, &reason, &expires); err != nil {
			return nil, err
		}
		var exp *string
		if expires != nil {
			v := expires.Format(time.RFC3339)
			exp = &v
		}
		out = append(out, map[string]any{"id": id, "alert_type": at, "entity_id": entity, "reason": reason, "expires_at": exp})
	}
	return out, rows.Err()
}

func (r *Repo) RecordFeedback(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, alertID *uuid.UUID, disposition, note string, createdBy uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO risk_feedback (tenant_id, alert_id, disposition, note, created_by)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, tenantID, alertID, disposition, note, createdBy).Scan(&id)
	return id, err
}

// PauseAllRules pauses every active rule — the incident-response kill switch.
func (r *Repo) PauseAllRules(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (int, error) {
	tag, err := tx.Exec(ctx, `UPDATE risk_rules SET status = 'paused' WHERE tenant_id = $1 AND status = 'active'`, tenantID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// GovernanceSummary reports rule/alert health and data-quality indicators.
func (r *Repo) GovernanceSummary(ctx context.Context, tenantID uuid.UUID) (map[string]any, error) {
	out := map[string]any{}
	var activeRules, pausedRules int
	if err := r.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status='active'), count(*) FILTER (WHERE status='paused') FROM risk_rules WHERE tenant_id = $1
	`, tenantID).Scan(&activeRules, &pausedRules); err != nil {
		return nil, err
	}
	var openAlerts, resolvedAlerts, dismissedAlerts int
	if err := r.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status IN ('open','acknowledged','investigating','escalated')),
		       count(*) FILTER (WHERE status='resolved'),
		       count(*) FILTER (WHERE status='dismissed')
		FROM risk_alerts WHERE tenant_id = $1
	`, tenantID).Scan(&openAlerts, &resolvedAlerts, &dismissedAlerts); err != nil {
		return nil, err
	}
	var signals, suppressions int
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM risk_signals WHERE tenant_id = $1`, tenantID).Scan(&signals)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM risk_suppressions WHERE tenant_id = $1 AND (expires_at IS NULL OR expires_at > now())`, tenantID).Scan(&suppressions)

	closed := resolvedAlerts + dismissedAlerts
	dismissalRate := 0.0
	if closed > 0 {
		dismissalRate = float64(dismissedAlerts) / float64(closed)
	}
	out["active_rules"] = activeRules
	out["paused_rules"] = pausedRules
	out["open_alerts"] = openAlerts
	out["resolved_alerts"] = resolvedAlerts
	out["dismissed_alerts"] = dismissedAlerts
	out["dismissal_rate"] = dismissalRate
	out["signals"] = signals
	out["active_suppressions"] = suppressions
	return out, nil
}
