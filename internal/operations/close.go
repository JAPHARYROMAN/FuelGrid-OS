package operations

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CloseLine is the per-nozzle snapshot frozen when a shift closes. Every
// money/litre field is an exact decimal STRING (the underlying numeric read
// ::text); litres-sold and expected-value arithmetic is done in SQL numeric,
// never Go float64 (MD-5/OPS-001). Scales: readings/litres numeric(14,3) ->
// "x.xxx"; unit_price/expected_value numeric(14,2) -> "x.xx".
type CloseLine struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	ShiftID        uuid.UUID
	NozzleID       uuid.UUID
	OpeningReading string
	ClosingReading string
	LitresSold     string
	UnitPrice      string
	ExpectedValue  string
}

// CloseLineInput is the raw per-nozzle close input: the opening/closing meter
// and the nozzle's unit price, all as exact decimal strings bound $N::numeric.
// litres_sold (= closing - opening) and expected_value (= litres * unit_price)
// are computed in SQL during the insert, never in Go.
type CloseLineInput struct {
	ShiftID        uuid.UUID
	NozzleID       uuid.UUID
	OpeningReading string
	ClosingReading string
	UnitPrice      string
}

// CashSubmission is the attendant's tender breakdown for a shift and the
// shortage/excess against expected cash. Money fields are exact decimal STRINGS
// (numeric(14,2) read ::text); the submitted total and variance are computed in
// SQL numeric, never Go float64 (MD-5).
type CashSubmission struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	ShiftID           uuid.UUID
	ExpectedCash      string
	CashAmount        string
	MobileMoneyAmount string
	CardAmount        string
	CreditAmount      string
	SubmittedTotal    string
	Variance          string
	SubmittedBy       uuid.UUID
	SubmittedAt       time.Time
	Notes             *string
}

// CashSubmissionInput is the tender breakdown plus expected cash, all as exact
// decimal strings bound $N::numeric. submitted_total (= sum of tenders) and
// variance (= submitted_total - expected_cash) are computed in SQL.
type CashSubmissionInput struct {
	ShiftID           uuid.UUID
	ExpectedCash      string
	CashAmount        string
	MobileMoneyAmount string
	CardAmount        string
	CreditAmount      string
	SubmittedBy       uuid.UUID
	Notes             *string
}

