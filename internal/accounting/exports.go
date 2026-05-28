package accounting

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// JournalExportRow is a flattened posted journal line for the journal export.
type JournalExportRow struct {
	EntryNumber int64
	EntryDate   time.Time
	AccountCode string
	AccountName string
	Debit       string
	Credit      string
	SourceType  string
	Status      string
	Memo        *string
}

// ExportJournalLines returns every posted/reversed journal line in a date
// range, in entry order — the detail behind the journal export.
func (r *Repo) ExportJournalLines(ctx context.Context, tenantID uuid.UUID, from, to time.Time) ([]JournalExportRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT e.entry_number, e.entry_date, a.code, a.name, l.debit::text, l.credit::text,
		       e.source_type, e.status, e.memo
		FROM journal_lines l
		JOIN journal_entries e ON e.id = l.journal_entry_id AND e.tenant_id = l.tenant_id
		JOIN accounts a ON a.id = l.account_id AND a.tenant_id = l.tenant_id
		WHERE l.tenant_id = $1 AND e.status IN ('posted', 'reversed') AND e.entry_date BETWEEN $2 AND $3
		ORDER BY e.entry_number, a.code
	`, tenantID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JournalExportRow{}
	for rows.Next() {
		var g JournalExportRow
		if err := rows.Scan(&g.EntryNumber, &g.EntryDate, &g.AccountCode, &g.AccountName, &g.Debit, &g.Credit, &g.SourceType, &g.Status, &g.Memo); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// RangeProvisional reports whether any accounting period overlapping [from, to]
// is not yet locked — i.e. the export covers data that could still change. An
// export over only locked periods is final and reproducible.
func (r *Repo) RangeProvisional(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (bool, error) {
	var provisional bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM accounting_periods
		    WHERE tenant_id = $1 AND status <> 'locked'
		      AND start_date <= $3 AND end_date >= $2
		)
	`, tenantID, from, to).Scan(&provisional)
	return provisional, err
}

// ExportRun is a recorded export generation.
type ExportRun struct {
	ID          uuid.UUID
	ExportType  string
	Format      string
	RowCount    int
	Checksum    string
	Provisional bool
	GeneratedAt time.Time
}

// RecordExport persists an export run (its filters, checksum, and provisional
// flag) and returns its id.
func (r *Repo) RecordExport(ctx context.Context, tenantID uuid.UUID, exportType, format string, filters map[string]any, rowCount int, checksum string, provisional bool, generatedBy uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		INSERT INTO accounting_exports (tenant_id, export_type, format, filters, row_count, checksum, provisional, generated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, tenantID, exportType, format, filters, rowCount, checksum, provisional, generatedBy).Scan(&id)
	return id, err
}

func (r *Repo) ListExports(ctx context.Context, tenantID uuid.UUID) ([]ExportRun, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, export_type, format, row_count, checksum, provisional, generated_at
		FROM accounting_exports WHERE tenant_id = $1 ORDER BY generated_at DESC LIMIT 100
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportRun{}
	for rows.Next() {
		var e ExportRun
		if err := rows.Scan(&e.ID, &e.ExportType, &e.Format, &e.RowCount, &e.Checksum, &e.Provisional, &e.GeneratedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
