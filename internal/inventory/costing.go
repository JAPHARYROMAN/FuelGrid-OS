package inventory

import (
	"context"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// WeightedAverageCost returns a tank's CUMULATIVE (lifetime) weighted-average
// landed cost per litre — the cost basis Phase 6 values COGS and stock at. It
// is the litre-weighted average of the landed cost of ALL the tank's posted,
// non-superseded delivery (stock-in) movements over the tank's whole history
// (Phase-5 receipts).
//
// IMPORTANT — this is NOT a perpetual ("moving") average: consumed (sold) stock
// never lowers the basis, so the figure does not decrement as litres are sold.
// It equals a true moving average only while landed cost per litre is constant
// across a tank's deliveries; it drifts from it as cost rises or falls over the
// tank's life. See docs/costing-policy.md for the exact policy, its limitation,
// and when it is accurate.
//
// found is false when the tank has no costed deliveries. Returned as a decimal
// string (numeric in the DB), never float.
func (r *Repo) WeightedAverageCost(ctx context.Context, q database.Querier, tenantID, tankID uuid.UUID) (cost string, found bool, err error) {
	var v *string
	if err = q.QueryRow(ctx, `
		SELECT (SUM(litres * landed_cost_per_litre) / NULLIF(SUM(litres), 0))::text
		FROM stock_movements
		WHERE tenant_id = $1 AND tank_id = $2 AND movement_type = 'delivery'
		  AND landed_cost_per_litre IS NOT NULL AND litres > 0
		  AND status = 'posted' AND supersedes_id IS NULL
	`, tenantID, tankID).Scan(&v); err != nil {
		return "", false, err
	}
	if v == nil {
		return "", false, nil
	}
	return *v, true, nil
}

// AverageLandedCostForStationProduct returns the CUMULATIVE (lifetime)
// weighted-average landed cost per litre across a station's tanks holding a
// product — the cost basis the below-cost price guard checks a selling price
// against. Like WeightedAverageCost this is a lifetime average over posted,
// non-superseded deliveries; it does not decrement as stock is sold. See
// docs/costing-policy.md.
func (r *Repo) AverageLandedCostForStationProduct(ctx context.Context, tenantID, stationID, productID uuid.UUID) (cost string, found bool, err error) {
	var v *string
	if err = r.pool.QueryRow(ctx, `
		SELECT (SUM(sm.litres * sm.landed_cost_per_litre) / NULLIF(SUM(sm.litres), 0))::text
		FROM stock_movements sm
		JOIN tanks t ON t.id = sm.tank_id AND t.tenant_id = sm.tenant_id
		WHERE sm.tenant_id = $1 AND t.station_id = $2 AND t.product_id = $3
		  AND sm.movement_type = 'delivery' AND sm.landed_cost_per_litre IS NOT NULL AND sm.litres > 0
		  AND sm.status = 'posted' AND sm.supersedes_id IS NULL
	`, tenantID, stationID, productID).Scan(&v); err != nil {
		return "", false, err
	}
	if v == nil {
		return "", false, nil
	}
	return *v, true, nil
}
