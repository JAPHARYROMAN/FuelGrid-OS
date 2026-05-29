package procurement

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type SupplierBalance struct {
	SupplierID        uuid.UUID
	SupplierName      string
	OutstandingAmount string
	InvoiceCount      int
}

type PriceTrendPoint struct {
	SupplierID         uuid.UUID
	SupplierName       string
	ProductID          uuid.UUID
	ProductName        string
	ReceivedAt         time.Time
	LandedCostPerLitre string
}

func (r *Repo) OpenPurchaseOrdersForStation(ctx context.Context, tenantID, stationID uuid.UUID) ([]PurchaseOrder, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+purchaseOrderColumns+`
		FROM purchase_orders
		WHERE tenant_id = $1 AND station_id = $2
		  AND status IN ('draft', 'submitted', 'confirmed', 'partially_received')
		ORDER BY expected_delivery_date NULLS LAST, created_at DESC
	`, tenantID, stationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PurchaseOrder{}
	for rows.Next() {
		var po PurchaseOrder
		if err := scanPurchaseOrder(rows, &po); err != nil {
			return nil, err
		}
		lines, err := r.listPurchaseOrderLines(ctx, r.pool, tenantID, po.ID)
		if err != nil {
			return nil, err
		}
		po.Lines = lines
		out = append(out, po)
	}
	return out, rows.Err()
}

func (r *Repo) SupplierBalancesForStation(ctx context.Context, tenantID, stationID uuid.UUID) ([]SupplierBalance, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.name, COALESCE(SUM(inv.total_amount), 0)::text, count(inv.id)
		FROM supplier_invoices inv
		JOIN suppliers s ON s.id = inv.supplier_id AND s.tenant_id = inv.tenant_id
		WHERE inv.tenant_id = $1 AND inv.station_id = $2 AND inv.status = 'approved'
		GROUP BY s.id, s.name
		ORDER BY s.name
	`, tenantID, stationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierBalance{}
	for rows.Next() {
		var b SupplierBalance
		if err := rows.Scan(&b.SupplierID, &b.SupplierName, &b.OutstandingAmount, &b.InvoiceCount); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *Repo) PriceTrendForStation(ctx context.Context, tenantID, stationID uuid.UUID, limit int) ([]PriceTrendPoint, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.name, p.id, p.name, d.received_at, d.landed_cost_per_litre::text
		FROM deliveries d
		JOIN suppliers s ON s.id = d.supplier_id AND s.tenant_id = d.tenant_id
		JOIN tanks t ON t.id = d.tank_id AND t.tenant_id = d.tenant_id
		JOIN products p ON p.id = t.product_id AND p.tenant_id = t.tenant_id
		WHERE d.tenant_id = $1 AND t.station_id = $2
		  AND d.landed_cost_per_litre IS NOT NULL
		ORDER BY d.received_at DESC
		LIMIT $3
	`, tenantID, stationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PriceTrendPoint{}
	for rows.Next() {
		var p PriceTrendPoint
		if err := rows.Scan(
			&p.SupplierID, &p.SupplierName, &p.ProductID, &p.ProductName,
			&p.ReceivedAt, &p.LandedCostPerLitre,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
