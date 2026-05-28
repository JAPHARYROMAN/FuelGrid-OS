package accounting

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

var (
	ErrUnbalanced      = errors.New("accounting: entry debits do not equal credits")
	ErrEntryNotFound   = errors.New("accounting: journal entry not found")
	ErrAlreadyReversed = errors.New("accounting: journal entry already reversed")
	ErrNoLines         = errors.New("accounting: entry has no lines")
)

type JournalEntry struct {
	ID                uuid.UUID
	EntryNumber       int64
	TenantID          uuid.UUID
	PeriodID          uuid.UUID
	EntryDate         time.Time
	SourceType        string
	SourceID          *uuid.UUID
	StationID         *uuid.UUID
	Status            string
	Memo              *string
	ReversesEntryID   *uuid.UUID
	ReversedByEntryID *uuid.UUID
	PostedBy          uuid.UUID
	PostedAt          time.Time
	Total             string // total debits, populated on list
	Lines             []JournalLine
}

type JournalLine struct {
	ID             uuid.UUID
	JournalEntryID uuid.UUID
	AccountID      uuid.UUID
	Debit          string
	Credit         string
	StationID      *uuid.UUID
	Memo           *string
}

// PostLine is a line to post. Provide either AccountID or SystemKey.
type PostLine struct {
	AccountID uuid.UUID
	SystemKey string
	Debit     string
	Credit    string
	StationID *uuid.UUID
	Memo      *string
}

type PostEntryInput struct {
	EntryDate   time.Time
	SourceType  string
	SourceID    *uuid.UUID
	StationID   *uuid.UUID
	Memo        *string
	PostedBy    uuid.UUID
	AllowClosed bool // adjustments may post into a closed (not locked) period
	Lines       []PostLine
}

const entryColumns = `
    id, entry_number, tenant_id, period_id, entry_date, source_type, source_id, station_id,
    status, memo, reverses_entry_id, reversed_by_entry_id, posted_by, posted_at
`

func scanEntry(row pgx.Row, e *JournalEntry) error {
	return row.Scan(
		&e.ID, &e.EntryNumber, &e.TenantID, &e.PeriodID, &e.EntryDate, &e.SourceType, &e.SourceID, &e.StationID,
		&e.Status, &e.Memo, &e.ReversesEntryID, &e.ReversedByEntryID, &e.PostedBy, &e.PostedAt,
	)
}

const lineColumns = `id, journal_entry_id, account_id, debit::text, credit::text, station_id, memo`

func scanLine(row pgx.Row, l *JournalLine) error {
	return row.Scan(&l.ID, &l.JournalEntryID, &l.AccountID, &l.Debit, &l.Credit, &l.StationID, &l.Memo)
}

func money0(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

// PostEntry posts a balanced journal entry inside the caller's tx. It resolves
// the period covering EntryDate (rejecting locked, and closed unless
// AllowClosed), resolves each line's account (by AccountID or SystemKey),
// inserts the entry and lines, and verifies debits == credits in SQL.
func (r *Repo) PostEntry(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in PostEntryInput) (*JournalEntry, error) {
	if len(in.Lines) == 0 {
		return nil, ErrNoLines
	}
	periodID, err := r.resolvePostingPeriod(ctx, tx, tenantID, in.EntryDate, in.AllowClosed)
	if err != nil {
		return nil, err
	}

	var e JournalEntry
	if err := scanEntry(tx.QueryRow(ctx, `
		INSERT INTO journal_entries
		    (tenant_id, period_id, entry_date, source_type, source_id, station_id, memo, posted_by, reverses_entry_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL)
		RETURNING `+entryColumns,
		tenantID, periodID, in.EntryDate, in.SourceType, in.SourceID, in.StationID, in.Memo, in.PostedBy,
	), &e); err != nil {
		return nil, err
	}

	for _, ln := range in.Lines {
		accID := ln.AccountID
		if accID == uuid.Nil {
			accID, err = r.resolveSystemAccount(ctx, tx, tenantID, ln.SystemKey)
			if err != nil {
				return nil, err
			}
		}
		var l JournalLine
		if err := scanLine(tx.QueryRow(ctx, `
			INSERT INTO journal_lines (tenant_id, journal_entry_id, account_id, debit, credit, station_id, memo)
			VALUES ($1, $2, $3, $4::numeric, $5::numeric, $6, $7)
			RETURNING `+lineColumns,
			tenantID, e.ID, accID, money0(ln.Debit), money0(ln.Credit), ln.StationID, ln.Memo,
		), &l); err != nil {
			return nil, err
		}
		e.Lines = append(e.Lines, l)
	}

	var balanced bool
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(debit), 0) = COALESCE(SUM(credit), 0) AND COALESCE(SUM(debit), 0) > 0,
		       COALESCE(SUM(debit), 0)::text
		FROM journal_lines WHERE tenant_id = $1 AND journal_entry_id = $2
	`, tenantID, e.ID).Scan(&balanced, &e.Total); err != nil {
		return nil, err
	}
	if !balanced {
		return nil, ErrUnbalanced
	}
	return &e, nil
}

func (r *Repo) GetEntry(ctx context.Context, tenantID, id uuid.UUID) (*JournalEntry, error) {
	return r.getEntry(ctx, r.pool, tenantID, id)
}

// getEntry loads an entry + lines through any querier, so callers inside a tx
// can read an entry they posted in the same uncommitted transaction.
func (r *Repo) getEntry(ctx context.Context, q database.Querier, tenantID, id uuid.UUID) (*JournalEntry, error) {
	var e JournalEntry
	err := scanEntry(q.QueryRow(ctx, `SELECT `+entryColumns+` FROM journal_entries WHERE tenant_id = $1 AND id = $2`, tenantID, id), &e)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEntryNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT `+lineColumns+` FROM journal_lines WHERE tenant_id = $1 AND journal_entry_id = $2 ORDER BY id`, tenantID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l JournalLine
		if err := scanLine(rows, &l); err != nil {
			return nil, err
		}
		e.Lines = append(e.Lines, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `
		SELECT COALESCE(SUM(debit), 0)::text FROM journal_lines WHERE tenant_id = $1 AND journal_entry_id = $2
	`, tenantID, id).Scan(&e.Total); err != nil {
		return nil, err
	}
	return &e, nil
}

