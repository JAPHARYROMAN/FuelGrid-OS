package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// handleExportAuditLogs streams the tenant's audit trail for a date range as
// CSV and — critically for compliance (AUDIT-EXPORT) — records the export
// itself as an audit_logs entry with action 'export.generated', so the act of
// exporting the audit trail is itself audited. Defaults to the last 30 days.
func (s *Server) handleExportAuditLogs(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx := r.Context()
	from := parseDateParam(r, "from", time.Now().AddDate(0, -1, 0))
	to := parseDateParam(r, "to", time.Now())
	toEnd := to.Add(24*time.Hour - time.Second) // inclusive end-of-day

	rows, qerr := s.deps.DB.Query(ctx, `
		SELECT occurred_at, actor_id, action, entity_type, entity_id,
		       COALESCE(reason, ''), COALESCE(host(ip), ''), COALESCE(request_id, '')
		FROM audit_logs
		WHERE tenant_id = $1 AND occurred_at >= $2 AND occurred_at <= $3
		ORDER BY occurred_at
	`, actor.TenantID, from, toEnd)
	if qerr != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()

	records := [][]string{{"occurred_at", "actor_id", "action", "entity_type", "entity_id", "reason", "ip", "request_id"}}
	for rows.Next() {
		var occurred time.Time
		var actorID uuid.UUID
		var action, etype, eid, reason, ip, reqID string
		if err := rows.Scan(&occurred, &actorID, &action, &etype, &eid, &reason, &ip, &reqID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		records = append(records, []string{occurred.Format(time.RFC3339), actorID.String(), action, etype, eid, reason, ip, reqID})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	if err := cw.WriteAll(records); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	sum := sha256.Sum256(buf.Bytes())
	checksum := hex.EncodeToString(sum[:])
	rowCount := len(records) - 1
	exportID := uuid.New()

	// The export is itself an audited event (export.generated).
	tx, terr := s.deps.DB.Begin(ctx)
	if terr != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "export.generated", EventType: "AuditExportGenerated",
		EntityType: "audit_export", EntityID: exportID.String(),
		NewValue:  map[string]any{"from": from.Format(dateLayout), "to": to.Format(dateLayout), "row_count": rowCount, "checksum": checksum},
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("audit export record", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"export_id": exportID, "export_type": "audit_logs", "format": "csv",
		"from": from.Format(dateLayout), "to": to.Format(dateLayout),
		"row_count": rowCount, "checksum": checksum, "csv": buf.String(),
	})
}

// exportTypeMap maps the URL slug to the stored export_type and back.
var exportTypeMap = map[string]string{
	"journal-entries": "journal_entries",
	"trial-balance":   "trial_balance",
	"ap-aging":        "ap_aging",
	"ar-aging":        "ar_aging",
}

// handleGenerateExport builds a CSV accounting export, records the run with a
// content checksum, and returns the CSV plus run metadata. Exports whose date
// range is fully locked are final (provisional=false) and reproducible — the
// same request yields the same checksum.
func (s *Server) handleGenerateExport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	slug := chi.URLParam(r, "type")
	exportType, ok := exportTypeMap[slug]
	if !ok {
		writeError(w, http.StatusBadRequest, "type must be journal-entries|trial-balance|ap-aging|ar-aging")
		return
	}
	ctx := r.Context()

	var records [][]string
	var from, to time.Time
	filters := map[string]any{}

	switch exportType {
	case "journal_entries":
		from = parseDateParam(r, "from", time.Now().AddDate(0, -1, 0))
		to = parseDateParam(r, "to", time.Now())
		filters["from"], filters["to"] = from.Format(dateLayout), to.Format(dateLayout)
		rows, qerr := s.accounting.ExportJournalLines(ctx, actor.TenantID, from, to)
		if qerr != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		records = append(records, []string{"entry_number", "entry_date", "account_code", "account_name", "debit", "credit", "source_type", "status", "memo"})
		for i := range rows {
			g := rows[i]
			memo := ""
			if g.Memo != nil {
				memo = *g.Memo
			}
			records = append(records, []string{
				strconv.FormatInt(g.EntryNumber, 10), g.EntryDate.Format(dateLayout), g.AccountCode, g.AccountName,
				g.Debit, g.Credit, g.SourceType, g.Status, memo,
			})
		}
	case "trial_balance":
		asOf := parseDateParam(r, "as_of", time.Now())
		from, to = time.Time{}, asOf
		filters["as_of"] = asOf.Format(dateLayout)
		rows, qerr := s.accounting.TrialBalance(ctx, actor.TenantID, asOf)
		if qerr != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		records = append(records, []string{"code", "name", "type", "normal_balance", "debit", "credit", "balance"})
		for i := range rows {
			t := rows[i]
			records = append(records, []string{t.Code, t.Name, t.Type, t.NormalBalance, t.Debit, t.Credit, t.Balance})
		}
	case "ap_aging":
		asOf := parseDateParam(r, "as_of", time.Now())
		from, to = time.Time{}, asOf
		filters["as_of"] = asOf.Format(dateLayout)
		rows, qerr := s.payables.Aging(ctx, actor.TenantID)
		if qerr != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		records = append(records, []string{"supplier_id", "outstanding", "open_count"})
		for i := range rows {
			records = append(records, []string{rows[i].SupplierID.String(), rows[i].Outstanding, strconv.Itoa(rows[i].OpenCount)})
		}
	case "ar_aging":
		asOf := parseDateParam(r, "as_of", time.Now())
		from, to = time.Time{}, asOf
		filters["as_of"] = asOf.Format(dateLayout)
		rows, qerr := s.receivables.InvoiceAging(ctx, actor.TenantID)
		if qerr != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		records = append(records, []string{"customer_id", "code", "name", "balance"})
		for i := range rows {
			records = append(records, []string{rows[i].CustomerID.String(), rows[i].Code, rows[i].Name, rows[i].Balance})
		}
	}

	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	if err := cw.WriteAll(records); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	sum := sha256.Sum256(buf.Bytes())
	checksum := hex.EncodeToString(sum[:])
	rowCount := len(records) - 1 // exclude header

	provisional, perr := s.accounting.RangeProvisional(ctx, actor.TenantID, from, to)
	if perr != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	exportID, rerr := s.accounting.RecordExport(ctx, actor.TenantID, exportType, "csv", filters, rowCount, checksum, provisional, actor.UserID)
	if rerr != nil {
		s.logger.Error("record export", "error", rerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"export_id": exportID, "export_type": exportType, "format": "csv",
		"row_count": rowCount, "checksum": checksum, "provisional": provisional, "csv": buf.String(),
	})
}

func (s *Server) handleListExports(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.accounting.ListExports(r.Context(), actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		e := rows[i]
		out = append(out, map[string]any{
			"id": e.ID, "export_type": e.ExportType, "format": e.Format, "row_count": e.RowCount,
			"checksum": e.Checksum, "provisional": e.Provisional, "generated_at": e.GeneratedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}
