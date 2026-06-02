package risk

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The Rules & Insights Engine (Workstream D). Detection is driven by per-tenant
// rule rows whose `condition` column names a deterministic, code-backed
// Evaluator registered here — NOT a free-form expression. Every evaluator runs
// auditable SQL and does all money/litre arithmetic in the database (::numeric);
// no float64 ever touches a money/litre path. There is no AI: each fire is
// explainable from immutable source facts and the configured threshold.

// queryer is the subset of pgx used by evaluators, satisfied by both *pgxpool
// connections and pgx.Tx so evaluators run inside the detection transaction.
type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// RuleConfig is the operator-configurable surface an evaluator reads. Threshold
// is a decimal string ("" when NULL) so it stays off the float path.
type RuleConfig struct {
	Threshold            string // decimal string; "" when the rule threshold is NULL
	ComparisonPeriodDays int    // window in days; <= 0 means the evaluator default
	Severity             string
}

// Candidate is one detected problem an evaluator emits. It is rendered into an
// alert: DedupeKey gives idempotency, Vars feed the message_template, and Amount
// is a decimal string (litres or money depending on the evaluator).
type Candidate struct {
	StationID  *uuid.UUID
	EntityType string // tank_reconciliation | attendant | tank | po_line
	EntityID   uuid.UUID
	Amount     string // decimal string
	DedupeKey  string // stable per (tenant, condition); used as the open-alert subject key
	Vars       map[string]string
}

// Evaluator runs one named condition and returns the candidates that fire.
type Evaluator func(ctx context.Context, q queryer, tenantID uuid.UUID, asOf time.Time, cfg RuleConfig) ([]Candidate, error)

// evaluatorRegistry maps a condition key to its evaluator. RunDetection looks
// rules up here by their `condition`; an unknown condition is skipped.
var evaluatorRegistry = map[string]Evaluator{
	"fuel_variance_over_tolerance": evalFuelVarianceOverTolerance,
	"repeated_cash_shortage":       evalRepeatedCashShortage,
	"stockout_coverage":            evalStockoutCoverage,
	"supplier_delivery_shortage":   evalSupplierDeliveryShortage,
}

// EvaluatorFor returns the evaluator for a condition key, or (nil, false).
func EvaluatorFor(condition string) (Evaluator, bool) {
	e, ok := evaluatorRegistry[condition]
	return e, ok
}

// renderTemplate substitutes {token} placeholders in a message template with
// values from vars using deterministic string replacement. Unknown tokens are
// left intact (so a misconfigured template degrades visibly rather than
// silently dropping context). Exported for unit testing.
func renderTemplate(tmpl string, vars map[string]string) string {
	if tmpl == "" {
		return tmpl
	}
	out := tmpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}

// strOr returns the decimal string s, or fallback when s is empty.
func strOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// itoa renders a small non-negative int as a decimal string without pulling in
// fmt on this hot path. Used only for the {days} template var.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// stationStr renders an optional station id for a template var ("" when nil).
func stationStr(s *uuid.UUID) string {
	if s == nil {
		return ""
	}
	return s.String()
}

// (a) fuel_variance_over_tolerance — inventory. From tank_reconciliations, fire
// when abs(variance_litres) exceeds the per-product tolerance in litres
// (closing_book * loss_tolerance_percent / 100), or the rule threshold in
// litres when set. Amount = abs(variance_litres).
func evalFuelVarianceOverTolerance(ctx context.Context, q queryer, tenantID uuid.UUID, asOf time.Time, cfg RuleConfig) ([]Candidate, error) {
	// Threshold override: when set it is an absolute litre floor; otherwise the
	// product's loss_tolerance_percent applied to the book volume is the floor.
	rows, err := q.Query(ctx, `
		SELECT tr.id, t.station_id,
		       abs(tr.variance_litres)::text AS variance_litres,
		       p.name AS product, t.name AS tank, t.code AS tank_code
		FROM tank_reconciliations tr
		JOIN tanks t    ON t.id = tr.tank_id  AND t.tenant_id = tr.tenant_id
		JOIN products p ON p.id = t.product_id AND p.tenant_id = t.tenant_id
		WHERE tr.tenant_id = $1
		  AND tr.status = 'exception'
		  AND abs(tr.variance_litres) > GREATEST(
		        COALESCE(NULLIF($2, '')::numeric, 0),
		        CASE WHEN NULLIF($2, '') IS NULL
		             THEN abs(tr.closing_book) * p.loss_tolerance_percent / 100.0
		             ELSE 0 END)
		ORDER BY tr.id
	`, tenantID, cfg.Threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var id uuid.UUID
		var station uuid.UUID
		var variance, product, tank, tankCode string
		if err := rows.Scan(&id, &station, &variance, &product, &tank, &tankCode); err != nil {
			return nil, err
		}
		st := station
		out = append(out, Candidate{
			StationID:  &st,
			EntityType: "tank_reconciliation",
			EntityID:   id,
			Amount:     variance,
			DedupeKey:  id.String(),
			Vars: map[string]string{
				"variance_litres": variance,
				"product":         product,
				"station":         station.String(),
				"tank":            tank,
				"pump":            tankCode,
			},
		})
	}
	return out, rows.Err()
}