// ListEntries returns recent journal entries (headers + total debit) for the
// tenant, newest first.
func (r *Repo) ListEntries(ctx context.Context, tenantID uuid.UUID, limit int) ([]JournalEntry, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+prefixedEntryColumns+`,
		    COALESCE((SELECT SUM(debit) FROM journal_lines l WHERE l.journal_entry_id = e.id), 0)::text
		FROM journal_entries e
		WHERE e.tenant_id = $1
		ORDER BY e.entry_number DESC
		LIMIT $2
	`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JournalEntry{}
	for rows.Next() {
		var e JournalEntry
		if err := rows.Scan(
			&e.ID, &e.EntryNumber, &e.TenantID, &e.PeriodID, &e.EntryDate, &e.SourceType, &e.SourceID, &e.StationID,
			&e.Status, &e.Memo, &e.ReversesEntryID, &e.ReversedByEntryID, &e.PostedBy, &e.PostedAt, &e.Total,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

const prefixedEntryColumns = `
    e.id, e.entry_number, e.tenant_id, e.period_id, e.entry_date, e.source_type, e.source_id, e.station_id,
    e.status, e.memo, e.reverses_entry_id, e.reversed_by_entry_id, e.posted_by, e.posted_at
`

// ReverseEntry posts a balanced reversal of a posted entry (lines swapped) and
// marks the original reversed, inside the caller's tx.
func (r *Repo) ReverseEntry(ctx context.Context, tx pgx.Tx, tenantID, entryID, postedBy uuid.UUID, memo *string) (*JournalEntry, error) {
	orig, err := r.getEntry(ctx, tx, tenantID, entryID)
	if err != nil {
		return nil, err
	}
	if orig.Status != "posted" {
		return nil, ErrAlreadyReversed
	}
	periodID, err := r.resolvePostingPeriod(ctx, tx, tenantID, orig.EntryDate, true)
	if err != nil {
		return nil, err
	}

	var rev JournalEntry
	if err := scanEntry(tx.QueryRow(ctx, `
		INSERT INTO journal_entries
		    (tenant_id, period_id, entry_date, source_type, source_id, station_id, memo, posted_by, reverses_entry_id)
		VALUES ($1, $2, $3, 'reversal', $4, $5, $6, $7, $8)
		RETURNING `+entryColumns,
		tenantID, periodID, orig.EntryDate, orig.ID, orig.StationID, memo, postedBy, orig.ID,
	), &rev); err != nil {
		return nil, err
	}

	// Swap debit/credit from the original lines.
	if _, err := tx.Exec(ctx, `
		INSERT INTO journal_lines (tenant_id, journal_entry_id, account_id, debit, credit, station_id, memo)
		SELECT tenant_id, $3, account_id, credit, debit, station_id, memo
		FROM journal_lines WHERE tenant_id = $1 AND journal_entry_id = $2
	`, tenantID, orig.ID, rev.ID); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE journal_entries SET status = 'reversed', reversed_by_entry_id = $3
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, orig.ID, rev.ID); err != nil {
		return nil, err
	}

	return r.getEntry(ctx, tx, tenantID, rev.ID)
}
