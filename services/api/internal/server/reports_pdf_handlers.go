package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/revenue"
)

// PDF report exports (REPORTS-PDF).
//
// The formal-document counterparts to the CSV exports in reports_handlers.go:
// a printable daily shift/close report (per station) and a financial statement
// (balance sheet + P&L, tenant-wide). They are gated by the identical read
// permissions and audited identically — the same 'report.exported' event with a
// content checksum, so exporting a PDF is just as provably logged as a CSV. The
// PDF body is rendered by reports_pdf.go from the exact decimal strings the
// repos return (no float money).

// writeReportPDF records the export in the audit log (action 'report.exported')
// with a content checksum and — on success — streams the PDF as a downloadable
// attachment. reportType is the stable slug stored on the audit entry (suffixed
// _pdf to distinguish it from the CSV of the same report); filename is the
// download filename; meta is merged into the audited NewValue alongside the
// byte count and checksum. Any failure writes a JSON error and returns early.
// This is always the handler's final step.
func (s *Server) writeReportPDF(
	w http.ResponseWriter, r *http.Request, actor identity.Actor,
	reportType, filename string, body []byte, meta map[string]any,
) {
	sum := sha256.Sum256(body)
	checksum := hex.EncodeToString(sum[:])
	exportID := uuid.New()

	newValue := map[string]any{
		"report_type": reportType, "format": "pdf",
		"byte_count": len(body), "checksum": checksum,
	}
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
		s.logger.Error("report pdf export audit", "error", err, "report_type", reportType)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("X-Export-Id", exportID.String())
	w.Header().Set("X-Export-Checksum", checksum)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// handleExportDailyClosePDF renders a station's daily shift/close report as a
// branded PDF: the resolved business day's headline figures (gross/net, tax,
// COGS, margin, cash variance) and the per-tender breakdown, then a trend table
// of recent revenue days. Built from the same revenue-day data as the revenue
// CSV. The day is the latest active operating day (or ?operating_day_id).
// Gated by revenue.read for the URL station via the route.
func (s *Server) handleExportDailyClosePDF(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}
	ctx := r.Context()
	station, err := s.stations.Get(ctx, actor.TenantID, stationID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Resolve the close day: an explicit ?operating_day_id, else the latest
	// active day for the station. Mirrors the reconciliation CSV's resolution.
	var dayID uuid.UUID
	var businessDate string
	if raw := r.URL.Query().Get("operating_day_id"); raw != "" {
		dayID, err = uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid operating_day_id")
			return
		}
		day, derr := s.operations.GetDay(ctx, actor.TenantID, dayID)
		if errors.Is(derr, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "operating day not found")
			return
		}
		if derr != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		businessDate = day.BusinessDate.Format(dateLayout)
	} else {
		day, derr := s.operations.LatestActiveDayForStation(ctx, actor.TenantID, stationID)
		if derr == nil {
			dayID = day.ID
			businessDate = day.BusinessDate.Format(dateLayout)
		} else if !errors.Is(derr, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	days, err := s.revenue.RecentDays(ctx, actor.TenantID, stationID, 30)
	if err != nil {
		s.logger.Error("export daily-close pdf: recent days", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// The headline day is the one matching dayID if we resolved one, else the
	// most recent revenue day on record.
	var headline *revenue.RevenueDay
	for i := range days {
		d := days[i]
		if dayID != uuid.Nil && d.OperatingDayID == dayID {
			headline = &days[i]
			break
		}
	}
	if headline == nil && dayID == uuid.Nil && len(days) > 0 {
		headline = &days[0] // RecentDays is newest-first
	}
	if businessDate == "" && headline != nil {
		businessDate = headline.BusinessDate.Format(dateLayout)
	}

	periodLine := fmt.Sprintf("Station %s — %s", station.Code, station.Name)
	if businessDate != "" {
		periodLine += "  •  Business date " + businessDate
	}
	doc := newLetterheadDoc(s.loadLetterhead(r, actor.TenantID), LetterheadOptions{
		Title:    "Daily Shift & Close Report",
		SubLines: []string{periodLine},
	})

	if headline != nil {
		doc.sectionHeading("Close summary (" + headline.BusinessDate.Format(dateLayout) + ", status: " + headline.Status + ")")
		doc.keyValue("Gross revenue", headline.GrossRevenue)
		doc.keyValue("Tax", headline.TaxTotal)
		doc.keyValue("Net revenue", headline.NetRevenue)
		doc.keyValue("Cost of goods sold", headline.CogsTotal)
		doc.totalRow("Margin", headline.MarginTotal)

		doc.sectionHeading("Tender breakdown")
		doc.keyValue("Cash", headline.CashTotal)
		doc.keyValue("Mobile money", headline.MobileMoneyTotal)
		doc.keyValue("Card", headline.CardTotal)
		doc.keyValue("Credit", headline.CreditTotal)
		doc.keyValue("Voucher", headline.VoucherTotal)
		doc.totalRow("Total tendered", headline.TenderTotal)
		doc.keyValue("Cash variance (tender - gross)", headline.CashVariance)
	} else {
		doc.sectionHeading("Close summary")
		doc.note("No revenue day has been computed for this station yet.")
	}

	// Recent-days trend table.
	doc.sectionHeading("Recent revenue days")
	if len(days) == 0 {
		doc.note("No revenue days on record.")
	} else {
		rows := make([][]string, 0, len(days))
		for i := range days {
			d := days[i]
			rows = append(rows, []string{
				d.BusinessDate.Format(dateLayout), d.Status,
				d.GrossRevenue, d.NetRevenue, d.MarginTotal, d.TenderTotal, d.CashVariance,
			})
		}
		doc.table(
			[]string{"Date", "Status", "Gross", "Net", "Margin", "Tendered", "Variance"},
			[]float64{24, 20, 27, 27, 27, 28, 27},
			[]string{"L", "L", "R", "R", "R", "R", "R"},
			rows,
		)
	}

	doc.note("All money figures are exact decimals, identical to the CSV export. This document is recorded in the audit log.")

	body, err := doc.bytes()
	if err != nil {
		s.logger.Error("export daily-close pdf: render", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	filename := fmt.Sprintf("daily-close-%s.pdf", station.Code)
	s.writeReportPDF(w, r, actor, "daily_close_pdf", filename, body, map[string]any{
		"station_id": stationID.String(), "station_code": station.Code,
		"operating_day_id": dayID.String(), "business_date": businessDate,
	})
}

// handleExportFinancialsPDF renders the tenant's financial statements (P&L and
// balance sheet) as a branded PDF for the ?period window (this-month default,
// last-month, ytd, last-30). Built from the same accounting figures as the
// financials CSV. Gated by finance.read via the route.
func (s *Server) handleExportFinancialsPDF(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx := r.Context()
	period := r.URL.Query().Get("period")
	from, to, label := resolveReportPeriod(period, time.Now())

	is, err := s.accounting.IncomeStatement(ctx, actor.TenantID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	bs, err := s.accounting.BalanceSheet(ctx, actor.TenantID, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	periodLine := fmt.Sprintf("Period %s  •  %s to %s",
		label, from.Format(dateLayout), to.Format(dateLayout))
	doc := newLetterheadDoc(s.loadLetterhead(r, actor.TenantID), LetterheadOptions{
		Title:    "Financial Statement",
		SubLines: []string{periodLine},
	})

	doc.sectionHeading("Profit & Loss")
	doc.keyValue("Revenue", is.Revenue)
	doc.keyValue("Expenses", is.Expenses)
	doc.totalRow("Net profit", is.NetProfit)

	doc.sectionHeading("Balance Sheet (as of " + to.Format(dateLayout) + ")")
	doc.keyValue("Assets", bs.Assets)
	doc.keyValue("Liabilities", bs.Liabilities)
	doc.keyValue("Retained earnings", bs.RetainedEarnings)
	doc.keyValue("Net income (period to date)", bs.NetIncome)
	doc.totalRow("Equity", bs.Equity)

	balanceNote := "Balance check: Assets = Liabilities + Equity holds to the cent."
	if !bs.Balanced {
		balanceNote = "Balance check: the books do NOT balance for this date — review posted journals."
	}
	doc.note(balanceNote)
	doc.note("All money figures are exact decimals, identical to the CSV export. This document is recorded in the audit log.")

	body, err := doc.bytes()
	if err != nil {
		s.logger.Error("export financials pdf: render", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	filename := fmt.Sprintf("financials-%s.pdf", label)
	s.writeReportPDF(w, r, actor, "financials_pdf", filename, body, map[string]any{
		"period": label, "from": from.Format(dateLayout), "to": to.Format(dateLayout),
	})
}
