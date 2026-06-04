// Package revenue is the data layer for recognized sales — valuing each
// approved shift's metered litres at the resolved selling price, splitting
// net + tax, and costing them at the tank's cumulative weighted-average landed
// cost for COGS and margin (Phase 6, Stages 3-4).
//
// COSTING POLICY: COGS, margin, and stock value use a CUMULATIVE (lifetime)
// weighted-average landed cost per litre — the litre-weighted average over all
// of a tank's posted, non-superseded delivery movements. It is deliberately NOT
// a perpetual ("moving") average: it is never decremented as stock is sold, so
// it equals a true moving average only while landed cost per litre is constant
// across deliveries and drifts from it otherwise. See docs/costing-policy.md
// for the full policy, its limitation, and when it is accurate.
//
// All money is computed in SQL (numeric) and carried as decimal strings.
package revenue

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/accounting"
	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// Sale is one recognized priced sale (one nozzle, one approved shift).
type Sale struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	ShiftID        uuid.UUID
	StationID      uuid.UUID
	OperatingDayID uuid.UUID
	NozzleID       uuid.UUID
	ProductID      uuid.UUID
	TankID         uuid.UUID
	Litres         float64
	UnitPrice      string
	GrossAmount    string
	TaxRate        string
	TaxAmount      string
	NetAmount      string
	UnitCost       *string
	CogsAmount     *string
	MarginAmount   *string
	RecordedBy     uuid.UUID
	RecordedAt     time.Time
}

// DaySummary is a station-day's revenue rollup.
type DaySummary struct {
	GrossAmount  string
	NetAmount    string
	TaxAmount    string
	CogsAmount   string
	MarginAmount string
	LitresSold   float64
	SaleCount    int
}

