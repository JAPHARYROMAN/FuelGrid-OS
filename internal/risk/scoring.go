package risk

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RecomputeStationScores rebuilds station risk scores from open alerts
// (severity-weighted), storing a component breakdown and a band. Returns the
// number of scored stations.
func (r *Repo) RecomputeStationScores(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (int, error) {
	if _, err := tx.Exec(ctx, `
		INSERT INTO risk_scores (tenant_id, dimension, entity_id, score, band, open_alerts, components, computed_at)
		SELECT a.tenant_id, 'station', a.station_id,
		       LEAST(SUM(a.score), 100) AS score,
		       CASE
		           WHEN LEAST(SUM(a.score),100) >= 90 THEN 'critical'
		           WHEN LEAST(SUM(a.score),100) >= 70 THEN 'high'
		           WHEN LEAST(SUM(a.score),100) >= 40 THEN 'elevated'
		           WHEN LEAST(SUM(a.score),100) >= 15 THEN 'watch'
		           ELSE 'low'
		       END,
		       count(*),
		       jsonb_object_agg(a.alert_type, a.cnt),
		       now()
		FROM (
		    SELECT tenant_id, station_id, alert_type, score, count(*) OVER (PARTITION BY station_id, alert_type) AS cnt
		    FROM risk_alerts
		    WHERE tenant_id = $1 AND station_id IS NOT NULL AND status IN ('open','acknowledged','investigating','escalated')
		) a
		GROUP BY a.tenant_id, a.station_id
		ON CONFLICT (tenant_id, dimension, entity_id) DO UPDATE SET
		    score = EXCLUDED.score, band = EXCLUDED.band, open_alerts = EXCLUDED.open_alerts,
		    components = EXCLUDED.components, computed_at = now()
	`, tenantID); err != nil {
		return 0, err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM risk_scores WHERE tenant_id = $1 AND dimension = 'station'`, tenantID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

type Score struct {
	Dimension  string
	EntityID   uuid.UUID
	Score      int
	Band       string
	OpenAlerts int
}

func (r *Repo) ListScores(ctx context.Context, tenantID uuid.UUID, dimension string) ([]Score, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT dimension, entity_id, score, band, open_alerts FROM risk_scores
		WHERE tenant_id = $1 AND ($2 = '' OR dimension = $2) ORDER BY score DESC
	`, tenantID, dimension)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Score{}
	for rows.Next() {
		var s Score
		if err := rows.Scan(&s.Dimension, &s.EntityID, &s.Score, &s.Band, &s.OpenAlerts); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListScoresPage is the paginated variant of ListScores (REL-REPO). score is not
// unique, so (dimension, entity_id) is appended as a deterministic tiebreaker.
// ListScores remains in use by Overview, so this is purely additive.
func (r *Repo) ListScoresPage(ctx context.Context, tenantID uuid.UUID, dimension string, limit, offset int) ([]Score, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT dimension, entity_id, score, band, open_alerts FROM risk_scores
		WHERE tenant_id = $1 AND ($2 = '' OR dimension = $2)
		ORDER BY score DESC, dimension, entity_id
		LIMIT $3 OFFSET $4
	`, tenantID, dimension, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Score{}
	for rows.Next() {
		var s Score
		if err := rows.Scan(&s.Dimension, &s.EntityID, &s.Score, &s.Band, &s.OpenAlerts); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Overview is the risk dashboard aggregate.
type Overview struct {
	OpenBySeverity map[string]int
	OpenTotal      int
	TopStations    []Score
	ComputedAt     *time.Time
}

func (r *Repo) Overview(ctx context.Context, tenantID uuid.UUID) (*Overview, error) {
	o := &Overview{OpenBySeverity: map[string]int{}}
	rows, err := r.pool.Query(ctx, `
		SELECT severity, count(*) FROM risk_alerts
		WHERE tenant_id = $1 AND status IN ('open','acknowledged','investigating','escalated')
		GROUP BY severity
	`, tenantID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var sev string
		var n int
		if err := rows.Scan(&sev, &n); err != nil {
			rows.Close()
			return nil, err
		}
		o.OpenBySeverity[sev] = n
		o.OpenTotal += n
	}
	rows.Close()
	top, err := r.ListScores(ctx, tenantID, "station")
	if err != nil {
		return nil, err
	}
	if len(top) > 10 {
		top = top[:10]
	}
	o.TopStations = top
	_ = r.pool.QueryRow(ctx, `SELECT max(computed_at) FROM risk_scores WHERE tenant_id = $1`, tenantID).Scan(&o.ComputedAt)
	return o, nil
}