// InsertCloseLine writes one per-nozzle close line inside the caller's tx,
// computing litres_sold (closing - opening) and expected_value (litres *
// unit_price) in SQL numeric from the bound decimal strings. The computed line
// (with litres_sold and expected_value as exact decimal strings) is returned so
// the caller never has to recompute money in Go.
func (r *Repo) InsertCloseLine(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CloseLineInput) (*CloseLine, error) {
	l := CloseLine{
		TenantID: tenantID, ShiftID: in.ShiftID, NozzleID: in.NozzleID,
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO shift_close_lines
		    (tenant_id, shift_id, nozzle_id, opening_reading, closing_reading,
		     litres_sold, unit_price, expected_value)
		VALUES ($1, $2, $3, $4::numeric, $5::numeric,
		        ($5::numeric - $4::numeric),
		        $6::numeric,
		        (($5::numeric - $4::numeric) * $6::numeric))
		RETURNING id, opening_reading::text, closing_reading::text,
		          litres_sold::text, unit_price::text, expected_value::text
	`, tenantID, in.ShiftID, in.NozzleID, in.OpeningReading, in.ClosingReading, in.UnitPrice,
	).Scan(&l.ID, &l.OpeningReading, &l.ClosingReading, &l.LitresSold, &l.UnitPrice, &l.ExpectedValue); err != nil {
		return nil, err
	}
	return &l, nil
}

func (r *Repo) ListCloseLines(ctx context.Context, tenantID, shiftID uuid.UUID) ([]CloseLine, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, shift_id, nozzle_id,
		       opening_reading::text, closing_reading::text,
		       litres_sold::text, unit_price::text, expected_value::text
		FROM shift_close_lines
		WHERE tenant_id = $1 AND shift_id = $2
		ORDER BY nozzle_id
	`, tenantID, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CloseLine
	for rows.Next() {
		var l CloseLine
		if err := rows.Scan(&l.ID, &l.TenantID, &l.ShiftID, &l.NozzleID,
			&l.OpeningReading, &l.ClosingReading, &l.LitresSold, &l.UnitPrice, &l.ExpectedValue); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// CloseLineDetail is a close line enriched with its nozzle's pump/product
// display labels — the per-nozzle calculation basis (litres_sold × unit_price
// = expected_value) the attendant collections screen renders (Mobile
// Attendant Phase 4, PRD §7.9/§12.3). Figures stay exact decimal strings.
type CloseLineDetail struct {
	CloseLine
	PumpNumber   int
	NozzleNumber int
	ProductName  string
	ProductColor string
}

// ListCloseLineDetails returns the shift's close lines with their nozzle's
// pump/product labels, ordered for display (pump then nozzle number).
func (r *Repo) ListCloseLineDetails(ctx context.Context, tenantID, shiftID uuid.UUID) ([]CloseLineDetail, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT l.id, l.tenant_id, l.shift_id, l.nozzle_id,
		       l.opening_reading::text, l.closing_reading::text,
		       l.litres_sold::text, l.unit_price::text, l.expected_value::text,
		       p.number, n.number, pr.name, pr.color
		FROM shift_close_lines l
		JOIN nozzles n   ON n.id  = l.nozzle_id
		JOIN pumps p     ON p.id  = n.pump_id
		JOIN products pr ON pr.id = n.product_id
		WHERE l.tenant_id = $1 AND l.shift_id = $2
		ORDER BY p.number, n.number
	`, tenantID, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CloseLineDetail
	for rows.Next() {
		var l CloseLineDetail
		if err := rows.Scan(&l.ID, &l.TenantID, &l.ShiftID, &l.NozzleID,
			&l.OpeningReading, &l.ClosingReading, &l.LitresSold, &l.UnitPrice, &l.ExpectedValue,
			&l.PumpNumber, &l.NozzleNumber, &l.ProductName, &l.ProductColor); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// rowQuerier is satisfied by both *database.Pool and pgx.Tx, so the expected-
// cash SUM can run either against committed data (dashboards) or inside the
// close tx (which must see its own just-inserted, uncommitted lines).
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// SumExpectedForShift returns the shift's total expected cash — SUM of the
// close lines' expected_value, computed in SQL numeric — as an exact decimal
// string ("0.00" when there are no lines). numeric(14,2) -> "x.xx". Pass the
// close tx to include just-inserted lines, or the pool for committed data.
func (r *Repo) SumExpectedForShift(ctx context.Context, q rowQuerier, tenantID, shiftID uuid.UUID) (string, error) {
	var total string
	if err := q.QueryRow(ctx, `
		SELECT COALESCE(SUM(expected_value), 0)::numeric(14,2)::text
		FROM shift_close_lines
		WHERE tenant_id = $1 AND shift_id = $2
	`, tenantID, shiftID).Scan(&total); err != nil {
		return "", err
	}
	return total, nil
}

// Pool exposes the repo's connection pool as a rowQuerier for SumExpectedForShift
// callers that read committed data (dashboards, summaries).
func (r *Repo) Pool() rowQuerier { return r.pool }

// ShiftCloseTotals is a shift's aggregated close figures for dashboards: total
// expected cash and total litres sold, both summed in SQL numeric and returned
// as exact decimal strings (expected_cash numeric(14,2) -> "x.xx"; litres_sold
// numeric(14,3) -> "x.xxx").
type ShiftCloseTotals struct {
	ExpectedCash string
	LitresSold   string
}

// CloseTotalsForShift returns the SQL-numeric SUMs of expected_value and
// litres_sold over a shift's close lines as exact decimal strings ("0.00" /
// "0.000" when there are no lines) — no Go float aggregation.
func (r *Repo) CloseTotalsForShift(ctx context.Context, tenantID, shiftID uuid.UUID) (ShiftCloseTotals, error) {
	var t ShiftCloseTotals
	if err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(expected_value), 0)::numeric(14,2)::text,
		       COALESCE(SUM(litres_sold), 0)::numeric(14,3)::text
		FROM shift_close_lines
		WHERE tenant_id = $1 AND shift_id = $2
	`, tenantID, shiftID).Scan(&t.ExpectedCash, &t.LitresSold); err != nil {
		return t, err
	}
	return t, nil
}

const cashColumns = `
    id, tenant_id, shift_id, expected_cash::text, cash_amount::text,
    mobile_money_amount::text, card_amount::text, credit_amount::text,
    submitted_total::text, variance::text, submitted_by, submitted_at, notes
`

func scanCash(row pgx.Row, c *CashSubmission) error {
	return row.Scan(
		&c.ID, &c.TenantID, &c.ShiftID, &c.ExpectedCash, &c.CashAmount, &c.MobileMoneyAmount,
		&c.CardAmount, &c.CreditAmount, &c.SubmittedTotal, &c.Variance, &c.SubmittedBy,
		&c.SubmittedAt, &c.Notes,
	)
}

// InsertCashSubmission writes the tender breakdown, computing submitted_total
// (sum of the four tenders) and variance (submitted_total - expected_cash) in
// SQL numeric from the bound decimal strings — no Go float money. The persisted
// row (all money as exact decimal strings) is returned.
func (r *Repo) InsertCashSubmission(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CashSubmissionInput) (*CashSubmission, error) {
	var c CashSubmission
	if err := scanCash(tx.QueryRow(ctx, `
		INSERT INTO cash_submissions
		    (tenant_id, shift_id, expected_cash, cash_amount, mobile_money_amount,
		     card_amount, credit_amount, submitted_total, variance, submitted_by, notes)
		VALUES ($1, $2, $3::numeric, $4::numeric, $5::numeric, $6::numeric, $7::numeric,
		        ($4::numeric + $5::numeric + $6::numeric + $7::numeric),
		        (($4::numeric + $5::numeric + $6::numeric + $7::numeric) - $3::numeric),
		        $8, $9)
		RETURNING `+cashColumns,
		tenantID, in.ShiftID, in.ExpectedCash, in.CashAmount, in.MobileMoneyAmount,
		in.CardAmount, in.CreditAmount, in.SubmittedBy, in.Notes,
	), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// TankSales is a shift's metered litres-sold aggregated up to one tank — the
// bridge from per-nozzle close lines to a per-tank inventory draw-down.
type TankSales struct {
	TankID uuid.UUID
	// MD boundary: LitresSold feeds the inventory sales path (inventory.SaleLine
	// is still float at its own MD-1 boundary). The SUM itself is done in SQL
	// numeric; the float is only the carrier into that downstream boundary.
	LitresSold float64
}

// LitresSoldPerTankForShift sums a shift's frozen close-line litres up to the
// tank behind each nozzle, so Phase 4 can post one sales movement per tank.
// Tanks with a net zero are still returned; the caller decides what to post.
func (r *Repo) LitresSoldPerTankForShift(ctx context.Context, tenantID, shiftID uuid.UUID) ([]TankSales, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT n.tank_id, COALESCE(SUM(cl.litres_sold), 0)
		FROM shift_close_lines cl
		JOIN nozzles n ON n.id = cl.nozzle_id AND n.tenant_id = cl.tenant_id
		WHERE cl.tenant_id = $1 AND cl.shift_id = $2
		GROUP BY n.tank_id
		ORDER BY n.tank_id
	`, tenantID, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TankSales
	for rows.Next() {
		var ts TankSales
		if err := rows.Scan(&ts.TankID, &ts.LitresSold); err != nil {
			return nil, err
		}
		out = append(out, ts)
	}
	return out, rows.Err()
}

// VarianceExceeds reports whether |variance| > threshold, comparing the two
// decimal strings in SQL numeric (no Go float). Used to decide whether a cash
// submission auto-raises a high cash_variance exception (OPS-001 boundary: the
// threshold compare stays decimal so the exception fires on the exact figure).
func (r *Repo) VarianceExceeds(ctx context.Context, variance, threshold string) (bool, error) {
	var over bool
	if err := r.pool.QueryRow(ctx,
		`SELECT abs($1::numeric) > $2::numeric`, variance, threshold,
	).Scan(&over); err != nil {
		return false, err
	}
	return over, nil
}

// GetCashSubmission returns a shift's cash submission, or pgx.ErrNoRows.
func (r *Repo) GetCashSubmission(ctx context.Context, tenantID, shiftID uuid.UUID) (*CashSubmission, error) {
	var c CashSubmission
	if err := scanCash(r.pool.QueryRow(ctx, `
		SELECT `+cashColumns+` FROM cash_submissions WHERE tenant_id = $1 AND shift_id = $2
	`, tenantID, shiftID), &c); err != nil {
		return nil, err
	}
	return &c, nil
}
