package readings

// Supervisor verification of closing meter readings — the dual-value model
// (Mobile Attendant App, Phase 0). The attendant's original meter_readings row
// is NEVER mutated: a verification row snapshots the submitted figure, the
// supervisor's figure when they diverge, and the final approved figure that
// downstream money math uses. One verification per reading; a later
// correction of the reading (supersedes chain) leaves the old verification on
// the superseded row and the new ACTIVE reading unverified again.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// Verification is one supervisor decision over one meter reading. All three
// reading figures are exact decimal STRINGS (numeric(14,3) read ::text).
type Verification struct {
	ID                        uuid.UUID
	TenantID                  uuid.UUID
	StationID                 uuid.UUID
	ShiftID                   uuid.UUID
	NozzleID                  uuid.UUID
	ReadingID                 uuid.UUID
	AttendantSubmittedReading string
	SupervisorVerifiedReading *string
	FinalApprovedReading      string
	Status                    string
	Reason                    *string
	VerifiedBy                uuid.UUID
	VerifiedAt                time.Time
}

// VerificationInput carries one verification insert. Submitted/Final (and
// SupervisorVerified when present) are exact decimal strings bound
// $N::numeric.
type VerificationInput struct {
	StationID                 uuid.UUID
	ShiftID                   uuid.UUID
	NozzleID                  uuid.UUID
	ReadingID                 uuid.UUID
	AttendantSubmittedReading string
	SupervisorVerifiedReading *string
	FinalApprovedReading      string
	Status                    string
	Reason                    *string
	VerifiedBy                uuid.UUID
}

const verificationColumns = `
    id, tenant_id, station_id, shift_id, nozzle_id, reading_id,
    attendant_submitted_reading::text, supervisor_verified_reading::text,
    final_approved_reading::text, status, reason, verified_by, verified_at
`

func scanVerification(row pgx.Row, v *Verification) error {
	return row.Scan(
		&v.ID, &v.TenantID, &v.StationID, &v.ShiftID, &v.NozzleID, &v.ReadingID,
		&v.AttendantSubmittedReading, &v.SupervisorVerifiedReading,
		&v.FinalApprovedReading, &v.Status, &v.Reason, &v.VerifiedBy, &v.VerifiedAt,
	)
}

