package fleet

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Vehicle struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	CustomerID       uuid.UUID
	Registration     string
	FleetNumber      *string
	VIN              *string
	VehicleType      *string
	DefaultProductID *uuid.UUID
	TankCapacity     *string
	OdometerRequired bool
	Status           string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type VehicleInput struct {
	CustomerID       uuid.UUID
	Registration     string
	FleetNumber      *string
	VIN              *string
	VehicleType      *string
	DefaultProductID *uuid.UUID
	TankCapacity     *string
	OdometerRequired bool
}

const vehicleColumns = `
    id, tenant_id, customer_id, registration, fleet_number, vin, vehicle_type,
    default_product_id, tank_capacity::text, odometer_required, status, created_at, updated_at
`

func scanVehicle(row pgx.Row, v *Vehicle) error {
	return row.Scan(
		&v.ID, &v.TenantID, &v.CustomerID, &v.Registration, &v.FleetNumber, &v.VIN, &v.VehicleType,
		&v.DefaultProductID, &v.TankCapacity, &v.OdometerRequired, &v.Status, &v.CreatedAt, &v.UpdatedAt,
	)
}

func (r *Repo) CreateVehicle(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in VehicleInput) (*Vehicle, error) {
	var v Vehicle
	err := scanVehicle(tx.QueryRow(ctx, `
		INSERT INTO customer_vehicles
		    (tenant_id, customer_id, registration, fleet_number, vin, vehicle_type, default_product_id, tank_capacity, odometer_required)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::numeric, $9)
		RETURNING `+vehicleColumns,
		tenantID, in.CustomerID, in.Registration, in.FleetNumber, in.VIN, in.VehicleType,
		in.DefaultProductID, nullableMoney(deref(in.TankCapacity)), in.OdometerRequired,
	), &v)
	if isUniqueViolation(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *Repo) GetVehicle(ctx context.Context, tenantID, id uuid.UUID) (*Vehicle, error) {
	var v Vehicle
	err := scanVehicle(r.pool.QueryRow(ctx, `SELECT `+vehicleColumns+` FROM customer_vehicles WHERE tenant_id = $1 AND id = $2`, tenantID, id), &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *Repo) ListVehicles(ctx context.Context, tenantID, customerID uuid.UUID) ([]Vehicle, error) {
	var custFilter *uuid.UUID
	if customerID != uuid.Nil {
		custFilter = &customerID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+vehicleColumns+` FROM customer_vehicles
		WHERE tenant_id = $1 AND ($2::uuid IS NULL OR customer_id = $2)
		ORDER BY registration
	`, tenantID, custFilter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Vehicle{}
	for rows.Next() {
		var v Vehicle
		if err := scanVehicle(rows, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repo) SetVehicleStatus(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, status string) (*Vehicle, error) {
	var v Vehicle
	err := scanVehicle(tx.QueryRow(ctx, `
		UPDATE customer_vehicles SET status = $3 WHERE tenant_id = $1 AND id = $2
		RETURNING `+vehicleColumns, tenantID, id, status), &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}
