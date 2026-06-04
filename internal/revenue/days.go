package revenue

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrNotFound is returned when a revenue day doesn't resolve.
var ErrNotFound = errors.New("revenue: revenue day not found")

// ErrLocked is returned when an operation targets a locked revenue day.
var ErrLocked = errors.New("revenue: revenue day is locked")

// RevenueDay is a station-day's rolled-up revenue close.
type RevenueDay struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	StationID        uuid.UUID
	OperatingDayID   uuid.UUID
	BusinessDate     time.Time
	GrossRevenue     string
	NetRevenue       string
	TaxTotal         string
	CogsTotal        string
	MarginTotal      string
	CashTotal        string
	MobileMoneyTotal string
	CardTotal        string
	CreditTotal      string
	VoucherTotal     string
	TenderTotal      string
	CashVariance     string
	Status           string
	LockedBy         *uuid.UUID
	LockedAt         *time.Time
}

const dayColumns = `
    id, tenant_id, station_id, operating_day_id, business_date,
    gross_revenue::text, net_revenue::text, tax_total::text, cogs_total::text, margin_total::text,
    cash_total::text, mobile_money_total::text, card_total::text, credit_total::text, voucher_total::text,
    tender_total::text, cash_variance::text, status, locked_by, locked_at
`

func scanDay(row pgx.Row, d *RevenueDay) error {
	return row.Scan(
		&d.ID, &d.TenantID, &d.StationID, &d.OperatingDayID, &d.BusinessDate,
		&d.GrossRevenue, &d.NetRevenue, &d.TaxTotal, &d.CogsTotal, &d.MarginTotal,
		&d.CashTotal, &d.MobileMoneyTotal, &d.CardTotal, &d.CreditTotal, &d.VoucherTotal,
		&d.TenderTotal, &d.CashVariance, &d.Status, &d.LockedBy, &d.LockedAt,
	)
}

