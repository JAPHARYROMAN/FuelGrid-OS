package risk

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

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

// RunDetection executes the built-in detection packs, raising idempotent alerts
// linked to immutable source facts. Returns the number of new alerts.
func (r *Repo) RunDetection(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (int, error) {
	created := 0
	packs := []struct {
		sql string
	}{
		// Fuel loss: tank reconciliations flagged exception (variance over tolerance).
		{`INSERT INTO risk_alerts (tenant_id, rule_code, alert_type, severity, station_id, subject_type, subject_id, detail, score)
		  SELECT tr.tenant_id, 'fuel_loss', 'fuel_loss', 'high', t.station_id, 'tank_reconciliation', tr.id,
		         'stock variance over tolerance: ' || tr.variance_litres || ' L', 70
		  FROM tank_reconciliations tr JOIN tanks t ON t.id = tr.tank_id AND t.tenant_id = tr.tenant_id
		  WHERE tr.tenant_id = $1 AND tr.status = 'exception'
		  ON CONFLICT (tenant_id, alert_type, subject_id) WHERE status IN ('open','acknowledged','investigating','escalated') DO NOTHING`},
		// Cash shortage: posted cash reconciliations with a negative variance.
		{`INSERT INTO risk_alerts (tenant_id, rule_code, alert_type, severity, station_id, subject_type, subject_id, detail, amount, score)
		  SELECT tenant_id, 'cash_shortage', 'cash_shortage', 'medium', station_id, 'cash_reconciliation', id,
		         'counted cash short by ' || abs(variance), abs(variance), 55
		  FROM cash_reconciliations WHERE tenant_id = $1 AND status = 'posted' AND variance < 0
		  ON CONFLICT (tenant_id, alert_type, subject_id) WHERE status IN ('open','acknowledged','investigating','escalated') DO NOTHING`},
		// Delivery discrepancy: open procurement discrepancies.
		{`INSERT INTO risk_alerts (tenant_id, rule_code, alert_type, severity, subject_type, subject_id, detail, amount, score)
		  SELECT tenant_id, 'delivery_discrepancy', 'delivery_discrepancy', 'medium', 'procurement_discrepancy', id,
		         'open procurement discrepancy', variance_amount, 50
		  FROM procurement_discrepancies WHERE tenant_id = $1 AND status = 'open'
		  ON CONFLICT (tenant_id, alert_type, subject_id) WHERE status IN ('open','acknowledged','investigating','escalated') DO NOTHING`},
	}
	for _, p := range packs {
		tag, err := tx.Exec(ctx, p.sql, tenantID)
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
	var a Alert
	err := scanAlert(tx.QueryRow(ctx, `
		UPDATE risk_alerts SET status = $3, disposition = COALESCE($4, disposition), assigned_to = COALESCE($5, assigned_to)
		WHERE tenant_id = $1 AND id = $2
		RETURNING `+alertColumns,
		tenantID, id, to, disposition, assignedTo,
	), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
