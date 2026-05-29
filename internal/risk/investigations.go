package risk

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// caseAllowedFrom / actionAllowedFrom are the investigation transition tables
// (RISK-004): for each target status, the statuses it may be reached from.
// Creation states ('open' for cases, 'suggested' for actions) are not targets;
// terminal states ('closed', 'completed', 'dismissed') never appear as a
// from-state, so a closed case or finished action cannot be silently reopened.
var caseAllowedFrom = map[string][]string{
	"assigned":        {"open", "in_review", "action_required"},
	"in_review":       {"open", "assigned", "action_required"},
	"action_required": {"assigned", "in_review"},
	"resolved":        {"open", "assigned", "in_review", "action_required"},
	"closed":          {"resolved"},
}

var actionAllowedFrom = map[string][]string{
	"accepted":  {"suggested"},
	"completed": {"suggested", "accepted"},
	"dismissed": {"suggested", "accepted"},
}

type Case struct {
	ID         uuid.UUID
	Title      string
	CaseType   string
	Status     string
	Severity   string
	AssignedTo *uuid.UUID
	Resolution *string
	CreatedAt  time.Time
}

const caseColumns = `id, title, case_type, status, severity, assigned_to, resolution, created_at`

func scanCase(row pgx.Row, c *Case) error {
	return row.Scan(&c.ID, &c.Title, &c.CaseType, &c.Status, &c.Severity, &c.AssignedTo, &c.Resolution, &c.CreatedAt)
}

func (r *Repo) CreateCase(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, title, caseType, severity string, openedBy uuid.UUID) (*Case, error) {
	var c Case
	if err := scanCase(tx.QueryRow(ctx, `
		INSERT INTO investigation_cases (tenant_id, title, case_type, severity, opened_by)
		VALUES ($1, $2, COALESCE(NULLIF($3,''),'other'), COALESCE(NULLIF($4,''),'medium'), $5)
		RETURNING `+caseColumns,
		tenantID, title, caseType, severity, openedBy,
	), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) ListCases(ctx context.Context, tenantID uuid.UUID, status string) ([]Case, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+caseColumns+` FROM investigation_cases
		WHERE tenant_id = $1 AND ($2 = '' OR status = $2) ORDER BY created_at DESC
	`, tenantID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Case{}
	for rows.Next() {
		var c Case
		if err := scanCase(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repo) GetCase(ctx context.Context, tenantID, id uuid.UUID) (*Case, error) {
	var c Case
	err := scanCase(r.pool.QueryRow(ctx, `SELECT `+caseColumns+` FROM investigation_cases WHERE tenant_id = $1 AND id = $2`, tenantID, id), &c)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// AttachAlert links a risk alert to a case (idempotent) and marks the alert
// escalated.
func (r *Repo) AttachAlert(ctx context.Context, tx pgx.Tx, tenantID, caseID, alertID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO investigation_case_alerts (tenant_id, case_id, alert_id) VALUES ($1, $2, $3)
		ON CONFLICT (tenant_id, case_id, alert_id) DO NOTHING
	`, tenantID, caseID, alertID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE risk_alerts SET status = 'escalated' WHERE tenant_id = $1 AND id = $2 AND status IN ('open','acknowledged','investigating')`, tenantID, alertID)
	return err
}

func (r *Repo) AddComment(ctx context.Context, tx pgx.Tx, tenantID, caseID uuid.UUID, body string, authorID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO investigation_case_comments (tenant_id, case_id, body, author_id) VALUES ($1, $2, $3, $4) RETURNING id`, tenantID, caseID, body, authorID).Scan(&id)
	return id, err
}

func (r *Repo) AddAction(ctx context.Context, tx pgx.Tx, tenantID, caseID uuid.UUID, actionType, detail string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO investigation_case_actions (tenant_id, case_id, action_type, detail) VALUES ($1, $2, $3, $4) RETURNING id`, tenantID, caseID, actionType, detail).Scan(&id)
	return id, err
}

func (r *Repo) SetActionStatus(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, status string) error {
	allowedFrom, ok := actionAllowedFrom[status]
	if !ok {
		return ErrBadState
	}
	tag, err := tx.Exec(ctx,
		`UPDATE investigation_case_actions SET status = $3 WHERE tenant_id = $1 AND id = $2 AND status = ANY($4::text[])`,
		tenantID, id, status, allowedFrom)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Disambiguate missing action from an illegal transition.
		var cur string
		switch qErr := tx.QueryRow(ctx, `SELECT status FROM investigation_case_actions WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&cur); {
		case errors.Is(qErr, pgx.ErrNoRows):
			return ErrNotFound
		case qErr != nil:
			return qErr
		default:
			return ErrBadState
		}
	}
	return nil
}

// SetCaseStatus transitions a case and records a resolution on close.
func (r *Repo) SetCaseStatus(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, status string, resolution *string, assignedTo *uuid.UUID) (*Case, error) {
	allowedFrom, ok := caseAllowedFrom[status]
	if !ok {
		return nil, ErrBadState
	}
	var c Case
	err := scanCase(tx.QueryRow(ctx, `
		UPDATE investigation_cases SET status = $3, resolution = COALESCE($4, resolution), assigned_to = COALESCE($5, assigned_to)
		WHERE tenant_id = $1 AND id = $2 AND status = ANY($6::text[]) RETURNING `+caseColumns,
		tenantID, id, status, resolution, assignedTo, allowedFrom,
	), &c)
	if errors.Is(err, pgx.ErrNoRows) {
		// Disambiguate missing case from an illegal transition.
		var cur string
		switch qErr := tx.QueryRow(ctx, `SELECT status FROM investigation_cases WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&cur); {
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
	return &c, nil
}

// CaseTimeline returns the case-scoped events (linked alerts, comments,
// actions) in chronological order — the investigator's reconstruction.
func (r *Repo) CaseTimeline(ctx context.Context, tenantID, caseID uuid.UUID) ([]map[string]any, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT kind, detail, at FROM (
		    SELECT 'alert_linked' AS kind, a.alert_type || ' (' || a.severity || ')' AS detail, ica.created_at AS at
		    FROM investigation_case_alerts ica JOIN risk_alerts a ON a.id = ica.alert_id AND a.tenant_id = ica.tenant_id
		    WHERE ica.tenant_id = $1 AND ica.case_id = $2
		    UNION ALL
		    SELECT 'comment', body, created_at FROM investigation_case_comments WHERE tenant_id = $1 AND case_id = $2
		    UNION ALL
		    SELECT 'action:' || status, action_type || COALESCE(' — ' || detail, ''), created_at FROM investigation_case_actions WHERE tenant_id = $1 AND case_id = $2
		) t ORDER BY at
	`, tenantID, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var kind, detail string
		var at time.Time
		if err := rows.Scan(&kind, &detail, &at); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"kind": kind, "detail": detail, "at": at.Format(time.RFC3339)})
	}
	return out, rows.Err()
}
