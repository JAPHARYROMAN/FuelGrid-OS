package banking

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

type CashReconciliation struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	StationID      uuid.UUID
	OperatingDayID uuid.UUID
	ExpectedCash   string
	CountedCash    string
	Variance       string
	Status         string
	Notes          *string
	JournalEntryID *uuid.UUID
	ReviewedBy     *uuid.UUID
	CreatedBy      uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CashPosting carries the per-account amounts (as decimal strings, computed in
// SQL) needed to post an approved reconciliation, plus the business date that
// selects the accounting period.
type CashPosting struct {
	StationID    uuid.UUID
	BusinessDate time.Time
	Counted      string
	Expected     string
	ShortAmount  string // debit cash over/short when counted < expected
	OverAmount   string // credit cash over/short when counted > expected
	Status       string
}

const cashReconColumns = `
    id, tenant_id, station_id, operating_day_id, expected_cash::text, counted_cash::text,
    variance::text, status, notes, journal_entry_id, reviewed_by, created_by, created_at, updated_at
`

func scanCashRecon(row pgx.Row, c *CashReconciliation) error {
	return row.Scan(
		&c.ID, &c.TenantID, &c.StationID, &c.OperatingDayID, &c.ExpectedCash, &c.CountedCash,
		&c.Variance, &c.Status, &c.Notes, &c.JournalEntryID, &c.ReviewedBy, &c.CreatedBy,
		&c.CreatedAt, &c.UpdatedAt,
	)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// CreateCashReconciliation opens a draft reconciliation for an operating day,
// seeding expected cash from Phase-6 cash tenders across the day's shifts and a
// per-shift line breakdown. A second reconciliation for the same day is
// rejected with ErrDuplicate.
func (r *Repo) CreateCashReconciliation(ctx context.Context, tx pgx.Tx, tenantID, stationID, operatingDayID, createdBy uuid.UUID) (*CashReconciliation, error) {
	var c CashReconciliation
	err := scanCashRecon(tx.QueryRow(ctx, `
		INSERT INTO cash_reconciliations (tenant_id, station_id, operating_day_id, expected_cash, created_by)
		VALUES (
		    $1, $2, $3,
		    COALESCE((
		        SELECT SUM(p.amount) FROM payments p
		        JOIN shifts s ON s.id = p.shift_id AND s.tenant_id = p.tenant_id
		        WHERE p.tenant_id = $1 AND s.operating_day_id = $3
		          AND p.tender_type = 'cash' AND p.status = 'recorded'
		    ), 0),
		    $4
		)
		RETURNING `+cashReconColumns,
		tenantID, stationID, operatingDayID, createdBy,
	), &c)
	if isUniqueViolation(err) {
		return nil, ErrDuplicate
	}
	if err != nil {
		return nil, err
	}
	// Per-shift expected-cash lines for drill-through.
	if _, err := tx.Exec(ctx, `
		INSERT INTO cash_reconciliation_lines (tenant_id, cash_reconciliation_id, shift_id, expected_cash)
		SELECT $1, $2, s.id,
		    COALESCE(SUM(p.amount) FILTER (WHERE p.tender_type = 'cash' AND p.status = 'recorded'), 0)
		FROM shifts s
		LEFT JOIN payments p ON p.shift_id = s.id AND p.tenant_id = s.tenant_id
		WHERE s.tenant_id = $1 AND s.operating_day_id = $3
		GROUP BY s.id
	`, tenantID, c.ID, operatingDayID); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) GetCashReconciliation(ctx context.Context, tenantID, id uuid.UUID) (*CashReconciliation, error) {
	var c CashReconciliation
	err := scanCashRecon(r.pool.QueryRow(ctx,
		`SELECT `+cashReconColumns+` FROM cash_reconciliations WHERE tenant_id = $1 AND id = $2`, tenantID, id), &c)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) ListCashReconciliations(ctx context.Context, tenantID, stationID uuid.UUID) ([]CashReconciliation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+cashReconColumns+` FROM cash_reconciliations
		WHERE tenant_id = $1 AND ($2::uuid IS NULL OR station_id = $2)
		ORDER BY created_at DESC
	`, tenantID, nullUUID(stationID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CashReconciliation{}
	for rows.Next() {
		var c CashReconciliation
		if err := scanCashRecon(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SubmitCashReconciliation records counted cash and recomputes variance in SQL,
// moving draft/rejected -> submitted.
func (r *Repo) SubmitCashReconciliation(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, countedCash string, notes *string) (*CashReconciliation, error) {
	var c CashReconciliation
	err := scanCashRecon(tx.QueryRow(ctx, `
		UPDATE cash_reconciliations
		SET counted_cash = $3::numeric,
		    variance = ($3::numeric - expected_cash),
		    notes = COALESCE($4, notes),
		    status = 'submitted'
		WHERE tenant_id = $1 AND id = $2 AND status IN ('draft', 'rejected')
		RETURNING `+cashReconColumns,
		tenantID, id, countedCash, notes,
	), &c)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBadState
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// PostingFor returns the SQL-computed posting amounts and business date for a
// submitted reconciliation, used by the approve handler to post the journal.
func (r *Repo) PostingFor(ctx context.Context, q database.Querier, tenantID, id uuid.UUID) (*CashPosting, error) {
	var p CashPosting
	err := q.QueryRow(ctx, `
		SELECT c.station_id, od.business_date, c.counted_cash::text, c.expected_cash::text,
		       GREATEST(c.expected_cash - c.counted_cash, 0)::text,
		       GREATEST(c.counted_cash - c.expected_cash, 0)::text,
		       c.status
		FROM cash_reconciliations c
		JOIN operating_days od ON od.id = c.operating_day_id AND od.tenant_id = c.tenant_id
		WHERE c.tenant_id = $1 AND c.id = $2
	`, tenantID, id).Scan(&p.StationID, &p.BusinessDate, &p.Counted, &p.Expected, &p.ShortAmount, &p.OverAmount, &p.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// MarkCashReconciliationPosted finalizes an approved reconciliation, attaching
// the journal entry and reviewer, moving submitted -> posted.
func (r *Repo) MarkCashReconciliationPosted(ctx context.Context, tx pgx.Tx, tenantID, id, reviewedBy uuid.UUID, journalEntryID *uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE cash_reconciliations
		SET status = 'posted', reviewed_by = $3, journal_entry_id = $4
		WHERE tenant_id = $1 AND id = $2 AND status = 'submitted'
	`, tenantID, id, reviewedBy, journalEntryID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrBadState
	}
	return nil
}

func nullUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}
