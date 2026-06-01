package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/accounting"
	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// General-ledger export (GL-EXPORT).
//
// A single endpoint streams the posted general ledger (journal entries + lines)
// for a reporting period in standard, accountant-importable formats:
//
//   - csv  — a generic, spreadsheet-ready ledger (one row per line)
//   - iif  — Intuit Interchange Format, importable into desktop QuickBooks
//   - xero — a Xero "Manual Journals" compatible CSV
//
// All three are built from the same posted/reversed journal lines the JSON
// general-ledger and journal-export endpoints use, so every figure drills back
// to a journal entry. Money stays an exact decimal string end-to-end — the
// values from the database are written verbatim, never parsed into a float.
//
// Like the other report exports (reports_handlers.go), the endpoint is
// permission-gated (finance.read), streams the file as a Content-Disposition
// attachment so the BFF can hand it straight to a browser download, and records
// the export as an audited event (action 'report.exported') with a content
// checksum so the act of exporting is provably logged.

// registerAccountingExportRoutes mounts the GL export endpoint inside the
// admin-console group (requireAuth + rateLimitPerTenant), gated by finance.read
// like the other tenant-wide financial report exports.
func (s *Server) registerAccountingExportRoutes(r chi.Router) {
	r.With(s.requirePermissionHeld("finance.read")).
		Get("/accounting/gl-export.csv", s.handleExportGeneralLedger)
}

// glExportFormats is the set of accepted ?format values.
var glExportFormats = map[string]bool{"csv": true, "iif": true, "xero": true}

