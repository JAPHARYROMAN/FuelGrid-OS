package fleet

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type OdometerReading struct {
	ID               uuid.UUID
	VehicleID        uuid.UUID
	AuthorizationID  *uuid.UUID
	StationID        *uuid.UUID
	Reading          string
	DistanceSince    *string
	ValidationStatus string
	Note             *string
	CapturedAt       time.Time
}

// RecordOdometer validates and stores an odometer reading. A non-increasing
// reading is flagged 'warning' (or 'override' when overridden); distance since
// the prior reading is computed in SQL.
func (r *Repo) RecordOdometer(ctx context.Context, tenantID, vehicleID uuid.UUID, authorizationID, stationID *uuid.UUID, reading string, note *string, override bool, recordedBy uuid.UUID) (*OdometerReading, error) {
	var last *float64
	if err := r.pool.QueryRow(ctx, `
		SELECT max(reading) FROM vehicle_odometer_readings WHERE tenant_id = $1 AND vehicle_id = $2
	`, tenantID, vehicleID).Scan(&last); err != nil {
		return nil, err
	}
	// Determine validation status from monotonicity (comparison only; the
	// stored reading value stays a SQL numeric).
	status := "valid"
	var cur float64
	if err := r.pool.QueryRow(ctx, `SELECT $1::numeric`, reading).Scan(&cur); err != nil {
		return nil, err
	}
	if last != nil && cur <= *last {
		if override {
			status = "override"
		} else {
			status = "warning"
		}
	}
	var o OdometerReading
	err := r.pool.QueryRow(ctx, `
		INSERT INTO vehicle_odometer_readings
		    (tenant_id, vehicle_id, authorization_id, station_id, reading, distance_since, validation_status, note, recorded_by)
		VALUES ($1, $2, $3, $4, $5::numeric,
		        (SELECT $5::numeric - max(reading) FROM vehicle_odometer_readings WHERE tenant_id = $1 AND vehicle_id = $2),
		        $6, $7, $8)
		RETURNING id, vehicle_id, authorization_id, station_id, reading::text, distance_since::text, validation_status, note, captured_at
	`, tenantID, vehicleID, authorizationID, stationID, reading, status, note, recordedBy).Scan(
		&o.ID, &o.VehicleID, &o.AuthorizationID, &o.StationID, &o.Reading, &o.DistanceSince, &o.ValidationStatus, &o.Note, &o.CapturedAt,
	)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *Repo) ListOdometerReadings(ctx context.Context, tenantID, vehicleID uuid.UUID) ([]OdometerReading, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, vehicle_id, authorization_id, station_id, reading::text, distance_since::text, validation_status, note, captured_at
		FROM vehicle_odometer_readings WHERE tenant_id = $1 AND vehicle_id = $2 ORDER BY captured_at DESC
	`, tenantID, vehicleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OdometerReading{}
	for rows.Next() {
		var o OdometerReading
		if err := rows.Scan(&o.ID, &o.VehicleID, &o.AuthorizationID, &o.StationID, &o.Reading, &o.DistanceSince, &o.ValidationStatus, &o.Note, &o.CapturedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// VehicleConsumption is a per-vehicle consumption summary over a period: the
// authorized spend (from fulfilled authorizations) and odometer distance.
type VehicleConsumption struct {
	VehicleID     uuid.UUID
	Registration  string
	Fuelings      int
	AmountTotal   string
	OdometerStart *string
	OdometerEnd   *string
	Distance      *string
}

// FleetConsumption reports per-vehicle consumption for a customer over a date
// range, from fulfilled fuel authorizations and odometer readings.
func (r *Repo) FleetConsumption(ctx context.Context, tenantID, customerID uuid.UUID, from, to time.Time) ([]VehicleConsumption, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT v.id, v.registration,
		       COALESCE(a.fuelings, 0),
		       COALESCE(a.amount, 0)::text,
		       o.min_reading::text, o.max_reading::text,
		       (o.max_reading - o.min_reading)::text
		FROM customer_vehicles v
		LEFT JOIN (
		    SELECT vehicle_id, count(*) AS fuelings, SUM(approved_amount) AS amount
		    FROM fuel_authorizations
		    WHERE tenant_id = $1 AND status = 'fulfilled' AND created_at::date BETWEEN $3 AND $4
		    GROUP BY vehicle_id
		) a ON a.vehicle_id = v.id
		LEFT JOIN (
		    SELECT vehicle_id, min(reading) AS min_reading, max(reading) AS max_reading
		    FROM vehicle_odometer_readings
		    WHERE tenant_id = $1 AND captured_at::date BETWEEN $3 AND $4
		    GROUP BY vehicle_id
		) o ON o.vehicle_id = v.id
		WHERE v.tenant_id = $1 AND v.customer_id = $2
		ORDER BY v.registration
	`, tenantID, customerID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []VehicleConsumption{}
	for rows.Next() {
		var c VehicleConsumption
		if err := rows.Scan(&c.VehicleID, &c.Registration, &c.Fuelings, &c.AmountTotal, &c.OdometerStart, &c.OdometerEnd, &c.Distance); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
