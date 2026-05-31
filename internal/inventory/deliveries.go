package inventory

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Delivery is one recorded fuel intake into a tank. Receiving it posts a
// matching +volume 'delivery' movement to the stock ledger.
type Delivery struct {
	ID                     uuid.UUID
	TenantID               uuid.UUID
	TankID                 uuid.UUID
	SupplierRef            *string
	SupplierID             *uuid.UUID
	PurchaseOrderID        *uuid.UUID
	POLineID               *uuid.UUID
	VolumeLitres           float64
	DipBeforeLitres        *float64
	DipAfterLitres         *float64
	DipVarianceLitres      *float64
	LineUnitPrice          *string
	FreightAmount          string
	DutyAmount             string
	LeviesAmount           string
	LandedCostTotal        *string
	LandedCostPerLitre     *string
	MatchStatus            string
	QuantityVarianceLitres *float64
	ReceivedBy             uuid.UUID
	ReceivedAt             time.Time
	Notes                  *string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// ReceiveInput is the data needed to record a delivery. DipVarianceLitres is
// the caller-computed declared−(after−before), stored alongside the dips.
type ReceiveInput struct {
	TankID            uuid.UUID
	SupplierRef       *string
	VolumeLitres      float64
	DipBeforeLitres   *float64
	DipAfterLitres    *float64
	DipVarianceLitres *float64
	ReceivedBy        uuid.UUID
	Notes             *string
}

// GoodsReceiptInput records a PO-backed receipt. Money/cost fields are
// decimal strings and are cast to numeric by Postgres so the app never has to
// carry currency through float64.
type GoodsReceiptInput struct {
	TankID            uuid.UUID
	PurchaseOrderID   uuid.UUID
	POLineID          uuid.UUID
	VolumeLitres      float64
	DipBeforeLitres   *float64
	DipAfterLitres    *float64
	DipVarianceLitres *float64
	LineUnitPrice     *string
	FreightAmount     string
	DutyAmount        string
	LeviesAmount      string
	ReceivedBy        uuid.UUID
	Notes             *string
}

type GoodsReceiptResult struct {
	Delivery               *Delivery
	Movement               *Movement
	PurchaseOrderStatus    string
	QuantityDiscrepancy    bool
	QuantityVarianceLitres float64
}

var (
	ErrPurchaseOrderNotReceivable = errors.New("inventory: purchase order is not receivable")
	ErrPOLineNotFound             = errors.New("inventory: purchase order line not found")
	ErrReceiptTankMismatch        = errors.New("inventory: receipt tank does not match purchase order")
	ErrOverReceipt                = errors.New("inventory: receipt exceeds the ordered quantity")
)

// receivingTolerancePct is the measurement allowance applied when receiving
// against a purchase order — deliberately distinct from a product's
// loss_tolerance_percent (an evaporation/reconciliation allowance that may be
// several percent). It bounds both the over-receipt cap and PO auto-completion,
// so a supplier shortfall beyond this tight margin neither posts excess stock
// nor closes the PO as fully received (PROC-07/13).
const receivingTolerancePct = 0.5

const deliveryColumns = `
    id, tenant_id, tank_id, supplier_ref, supplier_id, purchase_order_id, po_line_id, volume_litres,
    dip_before_litres, dip_after_litres, dip_variance_litres,
    line_unit_price::text, freight_amount::text, duty_amount::text, levies_amount::text,
    landed_cost_total::text, landed_cost_per_litre::text,
    match_status, quantity_variance_litres,
    received_by, received_at, notes, created_at, updated_at
`

func scanDelivery(row pgx.Row, d *Delivery) error {
	return row.Scan(
		&d.ID, &d.TenantID, &d.TankID, &d.SupplierRef, &d.SupplierID, &d.PurchaseOrderID, &d.POLineID, &d.VolumeLitres,
		&d.DipBeforeLitres, &d.DipAfterLitres, &d.DipVarianceLitres,
		&d.LineUnitPrice, &d.FreightAmount, &d.DutyAmount, &d.LeviesAmount,
		&d.LandedCostTotal, &d.LandedCostPerLitre,
		&d.MatchStatus, &d.QuantityVarianceLitres,
		&d.ReceivedBy, &d.ReceivedAt, &d.Notes, &d.CreatedAt, &d.UpdatedAt,
	)
}

// ReceiveDelivery records a delivery and posts its +volume 'delivery' stock
// movement inside the caller's tx, returning both. The movement carries
// source_ref -> the delivery, so book stock traces back to the intake. The
// post inherits PostMovement's guard, so a tank with no opening balance yields
// ErrNoOpeningBalance and the whole receive rolls back.
func (r *Repo) ReceiveDelivery(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in ReceiveInput) (*Delivery, *Movement, error) {
	var d Delivery
	if err := scanDelivery(tx.QueryRow(ctx, `
		INSERT INTO deliveries
		    (tenant_id, tank_id, supplier_ref, volume_litres,
		     dip_before_litres, dip_after_litres, dip_variance_litres,
		     received_by, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+deliveryColumns,
		tenantID, in.TankID, in.SupplierRef, in.VolumeLitres,
		in.DipBeforeLitres, in.DipAfterLitres, in.DipVarianceLitres,
		in.ReceivedBy, in.Notes,
	), &d); err != nil {
		return nil, nil, err
	}

	srcType := "delivery"
	m, err := r.PostMovement(ctx, tx, tenantID, PostInput{
		TankID:        in.TankID,
		MovementType:  TypeDelivery,
		SourceRefType: &srcType,
		SourceRefID:   &d.ID,
		// MD-1 boundary: VolumeLitres is still an upstream float (delivery volume
		// input). Format to 3-decimal numeric text; the float source is a later wave.
		Litres:     strconv.FormatFloat(in.VolumeLitres, 'f', 3, 64),
		RecordedBy: in.ReceivedBy,
		Notes:      in.Notes,
	})
	if err != nil {
		return nil, nil, err
	}
	return &d, m, nil
}

// ReceiveGoodsReceipt records a PO-backed receipt, updates the PO line's
// cumulative received litres, advances the PO status, and posts the attributed
// stock movement in one transaction.
func (r *Repo) ReceiveGoodsReceipt(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in GoodsReceiptInput) (*GoodsReceiptResult, error) {
	type lineSnapshot struct {
		SupplierID     uuid.UUID
		StationID      uuid.UUID
		ProductID      uuid.UUID
		UnitPrice      string
		OrderedLitres  float64
		ReceivedLitres float64
		Status         string
	}

	var snap lineSnapshot
	err := tx.QueryRow(ctx, `
		SELECT po.supplier_id, po.station_id, pol.product_id, pol.unit_price::text,
		       pol.ordered_litres, pol.received_litres, po.status
		FROM purchase_order_lines pol
		JOIN purchase_orders po ON po.id = pol.purchase_order_id AND po.tenant_id = pol.tenant_id
		WHERE pol.tenant_id = $1 AND po.id = $2 AND pol.id = $3
		FOR UPDATE OF po, pol
	`, tenantID, in.PurchaseOrderID, in.POLineID).Scan(
		&snap.SupplierID, &snap.StationID, &snap.ProductID, &snap.UnitPrice,
		&snap.OrderedLitres, &snap.ReceivedLitres, &snap.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPOLineNotFound
	}
	if err != nil {
		return nil, err
	}
	if snap.Status != "confirmed" && snap.Status != "partially_received" {
		return nil, ErrPurchaseOrderNotReceivable
	}

	var tankStationID, tankProductID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT station_id, product_id
		FROM tanks
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, in.TankID).Scan(&tankStationID, &tankProductID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrReceiptTankMismatch
	}
	if err != nil {
		return nil, err
	}
	if tankStationID != snap.StationID || tankProductID != snap.ProductID {
		return nil, ErrReceiptTankMismatch
	}

	lineUnitPrice := snap.UnitPrice
	if in.LineUnitPrice != nil && *in.LineUnitPrice != "" {
		lineUnitPrice = *in.LineUnitPrice
	}
	freight := defaultDecimal(in.FreightAmount)
	duty := defaultDecimal(in.DutyAmount)
	levies := defaultDecimal(in.LeviesAmount)

	newReceived := snap.ReceivedLitres + in.VolumeLitres
	variance := newReceived - snap.OrderedLitres
	toleranceLitres := snap.OrderedLitres * receivingTolerancePct / 100
	// PROC-07: never post stock past the ordered quantity (beyond the receiving
	// tolerance). A genuine over-delivery must be ordered for explicitly rather
	// than slipped in on a receipt with only an advisory flag.
	if newReceived > snap.OrderedLitres+toleranceLitres {
		return nil, ErrOverReceipt
	}
	matchStatus := "matched"
	if variance < -toleranceLitres {
		matchStatus = "short"
	} else if variance > toleranceLitres {
		matchStatus = "over"
	}
	quantityDiscrepancy := matchStatus != "matched"

	var d Delivery
	if err := scanDelivery(tx.QueryRow(ctx, `
		INSERT INTO deliveries
		    (tenant_id, tank_id, supplier_id, purchase_order_id, po_line_id, volume_litres,
		     dip_before_litres, dip_after_litres, dip_variance_litres,
		     received_by, notes, line_unit_price, freight_amount, duty_amount, levies_amount,
		     landed_cost_total, landed_cost_per_litre, match_status, quantity_variance_litres)
		VALUES ($1, $2, $3, $4, $5, $6,
		        $7, $8, $9, $10, $11,
		        $12::numeric, $13::numeric, $14::numeric, $15::numeric,
		        ROUND(($12::numeric * $6::numeric) + $13::numeric + $14::numeric + $15::numeric, 2),
		        ROUND((($12::numeric * $6::numeric) + $13::numeric + $14::numeric + $15::numeric) / NULLIF($6::numeric, 0), 4),
		        $16, $17)
		RETURNING `+deliveryColumns,
		tenantID, in.TankID, snap.SupplierID, in.PurchaseOrderID, in.POLineID, in.VolumeLitres,
		in.DipBeforeLitres, in.DipAfterLitres, in.DipVarianceLitres,
		in.ReceivedBy, in.Notes, lineUnitPrice, freight, duty, levies, matchStatus, variance,
	), &d); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE purchase_order_lines
		SET received_litres = received_litres + $4
		WHERE tenant_id = $1 AND purchase_order_id = $2 AND id = $3
	`, tenantID, in.PurchaseOrderID, in.POLineID, in.VolumeLitres); err != nil {
		return nil, err
	}

	status, err := r.advancePurchaseOrderStatus(ctx, tx, tenantID, in.PurchaseOrderID)
	if err != nil {
		return nil, err
	}

	srcType := "delivery"
	m, err := r.PostMovement(ctx, tx, tenantID, PostInput{
		TankID:        in.TankID,
		MovementType:  TypeDelivery,
		SourceRefType: &srcType,
		SourceRefID:   &d.ID,
		// MD-1 boundary: VolumeLitres is still an upstream float (PO receipt
		// volume). Format to 3-decimal numeric text; the float source is a later wave.
		Litres:             strconv.FormatFloat(in.VolumeLitres, 'f', 3, 64),
		SupplierID:         &snap.SupplierID,
		PurchaseOrderID:    &in.PurchaseOrderID,
		LandedCostTotal:    d.LandedCostTotal,
		LandedCostPerLitre: d.LandedCostPerLitre,
		RecordedBy:         in.ReceivedBy,
		Notes:              in.Notes,
	})
	if err != nil {
		return nil, err
	}

	return &GoodsReceiptResult{
		Delivery: dptr(d), Movement: m, PurchaseOrderStatus: status,
		QuantityDiscrepancy: quantityDiscrepancy, QuantityVarianceLitres: variance,
	}, nil
}

func defaultDecimal(v string) string {
	if v == "" {
		return "0"
	}
	return v
}

func dptr(d Delivery) *Delivery { return &d }

func (r *Repo) advancePurchaseOrderStatus(ctx context.Context, tx pgx.Tx, tenantID, poID uuid.UUID) (string, error) {
	var receivedAll, receivedAny bool
	// A line counts as fully received only within the tight receiving tolerance,
	// not the product's (possibly large) loss tolerance — so a real supplier
	// shortfall leaves the PO partially_received instead of auto-completing
	// (PROC-13).
	if err := tx.QueryRow(ctx, `
		SELECT
		    COALESCE(bool_and(received_litres >= ordered_litres - (ordered_litres * $3::numeric / 100)), false),
		    COALESCE(bool_or(received_litres > 0), false)
		FROM purchase_order_lines
		WHERE tenant_id = $1 AND purchase_order_id = $2
	`, tenantID, poID, receivingTolerancePct).Scan(&receivedAll, &receivedAny); err != nil {
		return "", err
	}
	status := "confirmed"
	if receivedAll {
		status = "received"
	} else if receivedAny {
		status = "partially_received"
	}
	var updated string
	err := tx.QueryRow(ctx, `
		UPDATE purchase_orders
		SET status = $3
		WHERE tenant_id = $1 AND id = $2
		RETURNING status
	`, tenantID, poID, status).Scan(&updated)
	return updated, err
}

// ListDeliveriesForTank returns a tank's deliveries, newest received first.
func (r *Repo) ListDeliveriesForTank(ctx context.Context, tenantID, tankID uuid.UUID) ([]Delivery, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+deliveryColumns+`
		FROM deliveries
		WHERE tenant_id = $1 AND tank_id = $2
		ORDER BY received_at DESC
	`, tenantID, tankID)
	if err != nil {
		return nil, err
	}
	return collectDeliveries(rows)
}

// ListDeliveriesForTankPage returns a page of a tank's deliveries, newest
// received first with id as a deterministic tiebreaker so paging is stable.
func (r *Repo) ListDeliveriesForTankPage(ctx context.Context, tenantID, tankID uuid.UUID, limit, offset int) ([]Delivery, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+deliveryColumns+`
		FROM deliveries
		WHERE tenant_id = $1 AND tank_id = $2
		ORDER BY received_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`, tenantID, tankID, limit, offset)
	if err != nil {
		return nil, err
	}
	return collectDeliveries(rows)
}

