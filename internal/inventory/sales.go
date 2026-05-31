package inventory

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// SaleLine is a tank's metered litres-sold for a shift — the input to a sales
// draw-down. LitresSold is the positive quantity dispensed; PostSalesForShift
// posts it as a negative (stock-out) movement.
type SaleLine struct {
	TankID     uuid.UUID
	LitresSold float64
}

// SalesPostedForShift reports whether a shift's sales have already been posted
// to the ledger — the idempotency check that keeps re-approval and replay from
// double-counting.
func (r *Repo) SalesPostedForShift(ctx context.Context, q database.Querier, tenantID, shiftID uuid.UUID) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM stock_movements
			WHERE tenant_id = $1 AND source_ref_type = 'shift' AND source_ref_id = $2
			  AND movement_type = 'sales' AND status = 'posted'
		)
	`, tenantID, shiftID).Scan(&exists)
	return exists, err
}

// PostSalesForShift posts one 'sales' stock-out movement per tank for an
// approved shift, inside the caller's tx, and returns the movements it posted.
//
// It is idempotent: if the shift's sales are already on the ledger it posts
// nothing. A tank with no opening balance is skipped (returned in skipped, not
// posted) rather than failing — inventory is additive on top of Phase 3, so a
// shift can be approved before its tanks are onboarded. Those skipped sales
// are the documented backfill case: once the tank has an opening balance,
// re-run PostSalesForShift for the shift to post them (the unique index +
// idempotency check keep that safe).
func (r *Repo) PostSalesForShift(ctx context.Context, tx pgx.Tx, tenantID, shiftID, recordedBy uuid.UUID, lines []SaleLine) (posted []Movement, skipped []uuid.UUID, err error) {
	already, err := r.SalesPostedForShift(ctx, tx, tenantID, shiftID)
	if err != nil {
		return nil, nil, err
	}
	if already {
		return nil, nil, nil
	}

	srcType := "shift"
	sid := shiftID
	for _, ln := range lines {
		if ln.LitresSold == 0 {
			continue
		}
		has, err := r.hasOpeningTx(ctx, tx, tenantID, ln.TankID)
		if err != nil {
			return nil, nil, err
		}
		if !has {
			skipped = append(skipped, ln.TankID)
			continue
		}
		m, err := r.PostMovement(ctx, tx, tenantID, PostInput{
			TankID:        ln.TankID,
			MovementType:  TypeSales,
			SourceRefType: &srcType,
			SourceRefID:   &sid,
			// MD-1 boundary: LitresSold is still an upstream float (metered
			// shift-close litres). Format the negated stock-out to 3-decimal
			// numeric text here; the float source is retyped in a later wave.
			Litres:     strconv.FormatFloat(-ln.LitresSold, 'f', 3, 64),
			RecordedBy: recordedBy,
		})
		if err != nil {
			return nil, nil, err
		}
		posted = append(posted, *m)
	}
	return posted, skipped, nil
}