// ComputeDay rolls up a station-day's sales and tenders into a draft
// revenue_days row inside the caller's tx (idempotent upsert). A locked row is
// not recomputed — that yields ErrLocked.
func (r *Repo) ComputeDay(ctx context.Context, tx pgx.Tx, tenantID, stationID, dayID uuid.UUID) (*RevenueDay, error) {
	var d RevenueDay
	err := scanDay(tx.QueryRow(ctx, `
		INSERT INTO revenue_days
		    (tenant_id, station_id, operating_day_id, business_date, gross_revenue, net_revenue,
		     tax_total, cogs_total, margin_total, cash_total, mobile_money_total, card_total,
		     credit_total, voucher_total, tender_total, cash_variance, status)
		SELECT $1, $2, $3, od.business_date,
		    s.gross, s.net, s.tax, s.cogs, s.margin,
		    p.cash, p.momo, p.card, p.credit, p.voucher, p.tender,
		    p.tender - s.gross, 'draft'
		FROM operating_days od,
		    -- Revenue NET of approved sale voids (Feature 4.3): an approved void
		    -- carries the sale's amounts negated (reversal_*), so adding them
		    -- reverses the voided sale without mutating the append-only sale row.
		    (SELECT COALESCE(SUM(sl.gross_amount), 0)  + COALESCE(SUM(v.reversal_gross),  0) gross,
		            COALESCE(SUM(sl.net_amount), 0)    + COALESCE(SUM(v.reversal_net),    0) net,
		            COALESCE(SUM(sl.tax_amount), 0)    + COALESCE(SUM(v.reversal_tax),    0) tax,
		            COALESCE(SUM(sl.cogs_amount), 0)   + COALESCE(SUM(v.reversal_cogs),   0) cogs,
		            COALESCE(SUM(sl.margin_amount), 0) + COALESCE(SUM(v.reversal_margin), 0) margin
		     FROM sales sl
		     LEFT JOIN sale_voids v
		         ON v.tenant_id = sl.tenant_id AND v.sale_id = sl.id AND v.status = 'approved'
		     WHERE sl.tenant_id = $1 AND sl.station_id = $2 AND sl.operating_day_id = $3) s,
		    (SELECT COALESCE(SUM(amount) FILTER (WHERE tender_type = 'cash'), 0) cash,
		            COALESCE(SUM(amount) FILTER (WHERE tender_type = 'mobile_money'), 0) momo,
		            COALESCE(SUM(amount) FILTER (WHERE tender_type = 'card'), 0) card,
		            COALESCE(SUM(amount) FILTER (WHERE tender_type = 'credit'), 0) credit,
		            COALESCE(SUM(amount) FILTER (WHERE tender_type = 'voucher'), 0) voucher,
		            COALESCE(SUM(amount), 0) tender
		     FROM payments
		     WHERE tenant_id = $1 AND station_id = $2 AND status = 'recorded'
		       AND shift_id IN (SELECT id FROM shifts WHERE tenant_id = $1 AND operating_day_id = $3)) p
		WHERE od.tenant_id = $1 AND od.id = $3
		ON CONFLICT (station_id, operating_day_id) DO UPDATE SET
		    gross_revenue = EXCLUDED.gross_revenue, net_revenue = EXCLUDED.net_revenue,
		    tax_total = EXCLUDED.tax_total, cogs_total = EXCLUDED.cogs_total,
		    margin_total = EXCLUDED.margin_total, cash_total = EXCLUDED.cash_total,
		    mobile_money_total = EXCLUDED.mobile_money_total, card_total = EXCLUDED.card_total,
		    credit_total = EXCLUDED.credit_total, voucher_total = EXCLUDED.voucher_total,
		    tender_total = EXCLUDED.tender_total, cash_variance = EXCLUDED.cash_variance
		WHERE revenue_days.status <> 'locked'
		RETURNING `+dayColumns,
		tenantID, stationID, dayID,
	), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLocked
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *Repo) GetDayByID(ctx context.Context, tenantID, id uuid.UUID) (*RevenueDay, error) {
	var d RevenueDay
	err := scanDay(r.pool.QueryRow(ctx, `SELECT `+dayColumns+` FROM revenue_days WHERE tenant_id = $1 AND id = $2`,
		tenantID, id), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *Repo) GetDay(ctx context.Context, tenantID, stationID, dayID uuid.UUID) (*RevenueDay, error) {
	var d RevenueDay
	err := scanDay(r.pool.QueryRow(ctx, `
		SELECT `+dayColumns+` FROM revenue_days WHERE tenant_id = $1 AND station_id = $2 AND operating_day_id = $3
	`, tenantID, stationID, dayID), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// RecentDays returns a station's most recent revenue days for the trend.
func (r *Repo) RecentDays(ctx context.Context, tenantID, stationID uuid.UUID, limit int) ([]RevenueDay, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+dayColumns+` FROM revenue_days
		WHERE tenant_id = $1 AND station_id = $2 ORDER BY business_date DESC LIMIT $3
	`, tenantID, stationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RevenueDay{}
	for rows.Next() {
		var d RevenueDay
		if err := scanDay(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// TenderBreakdown is a station-day's recorded tenders by type.
type TenderBreakdown struct {
	Cash        string
	MobileMoney string
	Card        string
	Credit      string
	Voucher     string
	Total       string
}

// DayTenders returns a station-day's recorded tenders by type (live, from
// payments).
func (r *Repo) DayTenders(ctx context.Context, tenantID, stationID, dayID uuid.UUID) (TenderBreakdown, error) {
	var t TenderBreakdown
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount) FILTER (WHERE tender_type = 'cash'), 0)::text,
		       COALESCE(SUM(amount) FILTER (WHERE tender_type = 'mobile_money'), 0)::text,
		       COALESCE(SUM(amount) FILTER (WHERE tender_type = 'card'), 0)::text,
		       COALESCE(SUM(amount) FILTER (WHERE tender_type = 'credit'), 0)::text,
		       COALESCE(SUM(amount) FILTER (WHERE tender_type = 'voucher'), 0)::text,
		       COALESCE(SUM(amount), 0)::text
		FROM payments
		WHERE tenant_id = $1 AND station_id = $2 AND status = 'recorded'
		  AND shift_id IN (SELECT id FROM shifts WHERE tenant_id = $1 AND operating_day_id = $3)
	`, tenantID, stationID, dayID).Scan(&t.Cash, &t.MobileMoney, &t.Card, &t.Credit, &t.Voucher, &t.Total)
	return t, err
}

// LockDay freezes a revenue day inside the caller's tx. A day already locked
// yields ErrLocked.
func (r *Repo) LockDay(ctx context.Context, tx pgx.Tx, tenantID, id, lockedBy uuid.UUID) (*RevenueDay, error) {
	var d RevenueDay
	err := scanDay(tx.QueryRow(ctx, `
		UPDATE revenue_days SET status = 'locked', locked_by = $3, locked_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status <> 'locked'
		RETURNING `+dayColumns,
		tenantID, id, lockedBy,
	), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gErr := r.GetDayByID(ctx, tenantID, id); errors.Is(gErr, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, ErrLocked
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}
