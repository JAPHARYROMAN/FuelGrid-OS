package banking

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type StatementLine struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	ImportID       uuid.UUID
	BankAccountID  uuid.UUID
	TxnDate        time.Time
	ValueDate      *time.Time
	Amount         string
	Reference      *string
	Description    *string
	Status         string
	MatchedDocType *string
	MatchedDocID   *uuid.UUID
	JournalEntryID *uuid.UUID
	CreatedAt      time.Time
}

type StatementLineInput struct {
	TxnDate     time.Time
	ValueDate   *time.Time
	Amount      string
	Reference   *string
	Description *string
}

const statementLineColumns = `
    id, tenant_id, import_id, bank_account_id, txn_date, value_date, amount::text, reference,
    description, status, matched_doc_type, matched_doc_id, journal_entry_id, created_at
`

func scanStatementLine(row pgx.Row, l *StatementLine) error {
	return row.Scan(
		&l.ID, &l.TenantID, &l.ImportID, &l.BankAccountID, &l.TxnDate, &l.ValueDate, &l.Amount,
		&l.Reference, &l.Description, &l.Status, &l.MatchedDocType, &l.MatchedDocID, &l.JournalEntryID, &l.CreatedAt,
	)
}

// ImportStatement ingests a batch of statement lines under a content hash. A
// re-import of the same (account, hash) is rejected with ErrDuplicate so
// replays cannot duplicate lines.
func (r *Repo) ImportStatement(ctx context.Context, tx pgx.Tx, tenantID, bankAccountID uuid.UUID, start, end *time.Time, hash string, importedBy uuid.UUID, lines []StatementLineInput) (uuid.UUID, int, error) {
	var importID uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO bank_statement_imports (tenant_id, bank_account_id, statement_start, statement_end, import_hash, line_count, imported_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, tenantID, bankAccountID, start, end, hash, len(lines), importedBy).Scan(&importID)
	if isUniqueViolation(err) {
		return uuid.Nil, 0, ErrDuplicate
	}
	if err != nil {
		return uuid.Nil, 0, err
	}
	for _, ln := range lines {
		if _, err := tx.Exec(ctx, `
			INSERT INTO bank_statement_lines
			    (tenant_id, import_id, bank_account_id, txn_date, value_date, amount, reference, description)
			VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8)
		`, tenantID, importID, bankAccountID, ln.TxnDate, ln.ValueDate, ln.Amount, ln.Reference, ln.Description); err != nil {
			return uuid.Nil, 0, err
		}
	}
	return importID, len(lines), nil
}

func (r *Repo) ListStatementLines(ctx context.Context, tenantID, bankAccountID uuid.UUID, status string) ([]StatementLine, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+statementLineColumns+` FROM bank_statement_lines
		WHERE tenant_id = $1
		  AND ($2::uuid IS NULL OR bank_account_id = $2)
		  AND ($3 = '' OR status = $3)
		ORDER BY txn_date DESC, created_at DESC
	`, tenantID, nullUUID(bankAccountID), status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StatementLine{}
	for rows.Next() {
		var l StatementLine
		if err := scanStatementLine(rows, &l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListStatementLinesPage returns a page of bank statement lines for the tenant
// (optionally filtered by account and status), newest first by txn_date (with
// id as a tiebreaker for stable paging), applying the supplied limit and offset.
func (r *Repo) ListStatementLinesPage(ctx context.Context, tenantID, bankAccountID uuid.UUID, status string, limit, offset int) ([]StatementLine, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+statementLineColumns+` FROM bank_statement_lines
		WHERE tenant_id = $1
		  AND ($2::uuid IS NULL OR bank_account_id = $2)
		  AND ($3 = '' OR status = $3)
		ORDER BY txn_date DESC, created_at DESC, id
		LIMIT $4 OFFSET $5
	`, tenantID, nullUUID(bankAccountID), status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StatementLine{}
	for rows.Next() {
		var l StatementLine
		if err := scanStatementLine(rows, &l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *Repo) GetStatementLine(ctx context.Context, tenantID, id uuid.UUID) (*StatementLine, error) {
	var l StatementLine
	err := scanStatementLine(r.pool.QueryRow(ctx, `SELECT `+statementLineColumns+` FROM bank_statement_lines WHERE tenant_id = $1 AND id = $2`, tenantID, id), &l)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// MatchLine links an unmatched line to a settlement document.
func (r *Repo) MatchLine(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, docType string, docID uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE bank_statement_lines
		SET status = 'matched', matched_doc_type = $3, matched_doc_id = $4
		WHERE tenant_id = $1 AND id = $2 AND status IN ('unmatched', 'unknown')
	`, tenantID, id, docType, docID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrBadState
	}
	return nil
}

// UnmatchLine returns a matched/flagged line to unmatched and clears its links.
func (r *Repo) UnmatchLine(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE bank_statement_lines
		SET status = 'unmatched', matched_doc_type = NULL, matched_doc_id = NULL, journal_entry_id = NULL
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkLine flags a line as a bank fee (with its posted journal entry) or as
// unknown for follow-up.
func (r *Repo) MarkLine(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, status string, journalEntryID *uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE bank_statement_lines
		SET status = $3, journal_entry_id = $4
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id, status, journalEntryID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