// handleExportGeneralLedger streams the period's general ledger in the
// requested format (?format=csv|iif|xero, default csv) over the window selected
// by ?period (this-month|last-month|ytd|last-30, default this-month). Gated by
// finance.read via the route and audited as a 'report.exported' event.
func (s *Server) handleExportGeneralLedger(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx := r.Context()

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}
	if !glExportFormats[format] {
		writeError(w, http.StatusBadRequest, "format must be csv|iif|xero")
		return
	}

	period := r.URL.Query().Get("period")
	from, to, label := resolveReportPeriod(period, time.Now())

	lines, err := s.accounting.ExportJournalLines(ctx, actor.TenantID, from, to)
	if err != nil {
		s.logger.Error("gl export: journal lines", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var body []byte
	switch format {
	case "iif":
		body = buildGLIIF(lines)
	case "xero":
		body, err = buildGLXeroCSV(lines)
	default:
		body, err = buildGLGenericCSV(lines)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	contentType, ext := "text/csv; charset=utf-8", "csv"
	if format == "iif" {
		contentType, ext = "application/octet-stream", "iif"
	}
	filename := fmt.Sprintf("general-ledger-%s-%s.%s", label, format, ext)

	s.writeExportFile(w, r, actor, "general_ledger", format, filename, contentType, body, map[string]any{
		"period": label, "from": from.Format(dateLayout), "to": to.Format(dateLayout),
		"format": format, "line_count": len(lines),
	})
}

// writeExportFile records the export in the audit log (action 'report.exported')
// with a content checksum, then — on success — streams the bytes as a
// downloadable attachment. Mirrors writeReportCSV but serialises arbitrary
// pre-built bytes (the IIF/Xero/CSV bodies) rather than CSV records, and carries
// the format on both the audit entry and response headers. Any failure writes a
// JSON error and returns; on success it writes the body. Always the final step.
func (s *Server) writeExportFile(
	w http.ResponseWriter, r *http.Request, actor identity.Actor,
	reportType, format, filename, contentType string, body []byte, meta map[string]any,
) {
	sum := sha256.Sum256(body)
	checksum := hex.EncodeToString(sum[:])
	exportID := uuid.New()

	newValue := map[string]any{"report_type": reportType, "format": format, "checksum": checksum}
	for k, v := range meta {
		newValue[k] = v
	}

	ctx := r.Context()
	tx, terr := s.deps.DB.Begin(ctx)
	if terr != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "report.exported", EventType: "ReportExported",
		EntityType: "report_export", EntityID: exportID.String(),
		NewValue:  newValue,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("gl export audit", "error", err, "report_type", reportType)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("X-Export-Id", exportID.String())
	w.Header().Set("X-Export-Checksum", checksum)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// glMemo flattens the optional entry memo to a plain string.
func glMemo(g accounting.JournalExportRow) string {
	if g.Memo != nil {
		return *g.Memo
	}
	return ""
}

// buildGLGenericCSV builds the generic, spreadsheet-ready ledger: one row per
// posted journal line, debit/credit as exact decimal strings.
func buildGLGenericCSV(lines []accounting.JournalExportRow) ([]byte, error) {
	records := make([][]string, 0, 1+len(lines))
	records = append(records, []string{
		"entry_number", "entry_date", "account_code", "account_name",
		"debit", "credit", "source_type", "status", "memo",
	})
	for i := range lines {
		g := lines[i]
		records = append(records, []string{
			strconv.FormatInt(g.EntryNumber, 10), g.EntryDate.Format(dateLayout),
			g.AccountCode, g.AccountName, g.Debit, g.Credit, g.SourceType, g.Status, glMemo(g),
		})
	}
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	if err := cw.WriteAll(records); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// buildGLXeroCSV builds a Xero "Manual Journals" compatible CSV. Xero imports a
// manual journal as a flat set of lines keyed by a Narration + Date that group
// lines into one journal; each line carries an AccountCode and exactly one of a
// Debit or Credit amount (decimal strings). We group by entry_number via the
// *Narration column, repeating the date per line as Xero expects.
func buildGLXeroCSV(lines []accounting.JournalExportRow) ([]byte, error) {
	records := make([][]string, 0, 1+len(lines))
	records = append(records, []string{
		"*Narration", "*Date", "*AccountCode", "Description", "Debit", "Credit",
	})
	for i := range lines {
		g := lines[i]
		narration := fmt.Sprintf("Journal %d (%s)", g.EntryNumber, g.SourceType)
		records = append(records, []string{
			narration, g.EntryDate.Format("02/01/2006"), g.AccountCode, glMemo(g),
			glAmount(g.Debit), glAmount(g.Credit),
		})
	}
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	if err := cw.WriteAll(records); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// glAmount blanks a zero amount so a Xero line carries a value in only the
// debit OR credit column, as Xero requires for manual-journal imports.
func glAmount(v string) string {
	switch v {
	case "", "0", "0.00":
		return ""
	}
	return v
}

// buildGLIIF builds an Intuit Interchange Format (IIF) file importable into
// desktop QuickBooks. IIF is a tab-delimited format: header definition rows
// (!TRNS / !SPL / !ENDTRNS) followed, per journal entry, by one TRNS line, one
// SPL line per remaining line, and an ENDTRNS terminator. QuickBooks expects a
// single signed AMOUNT per split (debit positive, credit negative); we keep the
// values as exact decimal strings, only prefixing a minus for credits.
func buildGLIIF(lines []accounting.JournalExportRow) []byte {
	var buf bytes.Buffer
	// Definition rows.
	buf.WriteString("!TRNS\tTRNSTYPE\tDATE\tACCNT\tNAME\tAMOUNT\tMEMO\n")
	buf.WriteString("!SPL\tTRNSTYPE\tDATE\tACCNT\tNAME\tAMOUNT\tMEMO\n")
	buf.WriteString("!ENDTRNS\n")

	// Group consecutive lines by entry_number (ExportJournalLines orders by
	// entry_number, account code, so an entry's lines are contiguous).
	i := 0
	for i < len(lines) {
		entryNum := lines[i].EntryNumber
		j := i
		for j < len(lines) && lines[j].EntryNumber == entryNum {
			j++
		}
		group := lines[i:j]
		for k := range group {
			g := group[k]
			tag := "SPL"
			if k == 0 {
				tag = "TRNS"
			}
			buf.WriteString(tag)
			buf.WriteByte('\t')
			buf.WriteString("GENERAL JOURNAL")
			buf.WriteByte('\t')
			buf.WriteString(g.EntryDate.Format("01/02/2006"))
			buf.WriteByte('\t')
			buf.WriteString(iifClean(g.AccountName))
			buf.WriteByte('\t')
			buf.WriteByte('\t') // NAME (customer/vendor) — not modelled
			buf.WriteString(iifSignedAmount(g.Debit, g.Credit))
			buf.WriteByte('\t')
			buf.WriteString(iifClean(glMemo(g)))
			buf.WriteByte('\n')
		}
		buf.WriteString("ENDTRNS\n")
		i = j
	}
	return buf.Bytes()
}

// iifSignedAmount returns the QuickBooks signed amount for a line: the debit as
// a positive decimal string, or the credit negated. Values stay exact strings;
// a credit is emitted with a leading minus.
func iifSignedAmount(debit, credit string) string {
	if d := glAmount(debit); d != "" {
		return d
	}
	if c := glAmount(credit); c != "" {
		return "-" + c
	}
	return "0"
}

// iifClean strips tab and newline characters that would break IIF's
// tab-delimited, line-oriented framing.
func iifClean(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch r {
		case '\t', '\n', '\r':
			out = append(out, ' ')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}
