package operations

// Handover chain (Mobile Attendant App, Phase 0). A station's shifts hand
// over to each other: a new shift must not open while a prior shift is
// closed-but-not-approved, and each nozzle's expected opening meter is the
// previous shift's FINAL APPROVED closing (the reading-verification figure
// when one exists, the raw closing otherwise).

import (
	"context"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// ClosedUnapprovedShiftIDsForStation returns the station's shifts stuck in
// 'closed' (not yet approved) — the handover gate for opening a new shift.
// It runs through any Querier so the open handler can check inside the tx
// that inserts the shift.
func (r *Repo) ClosedUnapprovedShiftIDsForStation(ctx context.Context, q database.Querier, tenantID, stationID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT id FROM shifts
		WHERE tenant_id = $1 AND station_id = $2 AND status = 'closed'
		ORDER BY opened_at, id
	`, tenantID, stationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ExpectedOpening is one assigned nozzle's expected opening meter for a
// shift: the previous shift's final approved closing (the verification's
// final_approved_reading when the closing was verified, the raw closing
// otherwise). ExpectedReading is nil when the nozzle has no prior closing at
// the station. All figures are exact decimal strings (numeric(14,3) ::text).
type ExpectedOpening struct {
	AssignmentID    uuid.UUID
	NozzleID        uuid.UUID
	AttendantID     uuid.UUID
	ExpectedReading *string
	// Source is "verified" when the expected figure came from a reading
	// verification, "raw" when it fell back to the unverified closing; nil
	// when there is no prior closing at all.
	Source          *string
	SourceShiftID   *uuid.UUID
	SourceReadingID *uuid.UUID
}

// expectedOpeningSQL resolves, for one nozzle, the latest ACTIVE closing
// reading recorded on any earlier shift at the station ($3) — earlier meaning
// (opened_at, id) before the current shift's ($4, $5) — preferring the
// verification's final approved figure over the raw closing.
const expectedOpeningSQL = `
	SELECT m.id, m.shift_id,
	       COALESCE(v.final_approved_reading, m.reading)::numeric(14,3)::text,
	       (v.id IS NOT NULL)
	FROM meter_readings m
	JOIN shifts prev ON prev.tenant_id = m.tenant_id AND prev.id = m.shift_id
	LEFT JOIN reading_verifications v
	       ON v.tenant_id = m.tenant_id AND v.reading_id = m.id
	WHERE m.tenant_id = $1 AND m.nozzle_id = $2
	  AND m.reading_type = 'closing' AND m.status = 'active'
	  AND prev.station_id = $3
	  AND (prev.opened_at, prev.id) < ($4, $5)
	ORDER BY prev.opened_at DESC, prev.id DESC, m.recorded_at DESC, m.id DESC
	LIMIT 1
`

// ExpectedOpeningsForShift returns one row per nozzle assignment on the
// shift, each carrying the previous shift's final approved closing for that
// nozzle (nil when the nozzle has never closed at this station before).
func (r *Repo) ExpectedOpeningsForShift(ctx context.Context, tenantID uuid.UUID, shift *Shift) ([]ExpectedOpening, error) {
	assignments, err := r.ListNozzleAssignments(ctx, tenantID, shift.ID)
	if err != nil {
		return nil, err
	}
	out := make([]ExpectedOpening, 0, len(assignments))
	for i := range assignments {
		eo := ExpectedOpening{
			AssignmentID: assignments[i].ID,
			NozzleID:     assignments[i].NozzleID,
			AttendantID:  assignments[i].AttendantID,
		}
		prev, err := r.expectedOpeningForNozzle(ctx, tenantID, shift, assignments[i].NozzleID)
		if err != nil {
			return nil, err
		}
		if prev != nil {
			eo.ExpectedReading = &prev.Reading
			eo.Source = &prev.Source
			eo.SourceShiftID = &prev.ShiftID
			eo.SourceReadingID = &prev.ReadingID
		}
		out = append(out, eo)
	}
	return out, nil
}

// previousClosing is the resolved prior closing behind an expected opening.
type previousClosing struct {
	ReadingID uuid.UUID
	ShiftID   uuid.UUID
	Reading   string
	Source    string
}

func (r *Repo) expectedOpeningForNozzle(ctx context.Context, tenantID uuid.UUID, shift *Shift, nozzleID uuid.UUID) (*previousClosing, error) {
	rows, err := r.pool.Query(ctx, expectedOpeningSQL,
		tenantID, nozzleID, shift.StationID, shift.OpenedAt, shift.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var (
		p        previousClosing
		verified bool
	)
	if err := rows.Scan(&p.ReadingID, &p.ShiftID, &p.Reading, &verified); err != nil {
		return nil, err
	}
	p.Source = "raw"
	if verified {
		p.Source = "verified"
	}
	return &p, rows.Err()
}

// ExpectedOpeningForNozzle returns the nozzle's expected opening for the
// shift — the previous shift's final approved closing as an exact decimal
// string — or nil when the nozzle has no prior closing at the station.
func (r *Repo) ExpectedOpeningForNozzle(ctx context.Context, tenantID uuid.UUID, shift *Shift, nozzleID uuid.UUID) (*string, error) {
	prev, err := r.expectedOpeningForNozzle(ctx, tenantID, shift, nozzleID)
	if err != nil || prev == nil {
		return nil, err
	}
	return &prev.Reading, nil
}

// DecimalLess reports a < b comparing the two decimal strings in SQL numeric
// (no Go float) — the opening-reading floor check against the expected
// opening.
func (r *Repo) DecimalLess(ctx context.Context, a, b string) (bool, error) {
	var less bool
	err := r.pool.QueryRow(ctx, `SELECT $1::numeric < $2::numeric`, a, b).Scan(&less)
	return less, err
}