// TankValuation is a tank's stock-on-hand valued at cumulative
// weighted-average cost (see docs/costing-policy.md).
type TankValuation struct {
	TankID     uuid.UUID
	Code       string
	Name       string
	ProductID  uuid.UUID
	BookLitres float64
	AvgCost    *string
	StockValue *string
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const columns = `
    id, tenant_id, shift_id, station_id, operating_day_id, nozzle_id, product_id, tank_id,
    litres, unit_price::text, gross_amount::text, tax_rate::text, tax_amount::text, net_amount::text,
    unit_cost::text, cogs_amount::text, margin_amount::text, recorded_by, recorded_at
`

func scan(row pgx.Row, s *Sale) error {
	return row.Scan(
		&s.ID, &s.TenantID, &s.ShiftID, &s.StationID, &s.OperatingDayID, &s.NozzleID, &s.ProductID, &s.TankID,
		&s.Litres, &s.UnitPrice, &s.GrossAmount, &s.TaxRate, &s.TaxAmount, &s.NetAmount,
		&s.UnitCost, &s.CogsAmount, &s.MarginAmount, &s.RecordedBy, &s.RecordedAt,
	)
}

// RecognizeShiftSales values a shift's close lines into sale rows inside the
// caller's tx, idempotently (one sale per shift/nozzle). The selling price is
// the price resolved for the product at the station as of now; it is treated
// as tax-inclusive (net = gross − tax). COGS uses the tank's cumulative
// weighted-average landed cost (lifetime litre-weighted average over posted,
// non-superseded deliveries — NOT a perpetual moving average; see
// docs/costing-policy.md) when costed deliveries exist. Lines for products with
// no resolvable price are skipped. Returns the number of sales recognized.
func (r *Repo) RecognizeShiftSales(ctx context.Context, tx pgx.Tx, tenantID, shiftID, recordedBy uuid.UUID) (int64, error) {
	tag, err := tx.Exec(ctx, `
		WITH lines AS (
		    SELECT cl.tenant_id, cl.shift_id, sh.station_id, sh.operating_day_id, cl.nozzle_id,
		           n.product_id, n.tank_id, cl.litres_sold AS litres,
		           price.unit_price, pr.tax_rate, cost.avg_cost
		    FROM shift_close_lines cl
		    JOIN shifts sh   ON sh.id = cl.shift_id  AND sh.tenant_id = cl.tenant_id
		    JOIN nozzles n   ON n.id  = cl.nozzle_id AND n.tenant_id  = cl.tenant_id
		    JOIN products pr ON pr.id = n.product_id AND pr.tenant_id = cl.tenant_id
		    LEFT JOIN LATERAL (
		        SELECT pc.unit_price FROM price_changes pc
		        WHERE pc.tenant_id = cl.tenant_id AND pc.station_id = sh.station_id
		          AND pc.product_id = n.product_id AND pc.effective_from <= now()
		        ORDER BY pc.effective_from DESC, pc.created_at DESC LIMIT 1
		    ) price ON true
		    -- avg_cost is the CUMULATIVE (lifetime) weighted-average landed cost
		    -- per litre over the tank's posted, non-superseded deliveries — it is
		    -- NOT decremented as stock is sold (not a perpetual moving average).
		    -- See docs/costing-policy.md.
		    LEFT JOIN LATERAL (
		        SELECT (SUM(sm.litres * sm.landed_cost_per_litre) / NULLIF(SUM(sm.litres), 0)) AS avg_cost
		        FROM stock_movements sm
		        WHERE sm.tenant_id = cl.tenant_id AND sm.tank_id = n.tank_id
		          AND sm.movement_type = 'delivery' AND sm.landed_cost_per_litre IS NOT NULL AND sm.litres > 0
		          AND sm.status = 'posted' AND sm.supersedes_id IS NULL
		    ) cost ON true
		    WHERE cl.tenant_id = $1 AND cl.shift_id = $2 AND cl.litres_sold <> 0
		      AND price.unit_price IS NOT NULL
		)
		INSERT INTO sales
		    (tenant_id, shift_id, station_id, operating_day_id, nozzle_id, product_id, tank_id,
		     litres, unit_price, gross_amount, tax_rate, tax_amount, net_amount,
		     unit_cost, cogs_amount, margin_amount, recorded_by)
		SELECT tenant_id, shift_id, station_id, operating_day_id, nozzle_id, product_id, tank_id,
		    litres, unit_price,
		    ROUND(litres * unit_price, 2) AS gross,
		    tax_rate,
		    ROUND(ROUND(litres * unit_price, 2) * tax_rate / (100 + tax_rate), 2) AS tax,
		    ROUND(litres * unit_price, 2) - ROUND(ROUND(litres * unit_price, 2) * tax_rate / (100 + tax_rate), 2) AS net,
		    avg_cost,
		    CASE WHEN avg_cost IS NOT NULL THEN ROUND(litres * avg_cost, 2) END AS cogs,
		    CASE WHEN avg_cost IS NOT NULL THEN
		        (ROUND(litres * unit_price, 2) - ROUND(ROUND(litres * unit_price, 2) * tax_rate / (100 + tax_rate), 2))
		        - ROUND(litres * avg_cost, 2)
		    END AS margin,
		    $3
		FROM lines
		ON CONFLICT (shift_id, nozzle_id) DO NOTHING
	`, tenantID, shiftID, recordedBy)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) ListForShift(ctx context.Context, tenantID, shiftID uuid.UUID) ([]Sale, error) {
	return r.list(ctx, `WHERE tenant_id = $1 AND shift_id = $2 ORDER BY nozzle_id`, tenantID, shiftID)
}

// ListForShiftPage returns a page of a shift's sales ordered by nozzle (with id
// as a stable tiebreaker), applying the supplied limit and offset.
func (r *Repo) ListForShiftPage(ctx context.Context, tenantID, shiftID uuid.UUID, limit, offset int) ([]Sale, error) {
	return r.list(ctx, `WHERE tenant_id = $1 AND shift_id = $2 ORDER BY nozzle_id, id LIMIT $3 OFFSET $4`,
		tenantID, shiftID, limit, offset)
}

func (r *Repo) ListForStationDay(ctx context.Context, tenantID, stationID, dayID uuid.UUID) ([]Sale, error) {
	return r.list(ctx, `WHERE tenant_id = $1 AND station_id = $2 AND operating_day_id = $3 ORDER BY recorded_at`,
		tenantID, stationID, dayID)
}

// ListForStationDayPage returns a page of a station-day's sales ordered by
// recorded_at (with id as a stable tiebreaker), applying the supplied limit and
// offset.
func (r *Repo) ListForStationDayPage(ctx context.Context, tenantID, stationID, dayID uuid.UUID, limit, offset int) ([]Sale, error) {
	return r.list(ctx, `WHERE tenant_id = $1 AND station_id = $2 AND operating_day_id = $3 ORDER BY recorded_at, id LIMIT $4 OFFSET $5`,
		tenantID, stationID, dayID, limit, offset)
}

func (r *Repo) list(ctx context.Context, where string, args ...any) ([]Sale, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+columns+` FROM sales `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Sale{}
	for rows.Next() {
		var s Sale
		if err := scan(rows, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DaySummary rolls up a station-day's recognized revenue, COGS, and margin,
// NET of any approved sale voids (Feature 4.3). An approved void carries the
// sale's amounts negated (reversal_*), so adding them to the sale sums reverses
// the voided revenue without mutating the append-only sale row. SaleCount is the
// count of sales NOT currently reversed by an approved void.
func (r *Repo) DaySummary(ctx context.Context, q database.Querier, tenantID, stationID, dayID uuid.UUID) (DaySummary, error) {
	var d DaySummary
	err := q.QueryRow(ctx, `
		SELECT
		    (COALESCE(SUM(s.gross_amount),  0) + COALESCE(SUM(v.reversal_gross),  0))::text,
		    (COALESCE(SUM(s.net_amount),    0) + COALESCE(SUM(v.reversal_net),    0))::text,
		    (COALESCE(SUM(s.tax_amount),    0) + COALESCE(SUM(v.reversal_tax),    0))::text,
		    (COALESCE(SUM(s.cogs_amount),   0) + COALESCE(SUM(v.reversal_cogs),   0))::text,
		    (COALESCE(SUM(s.margin_amount), 0) + COALESCE(SUM(v.reversal_margin), 0))::text,
		    (COALESCE(SUM(s.litres),        0) + COALESCE(SUM(v.reversal_litres), 0)),
		    COUNT(*) FILTER (WHERE v.id IS NULL)
		FROM sales s
		LEFT JOIN sale_voids v
		    ON v.tenant_id = s.tenant_id AND v.sale_id = s.id AND v.status = 'approved'
		WHERE s.tenant_id = $1 AND s.station_id = $2 AND s.operating_day_id = $3
	`, tenantID, stationID, dayID).Scan(
		&d.GrossAmount, &d.NetAmount, &d.TaxAmount, &d.CogsAmount, &d.MarginAmount, &d.LitresSold, &d.SaleCount,
	)
	return d, err
}

// InventoryValuation values each of a station's tanks' book stock at its
// cumulative weighted-average landed cost (lifetime average over posted,
// non-superseded deliveries; not a perpetual moving average — see
// docs/costing-policy.md). The `avg_cost` SQL alias names this cumulative
// average, not a moving one.
func (r *Repo) InventoryValuation(ctx context.Context, tenantID, stationID uuid.UUID) ([]TankValuation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.id, t.code, t.name, t.product_id,
		    COALESCE(SUM(sm.litres), 0) AS book_litres,
		    cost.avg_cost::text,
		    CASE WHEN cost.avg_cost IS NOT NULL
		         THEN ROUND(COALESCE(SUM(sm.litres), 0) * cost.avg_cost, 2)::text END
		FROM tanks t
		LEFT JOIN stock_movements sm ON sm.tank_id = t.id AND sm.tenant_id = t.tenant_id
		LEFT JOIN LATERAL (
		    SELECT (SUM(d.litres * d.landed_cost_per_litre) / NULLIF(SUM(d.litres), 0)) AS avg_cost
		    FROM stock_movements d
		    WHERE d.tenant_id = t.tenant_id AND d.tank_id = t.id
		      AND d.movement_type = 'delivery' AND d.landed_cost_per_litre IS NOT NULL AND d.litres > 0
		      AND d.status = 'posted' AND d.supersedes_id IS NULL
		) cost ON true
		WHERE t.tenant_id = $1 AND t.station_id = $2 AND t.status <> 'deleted'
		GROUP BY t.id, t.code, t.name, t.product_id, cost.avg_cost
		ORDER BY t.code
	`, tenantID, stationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TankValuation{}
	for rows.Next() {
		var v TankValuation
		if err := rows.Scan(&v.TankID, &v.Code, &v.Name, &v.ProductID, &v.BookLitres, &v.AvgCost, &v.StockValue); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ShiftRevenue is the GL-posting summary of a shift's recognized sales: the
// gross/net/tax money totals (decimal strings) plus the station and the
// business date the revenue journal should be dated to.
type ShiftRevenue struct {
	Gross        string
	Net          string
	Tax          string
	StationID    uuid.UUID
	BusinessDate time.Time
	Found        bool
}

// ShiftRevenueTotals sums a shift's recognized sales for GL posting.
func (r *Repo) ShiftRevenueTotals(ctx context.Context, q database.Querier, tenantID, shiftID uuid.UUID) (ShiftRevenue, error) {
	var sr ShiftRevenue
	var n int
	// station_id and business_date come from the shift (always present); the
	// money totals are summed from the shift's recognized sales. Avoids any
	// aggregate over uuid (Postgres has no max(uuid)).
	err := q.QueryRow(ctx, `
		SELECT
		    (SELECT count(*)                          FROM sales WHERE tenant_id = $1 AND shift_id = $2),
		    COALESCE((SELECT SUM(gross_amount)        FROM sales WHERE tenant_id = $1 AND shift_id = $2), 0)::text,
		    COALESCE((SELECT SUM(net_amount)          FROM sales WHERE tenant_id = $1 AND shift_id = $2), 0)::text,
		    COALESCE((SELECT SUM(tax_amount)          FROM sales WHERE tenant_id = $1 AND shift_id = $2), 0)::text,
		    sh.station_id,
		    od.business_date
		FROM shifts sh
		JOIN operating_days od ON od.id = sh.operating_day_id AND od.tenant_id = sh.tenant_id
		WHERE sh.tenant_id = $1 AND sh.id = $2
	`, tenantID, shiftID).Scan(&n, &sr.Gross, &sr.Net, &sr.Tax, &sr.StationID, &sr.BusinessDate)
	if errors.Is(err, pgx.ErrNoRows) {
		return sr, nil // shift not found -> Found stays false
	}
	if err != nil {
		return sr, err
	}
	sr.Found = n > 0
	return sr, nil
}

// PostShiftRevenueJournal posts the general-ledger revenue entry for an
// approved shift's recognized sales, inside the caller's tx:
//
//	DR sales_clearing (gross)  CR sales_revenue (net)  CR output_vat (tax)
//
// gross = net + tax, so the entry balances. It is idempotent — it skips if an
// entry already exists for the shift — and a no-op (posted=false, nil error)
// when the shift has no recognized sales. Account/period errors propagate so
// the caller can decide whether to retry or log-and-skip (the tenant's chart or
// period may not be configured yet). posted=true only when a new entry was
// written.
func (r *Repo) PostShiftRevenueJournal(ctx context.Context, tx pgx.Tx, acct *accounting.Repo, tenantID, shiftID, postedBy uuid.UUID) (*accounting.JournalEntry, bool, error) {
	exists, err := acct.EntryExistsForSource(ctx, tx, tenantID, "shift_revenue", shiftID)
	if err != nil {
		return nil, false, err
	}
	if exists {
		return nil, false, nil
	}

	sr, err := r.ShiftRevenueTotals(ctx, tx, tenantID, shiftID)
	if err != nil {
		return nil, false, err
	}
	if !sr.Found || sr.Gross == "0" || sr.Gross == "0.00" {
		return nil, false, nil
	}

	station := sr.StationID
	memo := "shift sales revenue"
	lines := []accounting.PostLine{
		{SystemKey: "sales_clearing", Debit: sr.Gross, Credit: "0", StationID: &station},
		{SystemKey: "sales_revenue", Debit: "0", Credit: sr.Net, StationID: &station},
	}
	if sr.Tax != "0" && sr.Tax != "0.00" {
		lines = append(lines, accounting.PostLine{SystemKey: "output_vat", Debit: "0", Credit: sr.Tax, StationID: &station})
	}

	entry, err := acct.PostEntry(ctx, tx, tenantID, accounting.PostEntryInput{
		EntryDate: sr.BusinessDate, SourceType: "shift_revenue", SourceID: &shiftID,
		StationID: &station, Memo: &memo, PostedBy: postedBy, Lines: lines,
	})
	if err != nil {
		return nil, false, err
	}
	return entry, true, nil
}