// (b) repeated_cash_shortage — cash. Fire when an attendant has >= threshold
// (default 3) cash shortages within comparison_period_days (default 7).
//
// Attendant linkage: cash_reconciliations carries variance per (station,
// operating day), not per shift. We attribute each shortage to the attendant(s)
// who worked the shifts on that reconciliation via
// cash_reconciliation_lines -> shifts -> shift_attendants -> users. A
// reconciliation that spans multiple shifts/attendants attributes the shortage
// EVENT to each attendant on it (count of distinct shortage reconciliations per
// attendant). This is the documented linkage decision (see PR notes).
func evalRepeatedCashShortage(ctx context.Context, q queryer, tenantID uuid.UUID, asOf time.Time, cfg RuleConfig) ([]Candidate, error) {
	days := cfg.ComparisonPeriodDays
	if days <= 0 {
		days = 7
	}
	threshold := strOr(cfg.Threshold, "3")
	rows, err := q.Query(ctx, `
		WITH shortages AS (
			SELECT DISTINCT cr.id AS recon_id, cr.station_id, sa.user_id, abs(cr.variance) AS shortage
			FROM cash_reconciliations cr
			JOIN cash_reconciliation_lines crl
			     ON crl.cash_reconciliation_id = cr.id AND crl.tenant_id = cr.tenant_id
			JOIN shift_attendants sa
			     ON sa.shift_id = crl.shift_id AND sa.tenant_id = cr.tenant_id
			WHERE cr.tenant_id = $1
			  AND cr.status = 'posted'
			  AND cr.variance < 0
			  AND cr.created_at >= $3::timestamptz - make_interval(days => $2)
			  AND cr.created_at <= $3::timestamptz
		)
		SELECT s.user_id,
		       COALESCE(u.full_name, u.email, s.user_id::text) AS attendant,
		       count(*)::text       AS cnt,
		       sum(s.shortage)::text AS total,
		       (min(s.station_id::text))::uuid AS station_id
		FROM shortages s
		JOIN users u ON u.id = s.user_id AND u.tenant_id = $1
		GROUP BY s.user_id, attendant
		HAVING count(*) >= $4::numeric
		ORDER BY s.user_id
	`, tenantID, days, asOf, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var userID uuid.UUID
		var attendant, cnt, total string
		var station *uuid.UUID
		if err := rows.Scan(&userID, &attendant, &cnt, &total, &station); err != nil {
			return nil, err
		}
		daysStr := itoa(days)
		out = append(out, Candidate{
			StationID:  station,
			EntityType: "attendant",
			EntityID:   userID,
			Amount:     total,
			DedupeKey:  userID.String(),
			Vars: map[string]string{
				"attendant": attendant,
				"count":     cnt,
				"days":      daysStr,
				"station":   stationStr(station),
			},
		})
	}
	return out, rows.Err()
}

