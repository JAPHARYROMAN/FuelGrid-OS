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

// UnverifiedClosingCountForShift counts the shift's ACTIVE closing readings
// without a verification row — the shift-approval gate. It runs through any
// Querier so the approval handler can re-check inside the tx that holds the
// shift's FOR UPDATE lock (a verification cannot slip in between the check and
// the approval flip).
func (r *Repo) UnverifiedClosingCountForShift(ctx context.Context, q database.Querier, tenantID, shiftID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `
		SELECT count(*)
		FROM meter_readings m
		WHERE m.tenant_id = $1 AND m.shift_id = $2
		  AND m.reading_type = 'closing' AND m.status = 'active'
		  AND NOT EXISTS (
		      SELECT 1 FROM reading_verifications v
		      WHERE v.tenant_id = m.tenant_id AND v.reading_id = m.id
		  )
	`, tenantID, shiftID).Scan(&n)
	return n, err
}