// InsertVerification writes one verification inside the caller's tx. A second
// verification of the same reading trips uq_reading_verifications_reading,
// which the handler maps to 409.
func (r *Repo) InsertVerification(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in VerificationInput) (*Verification, error) {
	var v Verification
	if err := scanVerification(tx.QueryRow(ctx, `
		INSERT INTO reading_verifications
		    (tenant_id, station_id, shift_id, nozzle_id, reading_id,
		     attendant_submitted_reading, supervisor_verified_reading,
		     final_approved_reading, status, reason, verified_by)
		VALUES ($1, $2, $3, $4, $5, $6::numeric, $7::numeric, $8::numeric, $9, $10, $11)
		RETURNING `+verificationColumns,
		tenantID, in.StationID, in.ShiftID, in.NozzleID, in.ReadingID,
		in.AttendantSubmittedReading, in.SupervisorVerifiedReading,
		in.FinalApprovedReading, in.Status, in.Reason, in.VerifiedBy,
	), &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// ListVerificationsForShift returns the shift's verifications, ordered by
// nozzle then verification time for stable output.
func (r *Repo) ListVerificationsForShift(ctx context.Context, tenantID, shiftID uuid.UUID) ([]Verification, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+verificationColumns+`
		FROM reading_verifications
		WHERE tenant_id = $1 AND shift_id = $2
		ORDER BY nozzle_id, verified_at, id
	`, tenantID, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Verification
	for rows.Next() {
		var v Verification
		if err := scanVerification(rows, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetVerificationForReading returns the reading's verification, or
// pgx.ErrNoRows.
func (r *Repo) GetVerificationForReading(ctx context.Context, tenantID, readingID uuid.UUID) (*Verification, error) {
	var v Verification
	if err := scanVerification(r.pool.QueryRow(ctx, `
		SELECT `+verificationColumns+`
		FROM reading_verifications
		WHERE tenant_id = $1 AND reading_id = $2
	`, tenantID, readingID), &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// UnverifiedClosingForShift returns the shift's ACTIVE closing readings that
// have no verification row yet — the work list for the batch-approve endpoint.
func (r *Repo) UnverifiedClosingForShift(ctx context.Context, tenantID, shiftID uuid.UUID) ([]MeterReading, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+meterColumns+`
		FROM meter_readings m
		WHERE m.tenant_id = $1 AND m.shift_id = $2
		  AND m.reading_type = 'closing' AND m.status = 'active'
		  AND NOT EXISTS (
		      SELECT 1 FROM reading_verifications v
		      WHERE v.tenant_id = m.tenant_id AND v.reading_id = m.id
		  )
		ORDER BY m.nozzle_id, m.id
	`, tenantID, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MeterReading
	for rows.Next() {
		var m MeterReading
		if err := scanMeter(rows, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// FinalClosingOverridesForShift maps the shift's ACTIVE closing reading ids
// to their verification's final_approved_reading (exact decimal strings).
// The close snapshot consults this map so a closing corrected BEFORE the
// shift closed still freezes the supervisor's approved figure, not the raw
// submission (the dual-value model's "downstream money math uses the final").
func (r *Repo) FinalClosingOverridesForShift(ctx context.Context, tenantID, shiftID uuid.UUID) (map[uuid.UUID]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT v.reading_id, v.final_approved_reading::text
		FROM reading_verifications v
		JOIN meter_readings m
		  ON m.tenant_id = v.tenant_id AND m.id = v.reading_id
		WHERE v.tenant_id = $1 AND v.shift_id = $2
		  AND m.reading_type = 'closing' AND m.status = 'active'
	`, tenantID, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]string{}
	for rows.Next() {
		var (
			id    uuid.UUID
			final string
		)
		if err := rows.Scan(&id, &final); err != nil {
			return nil, err
		}
		out[id] = final
	}
	return out, rows.Err()
}

// CorrectionReportRow is one non-approved (corrected/rejected) verification in
// a station/date window, denormalized for the Corrections & Variances report:
// who submitted what, what the supervisor finalized, the SQL-computed delta,
// and the mandatory reason. All meter figures are exact decimal strings.
type CorrectionReportRow struct {
	ShiftID          uuid.UUID
	ShiftName        string
	PumpNumber       int
	NozzleNumber     int
	AttendantID      uuid.UUID
	AttendantName    string
	SubmittedReading string
	FinalReading     string
	DeltaLitres      string // final - submitted, computed in SQL numeric
	Status           string
	Reason           *string
	VerifiedBy       uuid.UUID
	VerifiedByName   string
	VerifiedAt       time.Time
}

// CorrectionReportRows returns the station's corrected/rejected closing-reading
// verifications for shifts opened in [from, to] (dates, inclusive), newest
// decision first (Mobile Attendant Phase 7 report feed).
func (r *Repo) CorrectionReportRows(ctx context.Context, tenantID, stationID uuid.UUID, from, to time.Time) ([]CorrectionReportRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT v.shift_id, s.name, p.number, n.number,
		       m.recorded_by, rec.full_name,
		       v.attendant_submitted_reading::text,
		       v.final_approved_reading::text,
		       (v.final_approved_reading - v.attendant_submitted_reading)::text,
		       v.status, v.reason,
		       v.verified_by, ver.full_name, v.verified_at
		FROM reading_verifications v
		JOIN meter_readings m ON m.tenant_id = v.tenant_id AND m.id = v.reading_id
		JOIN shifts s         ON s.tenant_id = v.tenant_id AND s.id = v.shift_id
		JOIN nozzles n        ON n.tenant_id = v.tenant_id AND n.id = v.nozzle_id
		JOIN pumps p          ON p.tenant_id = v.tenant_id AND p.id = n.pump_id
		JOIN users rec        ON rec.tenant_id = v.tenant_id AND rec.id = m.recorded_by
		JOIN users ver        ON ver.tenant_id = v.tenant_id AND ver.id = v.verified_by
		WHERE v.tenant_id = $1 AND v.station_id = $2
		  AND v.status <> 'approved'
		  AND s.opened_at::date BETWEEN $3::date AND $4::date
		ORDER BY v.verified_at DESC, v.id
	`, tenantID, stationID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CorrectionReportRow
	for rows.Next() {
		var c CorrectionReportRow
		if err := rows.Scan(&c.ShiftID, &c.ShiftName, &c.PumpNumber, &c.NozzleNumber,
			&c.AttendantID, &c.AttendantName,
			&c.SubmittedReading, &c.FinalReading, &c.DeltaLitres,
			&c.Status, &c.Reason,
			&c.VerifiedBy, &c.VerifiedByName, &c.VerifiedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ClosingVerificationGateCounts breaks the shift's ACTIVE closing readings into
// the buckets the approval gate cares about (PRD §7.8/§9.5 closeout): readings
// with no verification yet, readings held by a 'rejected' verdict (the
// attendant must re-capture), and readings held by a 'flagged' verdict (under
// investigation). A reading is approvable only when its ACTIVE row carries a
// terminal-good verdict {approved, corrected} — any of these three buckets
// being non-zero blocks approval. Runs through any Querier so the approval
// handler can re-check inside its FOR UPDATE tx.
//
// Holds are matched against the ACTIVE reading's own verification: a rejected
// reading that the attendant re-captured leaves the rejection on the now
// SUPERSEDED row and the new ACTIVE row unverified, so it lands in `unverified`
// (re-verification pending), never in `rejected`.
func (r *Repo) ClosingVerificationGateCounts(ctx context.Context, q database.Querier, tenantID, shiftID uuid.UUID) (unverified, rejected, flagged int, err error) {
	err = q.QueryRow(ctx, `
		SELECT
		    count(*) FILTER (WHERE v.id IS NULL),
		    count(*) FILTER (WHERE v.status = 'rejected'),
		    count(*) FILTER (WHERE v.status = 'flagged')
		FROM meter_readings m
		LEFT JOIN reading_verifications v
		    ON v.tenant_id = m.tenant_id AND v.reading_id = m.id
		WHERE m.tenant_id = $1 AND m.shift_id = $2
		  AND m.reading_type = 'closing' AND m.status = 'active'
	`, tenantID, shiftID).Scan(&unverified, &rejected, &flagged)
	return unverified, rejected, flagged, err
}

// ActiveClosingRejected reports whether the shift's ACTIVE closing reading for
// a nozzle carries a 'rejected' verification — i.e. the supervisor sent it back
// and the attendant is expected to re-capture (PRD §7.8). The capture/correct
// paths consult this to relax the Phase 3 closing-submission lock for exactly
// that nozzle: a rejection (and only a rejection) unlocks attendant resubmission.
func (r *Repo) ActiveClosingRejected(ctx context.Context, tenantID, shiftID, nozzleID uuid.UUID) (bool, error) {
	var rejected bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1
		    FROM meter_readings m
		    JOIN reading_verifications v
		        ON v.tenant_id = m.tenant_id AND v.reading_id = m.id
		    WHERE m.tenant_id = $1 AND m.shift_id = $2 AND m.nozzle_id = $3
		      AND m.reading_type = 'closing' AND m.status = 'active'
		      AND v.status = 'rejected'
		)
	`, tenantID, shiftID, nozzleID).Scan(&rejected)
	return rejected, err
}
