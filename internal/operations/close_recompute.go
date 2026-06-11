package operations

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrCloseLineNotFound is returned when a shift has no frozen close line for
// the nozzle (the shift was not closed, or the nozzle had no line).
var ErrCloseLineNotFound = errors.New("operations: shift close line not found")

// RecomputeCloseLineClosing rewrites one frozen close line after a supervisor
// verification corrected its closing meter (Mobile Attendant Phase 0). The
// new closing is bound as an exact decimal string; litres_sold and
// expected_value are recomputed in SQL numeric from the line's own frozen
// opening_reading and unit_price — no Go float. The original attendant
// submission is preserved by the reading_verifications snapshot (and the
// untouched meter_readings row), so the line can be rewritten without losing
// the original facts. Returns ErrCloseLineNotFound when the shift has no line
// for the nozzle.
func (r *Repo) RecomputeCloseLineClosing(ctx context.Context, tx pgx.Tx, tenantID, shiftID, nozzleID uuid.UUID, newClosing string) (*CloseLine, error) {
	l := CloseLine{TenantID: tenantID, ShiftID: shiftID, NozzleID: nozzleID}
	err := tx.QueryRow(ctx, `
		UPDATE shift_close_lines
		SET closing_reading = $4::numeric,
		    litres_sold     = ($4::numeric - opening_reading),
		    expected_value  = (($4::numeric - opening_reading) * unit_price)
		WHERE tenant_id = $1 AND shift_id = $2 AND nozzle_id = $3
		RETURNING id, opening_reading::text, closing_reading::text,
		          litres_sold::text, unit_price::text, expected_value::text
	`, tenantID, shiftID, nozzleID, newClosing).Scan(
		&l.ID, &l.OpeningReading, &l.ClosingReading, &l.LitresSold, &l.UnitPrice, &l.ExpectedValue,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCloseLineNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}
