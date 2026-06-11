package operations

// Collection receipts — supervisor confirmation of a shift's cash submission
// (Mobile Attendant App, Phase 0). A receipt snapshots the expected amount and
// the attendant's submitted total, records what the supervisor actually
// received, and computes the difference (received − expected) in SQL numeric.
// One receipt per cash submission; shift approval is gated on a non-rejected
// receipt existing.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// CollectionReceipt is one supervisor confirmation of a cash handover. Every
// money field is an exact decimal STRING (numeric(14,2) read ::text).
type CollectionReceipt struct {
	ID                      uuid.UUID
	TenantID                uuid.UUID
	StationID               uuid.UUID
	ShiftID                 uuid.UUID
	CashSubmissionID        uuid.UUID
	ExpectedAmount          string
	AttendantSubmittedTotal string
	SupervisorReceivedTotal string
	Difference              string
	Status                  string
	Reason                  *string
	SupervisorComment       *string
	ReceivedBy              uuid.UUID
	ReceivedAt              time.Time
}

// CollectionReceiptInput carries one receipt insert. The three money figures
// are exact decimal strings bound $N::numeric; difference is computed in SQL.
type CollectionReceiptInput struct {
	StationID               uuid.UUID
	ShiftID                 uuid.UUID
	CashSubmissionID        uuid.UUID
	ExpectedAmount          string
	AttendantSubmittedTotal string
	SupervisorReceivedTotal string
	Status                  string
	Reason                  *string
	SupervisorComment       *string
	ReceivedBy              uuid.UUID
}

const receiptColumns = `
    id, tenant_id, station_id, shift_id, cash_submission_id,
    expected_amount::text, attendant_submitted_total::text,
    supervisor_received_total::text, difference::text,
    status, reason, supervisor_comment, received_by, received_at
`

func scanReceipt(row pgx.Row, c *CollectionReceipt) error {
	return row.Scan(
		&c.ID, &c.TenantID, &c.StationID, &c.ShiftID, &c.CashSubmissionID,
		&c.ExpectedAmount, &c.AttendantSubmittedTotal,
		&c.SupervisorReceivedTotal, &c.Difference,
		&c.Status, &c.Reason, &c.SupervisorComment, &c.ReceivedBy, &c.ReceivedAt,
	)
}

// InsertCollectionReceipt writes one receipt inside the caller's tx, computing
// difference (received − expected) in SQL numeric from the bound decimal
// strings. A second receipt for the same cash submission trips
// uq_collection_receipts_submission, which the handler maps to 409.
func (r *Repo) InsertCollectionReceipt(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CollectionReceiptInput) (*CollectionReceipt, error) {
	var c CollectionReceipt
	if err := scanReceipt(tx.QueryRow(ctx, `
		INSERT INTO collection_receipts
		    (tenant_id, station_id, shift_id, cash_submission_id,
		     expected_amount, attendant_submitted_total, supervisor_received_total,
		     difference, status, reason, supervisor_comment, received_by)
		VALUES ($1, $2, $3, $4, $5::numeric, $6::numeric, $7::numeric,
		        ($7::numeric - $5::numeric), $8, $9, $10, $11)
		RETURNING `+receiptColumns,
		tenantID, in.StationID, in.ShiftID, in.CashSubmissionID,
		in.ExpectedAmount, in.AttendantSubmittedTotal, in.SupervisorReceivedTotal,
		in.Status, in.Reason, in.SupervisorComment, in.ReceivedBy,
	), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// GetCollectionReceiptForShift returns the shift's receipt, or pgx.ErrNoRows.
func (r *Repo) GetCollectionReceiptForShift(ctx context.Context, tenantID, shiftID uuid.UUID) (*CollectionReceipt, error) {
	var c CollectionReceipt
	if err := scanReceipt(r.pool.QueryRow(ctx, `
		SELECT `+receiptColumns+`
		FROM collection_receipts WHERE tenant_id = $1 AND shift_id = $2
	`, tenantID, shiftID), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// CashSubmissionAwaitingReceipt reports whether the shift has a cash
// submission with no non-rejected collection receipt — the shift-approval
// gate. It runs through any Querier so the approval handler can re-check
// inside the tx that holds the shift's FOR UPDATE lock.
func (r *Repo) CashSubmissionAwaitingReceipt(ctx context.Context, q database.Querier, tenantID, shiftID uuid.UUID) (bool, error) {
	var awaiting bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM cash_submissions cs
			WHERE cs.tenant_id = $1 AND cs.shift_id = $2
			  AND NOT EXISTS (
			      SELECT 1 FROM collection_receipts cr
			      WHERE cr.tenant_id = cs.tenant_id
			        AND cr.cash_submission_id = cs.id
			        AND cr.status <> 'rejected'
			  )
		)
	`, tenantID, shiftID).Scan(&awaiting)
	return awaiting, err
}

// DecimalDifference returns a − b computed in SQL numeric as an exact
// numeric(14,2) decimal string, plus whether it is zero — used to derive a
// receipt's status/reason requirement before insert without Go float math.
func (r *Repo) DecimalDifference(ctx context.Context, a, b string) (diff string, zero bool, err error) {
	err = r.pool.QueryRow(ctx, `
		SELECT ($1::numeric - $2::numeric)::numeric(14,2)::text,
		       ($1::numeric - $2::numeric) = 0
	`, a, b).Scan(&diff, &zero)
	return diff, zero, err
}
