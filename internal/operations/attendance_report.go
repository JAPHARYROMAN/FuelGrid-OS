package operations

// Report feeds for the Mobile Attendant App Phase 7 datasets: attendance
// (roster vs check-in, with late/no-show derivation) and collection variances
// (expected vs received with difference + reason). Both are station + date
// range reads; every money figure is the exact SQL-numeric decimal string.

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// LateCheckInGrace is the deterministic late threshold: an attendant who
// checks in more than this long after the shift opened is derived "late".
// A fixed rule (not a tenant knob) so the report is reproducible.
const LateCheckInGrace = 15 * time.Minute

// AttendanceReportRow is one rostered attendant on one shift in the window,
// LEFT-joined to their attendance record. DerivedStatus is computed in SQL:
//
//	present         — checked in within LateCheckInGrace of the shift opening
//	late            — checked in after the grace window
//	not_checked_in  — no record yet, but the shift is still open
//	no_show         — no record and the shift has moved past open
type AttendanceReportRow struct {
	ShiftID        uuid.UUID
	ShiftName      string
	Slot           *string
	ShiftStatus    string
	OpenedAt       time.Time
	AttendantID    uuid.UUID
	AttendantName  string
	AttendantEmail string
	CheckInAt      *time.Time
	CheckOutAt     *time.Time
	DerivedStatus  string
}

// AttendanceReportRows returns the station's roster-vs-attendance rows for
// shifts opened in [from, to] (dates, inclusive), newest shift first.
func (r *Repo) AttendanceReportRows(ctx context.Context, tenantID, stationID uuid.UUID, from, to time.Time) ([]AttendanceReportRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.name, s.slot, s.status, s.opened_at,
		       u.id, u.full_name, u.email,
		       att.check_in_at, att.check_out_at,
		       CASE
		           WHEN att.id IS NULL AND s.status = 'open' THEN 'not_checked_in'
		           WHEN att.id IS NULL THEN 'no_show'
		           WHEN att.check_in_at > s.opened_at + make_interval(mins => $5) THEN 'late'
		           ELSE 'present'
		       END
		FROM shifts s
		JOIN shift_attendants sa ON sa.tenant_id = s.tenant_id AND sa.shift_id = s.id
		JOIN users u             ON u.tenant_id  = s.tenant_id AND u.id = sa.user_id
		LEFT JOIN shift_attendance att
		       ON att.tenant_id = s.tenant_id AND att.shift_id = s.id AND att.attendant_id = sa.user_id
		WHERE s.tenant_id = $1 AND s.station_id = $2
		  AND s.opened_at::date BETWEEN $3::date AND $4::date
		ORDER BY s.opened_at DESC, s.id, u.full_name
	`, tenantID, stationID, from, to, int(LateCheckInGrace.Minutes()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AttendanceReportRow
	for rows.Next() {
		var a AttendanceReportRow
		if err := rows.Scan(&a.ShiftID, &a.ShiftName, &a.Slot, &a.ShiftStatus, &a.OpenedAt,
			&a.AttendantID, &a.AttendantName, &a.AttendantEmail,
			&a.CheckInAt, &a.CheckOutAt, &a.DerivedStatus); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CollectionVarianceRow is one collection receipt in the window with its cash
// submission's submitter: expected vs submitted vs received, the SQL-computed
// difference, and the reason when the supervisor recorded one. All money
// figures are exact decimal strings.
type CollectionVarianceRow struct {
	ShiftID         uuid.UUID
	ShiftName       string
	SubmittedBy     uuid.UUID
	SubmittedByName string
	ExpectedAmount  string
	SubmittedTotal  string
	ReceivedTotal   string
	Difference      string
	Status          string
	Reason          *string
	ReceivedAt      time.Time
}

// CollectionVarianceTotals are the window's shortage/excess rollups, summed in
// SQL numeric (never Go float) and returned as exact decimal strings.
type CollectionVarianceTotals struct {
	ReceiptCount  int
	ShortageTotal string // sum of |difference| where difference < 0
	ExcessTotal   string // sum of difference where difference > 0
}

// CollectionVarianceReport returns the station's collection receipts for
// shifts opened in [from, to] (dates, inclusive), newest first, plus the
// SQL-numeric shortage/excess totals.
func (r *Repo) CollectionVarianceReport(ctx context.Context, tenantID, stationID uuid.UUID, from, to time.Time) ([]CollectionVarianceRow, CollectionVarianceTotals, error) {
	var totals CollectionVarianceTotals
	rows, err := r.pool.Query(ctx, `
		SELECT cr.shift_id, s.name, cs.submitted_by, u.full_name,
		       cr.expected_amount::text, cr.attendant_submitted_total::text,
		       cr.supervisor_received_total::text, cr.difference::text,
		       cr.status, cr.reason, cr.received_at
		FROM collection_receipts cr
		JOIN cash_submissions cs ON cs.tenant_id = cr.tenant_id AND cs.id = cr.cash_submission_id
		JOIN shifts s            ON s.tenant_id  = cr.tenant_id AND s.id = cr.shift_id
		JOIN users u             ON u.tenant_id  = cr.tenant_id AND u.id = cs.submitted_by
		WHERE cr.tenant_id = $1 AND cr.station_id = $2
		  AND s.opened_at::date BETWEEN $3::date AND $4::date
		ORDER BY cr.received_at DESC, cr.id
	`, tenantID, stationID, from, to)
	if err != nil {
		return nil, totals, err
	}
	defer rows.Close()
	var out []CollectionVarianceRow
	for rows.Next() {
		var c CollectionVarianceRow
		if err := rows.Scan(&c.ShiftID, &c.ShiftName, &c.SubmittedBy, &c.SubmittedByName,
			&c.ExpectedAmount, &c.SubmittedTotal, &c.ReceivedTotal, &c.Difference,
			&c.Status, &c.Reason, &c.ReceivedAt); err != nil {
			return nil, totals, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, totals, err
	}

	// Shortage/excess rollups in SQL numeric on the same window.
	if err := r.pool.QueryRow(ctx, `
		SELECT count(*),
		       COALESCE(SUM(CASE WHEN cr.difference < 0 THEN -cr.difference ELSE 0 END), 0)::text,
		       COALESCE(SUM(CASE WHEN cr.difference > 0 THEN  cr.difference ELSE 0 END), 0)::text
		FROM collection_receipts cr
		JOIN shifts s ON s.tenant_id = cr.tenant_id AND s.id = cr.shift_id
		WHERE cr.tenant_id = $1 AND cr.station_id = $2
		  AND s.opened_at::date BETWEEN $3::date AND $4::date
	`, tenantID, stationID, from, to).Scan(&totals.ReceiptCount, &totals.ShortageTotal, &totals.ExcessTotal); err != nil {
		return nil, totals, err
	}
	return out, totals, nil
}
