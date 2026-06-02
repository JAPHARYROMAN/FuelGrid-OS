package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// Standard report exports (REPORTS).
//
// These endpoints return ready-to-open CSV files for the standard operational
// and financial reports, built from the same overview/service data the
// dashboards use, with money/litres carried as exact decimal strings (never
// floats — the figures are written into the CSV verbatim). Each export is
// itself an audited event (action 'report.exported'), mirroring the audit-log
// CSV export in export_handlers.go: a content checksum is computed and the run
// recorded via audit.WriteWithOutbox so the act of exporting is provably
// logged.
//
// Unlike the JSON-wrapping accounting exporter, these stream the CSV body
// directly with a Content-Disposition attachment so the browser BFF can hand
// the file straight to a download.

// writeReportCSV serialises the records to CSV, records the export in the audit
// log (action 'report.exported') with a content checksum, and — on success —
// streams the CSV as a downloadable attachment. records[0] must be the header
// row. reportType is the stable slug stored on the audit entry; filename is the
// download filename; meta is merged into the audited NewValue alongside the
// row count and checksum. Any failure writes a JSON error and returns early; on
// success it writes the CSV body. This is always the handler's final step.
func (s *Server) writeReportCSV(
	w http.ResponseWriter, r *http.Request, actor identity.Actor,
	reportType, filename string, records [][]string, meta map[string]any,
) {
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	if err := cw.WriteAll(records); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	sum := sha256.Sum256(buf.Bytes())
	checksum := hex.EncodeToString(sum[:])
	rowCount := len(records) - 1 // exclude header
	exportID := uuid.New()

	newValue := map[string]any{"report_type": reportType, "row_count": rowCount, "checksum": checksum}
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
		s.logger.Error("report export audit", "error", err, "report_type", reportType)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("X-Export-Id", exportID.String())
	w.Header().Set("X-Export-Checksum", checksum)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// handleExportRevenueCSV streams a station's recent revenue-day trend as CSV
// (gross/net/tax/cogs/margin + the per-tender split, all decimal strings).
// Gated by revenue.read for the URL station via the route.
func (s *Server) handleExportRevenueCSV(w http.ResponseWriter, r *http.Request) {
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

	days, err := s.revenue.RecentDays(ctx, actor.TenantID, stationID, 90)
	if err != nil {
		s.logger.Error("export revenue: recent days", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	records := [][]string{{
		"business_date", "status", "gross_revenue", "net_revenue", "tax_total",
		"cogs_total", "margin_total", "cash_total", "mobile_money_total", "card_total",
		"credit_total", "voucher_total", "tender_total", "cash_variance",
	}}
	for i := range days {
		d := days[i]
		records = append(records, []string{
			d.BusinessDate.Format(dateLayout), d.Status, d.GrossRevenue, d.NetRevenue, d.TaxTotal,
			d.CogsTotal, d.MarginTotal, d.CashTotal, d.MobileMoneyTotal, d.CardTotal,
			d.CreditTotal, d.VoucherTotal, d.TenderTotal, d.CashVariance,
		})
	}

	filename := fmt.Sprintf("revenue-%s.csv", station.Code)
	s.writeReportCSV(w, r, actor, "revenue_day", filename, records, map[string]any{
		"station_id": stationID.String(), "station_code": station.Code,
	})
}

// handleExportInventoryCSV streams a station's current inventory snapshot as
// CSV: each tank's book balance (exact decimal string), latest physical dip,
// and its latest reconciliation variance. Gated by inventory.read for the URL
// station via the route.
func (s *Server) handleExportInventoryCSV(w http.ResponseWriter, r *http.Request) {
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

	tankRows, err := s.tanks.List(ctx, actor.TenantID, []uuid.UUID{stationID})
	if err != nil {
		s.logger.Error("export inventory: tanks", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	latest, err := s.readings.LatestDipsForStation(ctx, actor.TenantID, stationID)
	if err != nil {
		s.logger.Error("export inventory: latest dips", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	records := [][]string{{
		"tank_code", "tank_name", "capacity_litres", "book_balance_litres",
		"latest_physical_litres", "latest_physical_at",
		"last_variance_litres", "last_variance_percent", "last_reconciliation_status",
	}}
	for i := range tankRows {
		tank := tankRows[i]
		book, err := s.inventory.CurrentBalance(ctx, actor.TenantID, tank.ID)
		if err != nil {
			s.logger.Error("export inventory: balance", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		physical, physicalAt := "", ""
		if ld, ok := latest[tank.ID]; ok {
			// VolumeLitres is already an exact-decimal string (numeric text).
			physical = ld.VolumeLitres
			physicalAt = ld.RecordedAt.Format(time.RFC3339)
		}
		varLitres, varPercent, recStatus := "", "", ""
		recent, err := s.reconciliation.RecentForTank(ctx, actor.TenantID, tank.ID, 1)
		if err != nil {
			s.logger.Error("export inventory: recent reconciliations", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if len(recent) > 0 {
			varLitres = recent[0].VarianceLitres
			varPercent = recent[0].VariancePercent
			recStatus = recent[0].Status
		}
		records = append(records, []string{
			tank.Code, tank.Name, tank.CapacityLitres, book,
			physical, physicalAt, varLitres, varPercent, recStatus,
		})
	}

	filename := fmt.Sprintf("inventory-%s.csv", station.Code)
	s.writeReportCSV(w, r, actor, "inventory_snapshot", filename, records, map[string]any{
		"station_id": stationID.String(), "station_code": station.Code,
	})
}

// handleExportReconciliationCSV streams a station's per-tank reconciliations
// for the active (or ?operating_day_id) day as CSV: the full book→physical
// variance breakdown, all decimal strings. Gated by reconciliation.read for the
// URL station via the route.
func (s *Server) handleExportReconciliationCSV(w http.ResponseWriter, r *http.Request) {
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

	// Resolve the day: an explicit ?operating_day_id, else the latest active day.
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

	records := [][]string{{
		"tank_id", "business_date", "opening_book", "deliveries_total", "sales_total",
		"adjustments_total", "closing_book", "closing_physical", "variance_litres",
		"variance_percent", "tolerance_percent", "status",
	}}
	if dayID != uuid.Nil {
		recs, rerr := s.reconciliation.ListForStationDay(ctx, actor.TenantID, stationID, dayID)
		if rerr != nil {
			s.logger.Error("export reconciliation: list", "error", rerr)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for i := range recs {
			rec := recs[i]
			records = append(records, []string{
				rec.TankID.String(), businessDate, rec.OpeningBook, rec.DeliveriesTotal, rec.SalesTotal,
				rec.AdjustmentsTotal, rec.ClosingBook, rec.ClosingPhysical, rec.VarianceLitres,
				rec.VariancePercent, rec.TolerancePercent, rec.Status,
			})
		}
	}

	filename := fmt.Sprintf("reconciliation-%s.csv", station.Code)
	s.writeReportCSV(w, r, actor, "reconciliation", filename, records, map[string]any{
		"station_id": stationID.String(), "station_code": station.Code,
		"operating_day_id": dayID.String(), "business_date": businessDate,
	})
}

// handleExportFinancialsCSV streams the tenant's financial statements (P&L and
// balance sheet) as a single CSV of labelled line items, all decimal strings.
// The reporting window is a ?period query param: this-month (default),
// last-month, ytd, or last-30. Gated by finance.read via the route.
func (s *Server) handleExportFinancialsCSV(w http.ResponseWriter, r *http.Request) {
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

	records := [][]string{
		{"statement", "line_item", "amount"},
		{"income_statement", "revenue", is.Revenue},
		{"income_statement", "expenses", is.Expenses},
		{"income_statement", "net_profit", is.NetProfit},
		{"balance_sheet", "assets", bs.Assets},
		{"balance_sheet", "liabilities", bs.Liabilities},
		{"balance_sheet", "equity", bs.Equity},
		{"balance_sheet", "retained_earnings", bs.RetainedEarnings},
		{"balance_sheet", "net_income", bs.NetIncome},
		{"balance_sheet", "balanced", strconv.FormatBool(bs.Balanced)},
	}

	filename := fmt.Sprintf("financials-%s.csv", label)
	s.writeReportCSV(w, r, actor, "financials", filename, records, map[string]any{
		"period": label, "from": from.Format(dateLayout), "to": to.Format(dateLayout),
	})
}

// handleExportARagingCSV streams the tenant's accounts-receivable aging as CSV:
// every credit customer with a non-zero balance (decimal string). Gated by
// customer.read via the route.
func (s *Server) handleExportARagingCSV(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.receivables.Aging(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("export ar aging", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	records := [][]string{{"customer_id", "code", "name", "balance"}}
	for i := range rows {
		records = append(records, []string{
			rows[i].CustomerID.String(), rows[i].Code, rows[i].Name, rows[i].Balance,
		})
	}
	s.writeReportCSV(w, r, actor, "ar_aging", "ar-aging.csv", records, nil)
}

// resolveReportPeriod maps a period slug to a [from, to] date window and a
// stable label used in the audit entry and download filename. Unknown slugs
// fall back to the current calendar month.
func resolveReportPeriod(period string, now time.Time) (from, to time.Time, label string) {
	y, m, _ := now.Date()
	monthStart := time.Date(y, m, 1, 0, 0, 0, 0, now.Location())
	switch period {
	case "last-month":
		prev := monthStart.AddDate(0, -1, 0)
		return prev, monthStart.AddDate(0, 0, -1), "last-month"
	case "ytd":
		return time.Date(y, 1, 1, 0, 0, 0, 0, now.Location()), now, "ytd"
	case "last-30":
		return now.AddDate(0, 0, -30), now, "last-30"
	default:
		return monthStart, now, "this-month"
	}
}
