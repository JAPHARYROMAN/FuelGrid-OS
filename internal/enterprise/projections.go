package enterprise

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RebuildStationKPIs recomputes the station_daily_kpis projection from posted
// Phase-6 revenue days, idempotently (replays don't double-count), and records
// projection freshness. Returns the row count.
func (r *Repo) RebuildStationKPIs(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (int, error) {
	if _, err := tx.Exec(ctx, `
		INSERT INTO station_daily_kpis (tenant_id, station_id, business_date, gross_revenue, net_revenue, margin_total, cogs_total)
		SELECT tenant_id, station_id, business_date, gross_revenue, net_revenue, margin_total, cogs_total
		FROM revenue_days WHERE tenant_id = $1
		ON CONFLICT (tenant_id, station_id, business_date) DO UPDATE SET
		    gross_revenue = EXCLUDED.gross_revenue, net_revenue = EXCLUDED.net_revenue,
		    margin_total = EXCLUDED.margin_total, cogs_total = EXCLUDED.cogs_total, updated_at = now()
	`, tenantID); err != nil {
		return 0, err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM station_daily_kpis WHERE tenant_id = $1`, tenantID).Scan(&n); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO enterprise_projection_state (tenant_id, projection_type, last_rebuilt_at, row_count)
		VALUES ($1, 'station_daily_kpis', now(), $2)
		ON CONFLICT (tenant_id, projection_type) DO UPDATE SET last_rebuilt_at = now(), row_count = $2
	`, tenantID, n); err != nil {
		return 0, err
	}
	return n, nil
}

// Overview is the executive command-center aggregate.
type Overview struct {
	GrossRevenue     string
	NetRevenue       string
	MarginTotal      string
	APOutstanding    string
	AROutstanding    string
	OpenIncidents    int
	ApprovalsWaiting int
	ProjectionAt     *time.Time
}

func (r *Repo) EnterpriseOverview(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (*Overview, error) {
	var o Overview
	if err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(gross_revenue),0)::text, COALESCE(SUM(net_revenue),0)::text, COALESCE(SUM(margin_total),0)::text
		FROM station_daily_kpis WHERE tenant_id = $1 AND business_date BETWEEN $2 AND $3
	`, tenantID, from, to).Scan(&o.GrossRevenue, &o.NetRevenue, &o.MarginTotal); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(outstanding_amount),0)::text FROM payables WHERE tenant_id = $1 AND status IN ('open','partially_paid')
	`, tenantID).Scan(&o.APOutstanding); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount),0)::text FROM ar_entries WHERE tenant_id = $1
	`, tenantID).Scan(&o.AROutstanding); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(count(*) FILTER (WHERE status NOT IN ('resolved','closed')),0) FROM incidents WHERE tenant_id = $1
	`, tenantID).Scan(&o.OpenIncidents); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM approval_requests WHERE tenant_id = $1 AND status = 'requested'
	`, tenantID).Scan(&o.ApprovalsWaiting); err != nil {
		return nil, err
	}
	_ = r.pool.QueryRow(ctx, `SELECT last_rebuilt_at FROM enterprise_projection_state WHERE tenant_id = $1 AND projection_type = 'station_daily_kpis'`, tenantID).Scan(&o.ProjectionAt)
	return &o, nil
}

// StationRank is one station's ranked KPIs.
type StationRank struct {
	StationID    uuid.UUID
	Name         string
	GrossRevenue string
	MarginTotal  string
}

// StationRanking ranks stations (optionally within a region) by gross revenue
// over a period, reading the projection.
func (r *Repo) StationRanking(ctx context.Context, tenantID uuid.UUID, regionID *uuid.UUID, from, to time.Time) ([]StationRank, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.name, COALESCE(SUM(k.gross_revenue),0)::text, COALESCE(SUM(k.margin_total),0)::text
		FROM stations s
		LEFT JOIN station_daily_kpis k ON k.station_id = s.id AND k.tenant_id = s.tenant_id AND k.business_date BETWEEN $2 AND $3
		WHERE s.tenant_id = $1 AND ($4::uuid IS NULL OR s.region_id = $4)
		GROUP BY s.id, s.name
		ORDER BY COALESCE(SUM(k.gross_revenue),0) DESC
	`, tenantID, from, to, regionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StationRank{}
	for rows.Next() {
		var sr StationRank
		if err := rows.Scan(&sr.StationID, &sr.Name, &sr.GrossRevenue, &sr.MarginTotal); err != nil {
			return nil, err
		}
		out = append(out, sr)
	}
	return out, rows.Err()
}