// (c) stockout_coverage — inventory. coverage_days = current on-hand litres /
// average daily sales litres over comparison_period_days. Fire when
// coverage_days < threshold (default 2). All arithmetic in SQL; divide-by-zero
// guarded with NULLIF (tanks with no sales never fire). Amount = coverage_days.
func evalStockoutCoverage(ctx context.Context, q queryer, tenantID uuid.UUID, asOf time.Time, cfg RuleConfig) ([]Candidate, error) {
	days := cfg.ComparisonPeriodDays
	if days <= 0 {
		days = 14
	}
	threshold := strOr(cfg.Threshold, "2")
	// On-hand = SUM(stock_movements.litres) per tank (the ledger is the source
	// of truth). Average daily sales = abs(SUM(sales litres in window)) / days.
	// Sales movements are negative (outflow), so we negate to get a positive
	// daily draw. coverage_days = on_hand / daily_sales.
	rows, err := q.Query(ctx, `
		WITH onhand AS (
			SELECT tank_id, sum(litres) AS on_hand
			FROM stock_movements
			WHERE tenant_id = $1
			GROUP BY tank_id
		),
		sales AS (
			SELECT tank_id, abs(sum(litres)) AS sold
			FROM stock_movements
			WHERE tenant_id = $1
			  AND movement_type = 'sales'
			  AND recorded_at >= $3::timestamptz - make_interval(days => $2)
			  AND recorded_at <= $3::timestamptz
			GROUP BY tank_id
		)
		SELECT t.id, t.station_id, p.name AS product, t.name AS tank,
		       (oh.on_hand / NULLIF(s.sold / $2::numeric, 0))::numeric(14,3)::text AS coverage_days,
		       (oh.on_hand / NULLIF(s.sold / $2::numeric, 0) * 24)::numeric(14,1)::text AS hours
		FROM tanks t
		JOIN onhand oh  ON oh.tank_id = t.id
		JOIN sales  s   ON s.tank_id = t.id
		JOIN products p ON p.id = t.product_id AND p.tenant_id = t.tenant_id
		WHERE t.tenant_id = $1
		  AND t.status = 'active'
		  AND s.sold > 0
		  AND (oh.on_hand / NULLIF(s.sold / $2::numeric, 0)) < $4::numeric
		ORDER BY t.id
	`, tenantID, days, asOf, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var id, station uuid.UUID
		var product, tank, coverage, hours string
		if err := rows.Scan(&id, &station, &product, &tank, &coverage, &hours); err != nil {
			return nil, err
		}
		st := station
		out = append(out, Candidate{
			StationID:  &st,
			EntityType: "tank",
			EntityID:   id,
			Amount:     coverage,
			DedupeKey:  id.String(),
			Vars: map[string]string{
				"product":       product,
				"station":       station.String(),
				"tank":          tank,
				"coverage_days": coverage,
				"hours":         hours,
			},
		})
	}
	return out, rows.Err()
}

// (d) supplier_delivery_shortage — procurement. From purchase_order_lines, fire
// when received_litres < ordered_litres by more than the tolerance FRACTION
// (rule.threshold interpreted as a fraction, e.g. 0.02 = 2%; default 0.0 means
// any shortage fires). Amount = shortage litres (ordered - received).
func evalSupplierDeliveryShortage(ctx context.Context, q queryer, tenantID uuid.UUID, asOf time.Time, cfg RuleConfig) ([]Candidate, error) {
	fraction := strOr(cfg.Threshold, "0")
	rows, err := q.Query(ctx, `
		SELECT pol.id, po.station_id, s.name AS supplier, po.id::text AS po,
		       (pol.ordered_litres - pol.received_litres)::text AS shortage
		FROM purchase_order_lines pol
		JOIN purchase_orders po ON po.id = pol.purchase_order_id AND po.tenant_id = pol.tenant_id
		JOIN suppliers s        ON s.id = po.supplier_id AND s.tenant_id = po.tenant_id
		WHERE pol.tenant_id = $1
		  AND po.status IN ('partially_received', 'received', 'closed')
		  AND pol.received_litres < pol.ordered_litres
		  AND (pol.ordered_litres - pol.received_litres)
		        > pol.ordered_litres * COALESCE(NULLIF($2, '')::numeric, 0)
		ORDER BY pol.id
	`, tenantID, fraction)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var id uuid.UUID
		var station *uuid.UUID
		var supplier, po, shortage string
		if err := rows.Scan(&id, &station, &supplier, &po, &shortage); err != nil {
			return nil, err
		}
		out = append(out, Candidate{
			StationID:  station,
			EntityType: "po_line",
			EntityID:   id,
			Amount:     shortage,
			DedupeKey:  id.String(),
			Vars: map[string]string{
				"supplier":        supplier,
				"shortage_litres": shortage,
				"po":              po,
				"station":         stationStr(station),
			},
		})
	}
	return out, rows.Err()
}
