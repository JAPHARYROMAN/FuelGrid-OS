// Package revenue is the data layer for recognized sales — valuing each
// approved shift's metered litres at the resolved selling price, splitting
// net + tax, and costing them at the tank's moving-average landed cost for
// COGS and margin (Phase 6, Stages 3-4).
//
// All money is computed in SQL (numeric) and carried as decimal strings.
package revenue

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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

// TankValuation is a tank's stock-on-hand valued at moving-average cost.
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
// as tax-inclusive (net = gross − tax). COGS uses the tank's moving-average
// landed cost when costed deliveries exist. Lines for products with no
// resolvable price are skipped. Returns the number of sales recognized.
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
		    LEFT JOIN LATERAL (
		        SELECT (SUM(sm.litres * sm.landed_cost_per_litre) / NULLIF(SUM(sm.litres), 0)) AS avg_cost
		        FROM stock_movements sm
		        WHERE sm.tenant_id = cl.tenant_id AND sm.tank_id = n.tank_id
		          AND sm.movement_type = 'delivery' AND sm.landed_cost_per_litre IS NOT NULL AND sm.litres > 0
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

func (r *Repo) ListForStationDay(ctx context.Context, tenantID, stationID, dayID uuid.UUID) ([]Sale, error) {
	return r.list(ctx, `WHERE tenant_id = $1 AND station_id = $2 AND operating_day_id = $3 ORDER BY recorded_at`,
		tenantID, stationID, dayID)
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

// DaySummary rolls up a station-day's recognized revenue, COGS, and margin.
func (r *Repo) DaySummary(ctx context.Context, q database.Querier, tenantID, stationID, dayID uuid.UUID) (DaySummary, error) {
	var d DaySummary
	err := q.QueryRow(ctx, `
		SELECT COALESCE(SUM(gross_amount), 0)::text, COALESCE(SUM(net_amount), 0)::text,
		       COALESCE(SUM(tax_amount), 0)::text, COALESCE(SUM(cogs_amount), 0)::text,
		       COALESCE(SUM(margin_amount), 0)::text, COALESCE(SUM(litres), 0), count(*)
		FROM sales
		WHERE tenant_id = $1 AND station_id = $2 AND operating_day_id = $3
	`, tenantID, stationID, dayID).Scan(
		&d.GrossAmount, &d.NetAmount, &d.TaxAmount, &d.CogsAmount, &d.MarginAmount, &d.LitresSold, &d.SaleCount,
	)
	return d, err
}

// InventoryValuation values each of a station's tanks' book stock at its
// moving-average landed cost.
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
