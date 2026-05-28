package operations

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CloseLine is the per-nozzle snapshot frozen when a shift closes.
type CloseLine struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	ShiftID        uuid.UUID
	NozzleID       uuid.UUID
	OpeningReading float64
	ClosingReading float64
	LitresSold     float64
	UnitPrice      float64
	ExpectedValue  float64
}

// CashSubmission is the attendant's tender breakdown for a shift and the
// shortage/excess against expected cash.
type CashSubmission struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	ShiftID           uuid.UUID
	ExpectedCash      float64
	CashAmount        float64
	MobileMoneyAmount float64
	CardAmount        float64
	CreditAmount      float64
	SubmittedTotal    float64
	Variance          float64
	SubmittedBy       uuid.UUID
	SubmittedAt       time.Time
	Notes             *string
}

type CashSubmissionInput struct {
	ShiftID           uuid.UUID
	ExpectedCash      float64
	CashAmount        float64
	MobileMoneyAmount float64
	CardAmount        float64
	CreditAmount      float64
	SubmittedTotal    float64
	Variance          float64
	SubmittedBy       uuid.UUID
	Notes             *string
}

// InsertCloseLine writes one per-nozzle close line inside the caller's tx.
func (r *Repo) InsertCloseLine(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, l CloseLine) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO shift_close_lines
		    (tenant_id, shift_id, nozzle_id, opening_reading, closing_reading,
		     litres_sold, unit_price, expected_value)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, tenantID, l.ShiftID, l.NozzleID, l.OpeningReading, l.ClosingReading,
		l.LitresSold, l.UnitPrice, l.ExpectedValue)
	return err
}

func (r *Repo) ListCloseLines(ctx context.Context, tenantID, shiftID uuid.UUID) ([]CloseLine, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, shift_id, nozzle_id, opening_reading, closing_reading,
		       litres_sold, unit_price, expected_value
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

const cashColumns = `
    id, tenant_id, shift_id, expected_cash, cash_amount, mobile_money_amount,
    card_amount, credit_amount, submitted_total, variance, submitted_by,
    submitted_at, notes
`

func scanCash(row pgx.Row, c *CashSubmission) error {
	return row.Scan(
		&c.ID, &c.TenantID, &c.ShiftID, &c.ExpectedCash, &c.CashAmount, &c.MobileMoneyAmount,
		&c.CardAmount, &c.CreditAmount, &c.SubmittedTotal, &c.Variance, &c.SubmittedBy,
		&c.SubmittedAt, &c.Notes,
	)
}

func (r *Repo) InsertCashSubmission(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CashSubmissionInput) (*CashSubmission, error) {
	var c CashSubmission
	if err := scanCash(tx.QueryRow(ctx, `
		INSERT INTO cash_submissions
		    (tenant_id, shift_id, expected_cash, cash_amount, mobile_money_amount,
		     card_amount, credit_amount, submitted_total, variance, submitted_by, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+cashColumns,
		tenantID, in.ShiftID, in.ExpectedCash, in.CashAmount, in.MobileMoneyAmount,
		in.CardAmount, in.CreditAmount, in.SubmittedTotal, in.Variance, in.SubmittedBy, in.Notes,
	), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// TankSales is a shift's metered litres-sold aggregated up to one tank — the
// bridge from per-nozzle close lines to a per-tank inventory draw-down.
type TankSales struct {
	TankID     uuid.UUID
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
