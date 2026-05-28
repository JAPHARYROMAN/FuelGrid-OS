package inventory

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Delivery is one recorded fuel intake into a tank. Receiving it posts a
// matching +volume 'delivery' movement to the stock ledger.
type Delivery struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	TankID            uuid.UUID
	SupplierRef       *string
	VolumeLitres      float64
	DipBeforeLitres   *float64
	DipAfterLitres    *float64
	DipVarianceLitres *float64
	ReceivedBy        uuid.UUID
	ReceivedAt        time.Time
	Notes             *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
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

const deliveryColumns = `
    id, tenant_id, tank_id, supplier_ref, volume_litres,
    dip_before_litres, dip_after_litres, dip_variance_litres,
    received_by, received_at, notes, created_at, updated_at
`

func scanDelivery(row pgx.Row, d *Delivery) error {
	return row.Scan(
		&d.ID, &d.TenantID, &d.TankID, &d.SupplierRef, &d.VolumeLitres,
		&d.DipBeforeLitres, &d.DipAfterLitres, &d.DipVarianceLitres,
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
		Litres:        in.VolumeLitres,
		RecordedBy:    in.ReceivedBy,
		Notes:         in.Notes,
	})
	if err != nil {
		return nil, nil, err
	}
	return &d, m, nil
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

// prefixedDeliveryColumns is deliveryColumns qualified to the d alias for the
// join in ListDeliveriesForStation.
const prefixedDeliveryColumns = `
    d.id, d.tenant_id, d.tank_id, d.supplier_ref, d.volume_litres,
    d.dip_before_litres, d.dip_after_litres, d.dip_variance_litres,
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
