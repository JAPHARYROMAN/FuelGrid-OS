package inventory

import (
	"context"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// PeriodTotals is a tank's ledger activity over a reconciliation period —
// the movements with seq greater than the prior reconciliation's watermark.
// Sales is reported positive (the negated stock-out litres).
type PeriodTotals struct {
	OpeningTotal     float64 // 'opening' movements in the period (genesis only)
	DeliveriesTotal  float64
	SalesTotal       float64
	AdjustmentsTotal float64
	ThroughSeq       int64 // max seq seen (== fromSeq when the period is empty)
}

// PeriodTotalsSince sums a tank's movements grouped by type for seq > fromSeq,
// the activity since the last reconciled watermark. It reads through the given
// Querier so it can run on the pool or inside a transaction (e.g. right after
// an adjustment posts, to recompute the draft).
func (r *Repo) PeriodTotalsSince(ctx context.Context, q database.Querier, tenantID, tankID uuid.UUID, fromSeq int64) (PeriodTotals, error) {
	var t PeriodTotals
	err := q.QueryRow(ctx, `
		SELECT
		    COALESCE(SUM(litres) FILTER (WHERE movement_type = 'opening'), 0),
		    COALESCE(SUM(litres) FILTER (WHERE movement_type = 'delivery'), 0),
		    COALESCE(-SUM(litres) FILTER (WHERE movement_type = 'sales'), 0),
		    COALESCE(SUM(litres) FILTER (WHERE movement_type = 'adjustment'), 0),
		    COALESCE(MAX(seq), $3)
		FROM stock_movements
		WHERE tenant_id = $1 AND tank_id = $2 AND seq > $3
	`, tenantID, tankID, fromSeq).Scan(
		&t.OpeningTotal, &t.DeliveriesTotal, &t.SalesTotal, &t.AdjustmentsTotal, &t.ThroughSeq,
	)
	return t, err
}

// MaxSeqForTank returns the highest movement seq for a tank (0 when none),
// the watermark a reconciliation seals through.
func (r *Repo) MaxSeqForTank(ctx context.Context, q database.Querier, tenantID, tankID uuid.UUID) (int64, error) {
	var seq int64
	err := q.QueryRow(ctx, `
		SELECT COALESCE(MAX(seq), 0) FROM stock_movements
		WHERE tenant_id = $1 AND tank_id = $2
	`, tenantID, tankID).Scan(&seq)
	return seq, err
}
