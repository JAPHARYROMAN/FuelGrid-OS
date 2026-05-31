package fleet

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// RecordOdometer validates and stores an odometer reading inside the caller's
// transaction (FLEET-008). The prior maximum, the monotonicity check, and
// distance-since are all evaluated in one SQL statement against SQL numeric —
// no Go float64 ever touches the mileage, and there is no read-then-insert
// gap. A non-increasing reading is flagged 'warning' (or 'override' when the
// caller overrides).
func (r *Repo) RecordOdometer(ctx context.Context, tx pgx.Tx, tenantID, vehicleID uuid.UUID, authorizationID, stationID *uuid.UUID, reading string, note *string, override bool, recordedBy uuid.UUID) (*OdometerReading, error) {
	nonValidStatus := "warning"
	if override {
		nonValidStatus = "override"
	}
	var o OdometerReading
	err := tx.QueryRow(ctx, `
		INSERT INTO vehicle_odometer_readings
		    (tenant_id, vehicle_id, authorization_id, station_id, reading, distance_since, validation_status, note, recorded_by)
		SELECT $1, $2, $3, $4, $5::numeric,
		       $5::numeric - prev.max_reading,
		       CASE WHEN prev.max_reading IS NOT NULL AND $5::numeric <= prev.max_reading
		            THEN $6 ELSE 'valid' END,
		       $7, $8
		FROM (
		    SELECT max(reading) AS max_reading
		    FROM vehicle_odometer_readings WHERE tenant_id = $1 AND vehicle_id = $2
		) prev
		RETURNING id, vehicle_id, authorization_id, station_id, reading::text, distance_since::text, validation_status, note, captured_at
	`, tenantID, vehicleID, authorizationID, stationID, reading, nonValidStatus, note, recordedBy).Scan(
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

// ListOdometerReadingsPage is the paginated variant of ListOdometerReadings
// (REL-REPO). captured_at is not unique, so id is appended as a tiebreaker.
func (r *Repo) ListOdometerReadingsPage(ctx context.Context, tenantID, vehicleID uuid.UUID, limit, offset int) ([]OdometerReading, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, vehicle_id, authorization_id, station_id, reading::text, distance_since::text, validation_status, note, captured_at
		FROM vehicle_odometer_readings WHERE tenant_id = $1 AND vehicle_id = $2
		ORDER BY captured_at DESC, id
		LIMIT $3 OFFSET $4
	`, tenantID, vehicleID, limit, offset)
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