func (r *Repo) GetDelivery(ctx context.Context, tenantID, id uuid.UUID) (*Delivery, error) {
	var d Delivery
	err := scanDelivery(r.pool.QueryRow(ctx, `
		SELECT `+deliveryColumns+`
		FROM deliveries
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id), &d)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ListDeliveriesForStation returns every delivery into the station's tanks,
// newest received first.
func (r *Repo) ListDeliveriesForStation(ctx context.Context, tenantID, stationID uuid.UUID) ([]Delivery, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+prefixedDeliveryColumns+`
		FROM deliveries d
		JOIN tanks t ON t.id = d.tank_id
		WHERE d.tenant_id = $1 AND t.station_id = $2
		ORDER BY d.received_at DESC
	`, tenantID, stationID)
	if err != nil {
		return nil, err
	}
	return collectDeliveries(rows)
}

// ListDeliveriesForStationPage returns a page of every delivery into the
// station's tanks, newest received first with id as a deterministic tiebreaker
// so paging is stable.
func (r *Repo) ListDeliveriesForStationPage(ctx context.Context, tenantID, stationID uuid.UUID, limit, offset int) ([]Delivery, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+prefixedDeliveryColumns+`
		FROM deliveries d
		JOIN tanks t ON t.id = d.tank_id
		WHERE d.tenant_id = $1 AND t.station_id = $2
		ORDER BY d.received_at DESC, d.id DESC
		LIMIT $3 OFFSET $4
	`, tenantID, stationID, limit, offset)
	if err != nil {
		return nil, err
	}
	return collectDeliveries(rows)
}

// prefixedDeliveryColumns is deliveryColumns qualified to the d alias for the
// join in ListDeliveriesForStation.
const prefixedDeliveryColumns = `
    d.id, d.tenant_id, d.tank_id, d.supplier_ref, d.supplier_id, d.purchase_order_id, d.po_line_id, d.volume_litres,
    d.dip_before_litres, d.dip_after_litres, d.dip_variance_litres,
    d.line_unit_price::text, d.freight_amount::text, d.duty_amount::text, d.levies_amount::text,
    d.landed_cost_total::text, d.landed_cost_per_litre::text,
    d.match_status, d.quantity_variance_litres,
    d.received_by, d.received_at, d.notes, d.created_at, d.updated_at
`

func collectDeliveries(rows pgx.Rows) ([]Delivery, error) {
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		var d Delivery
		if err := scanDelivery(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
