package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/reporting"
)

// Credit & cashflow structured report (Feature 10.5).
//
// Returns the shared ReportEnvelope (report_envelope.go) and reuses the SAME
// recorded-tender, customer-payment, customer-invoice, supplier-payment and
// cash-reconciliation facts the dashboards use — no money figure is recomputed
// in Go float. Every total is summed in SQL ::numeric (revenue.Cashflow) and
// carried through as an exact decimal string; the deterministic insights +
// data-quality come from internal/reporting verbatim.
//
// Station-scoped (?station_id, revenue.read) with an in-handler authorizeStation
// so an out-of-scope station 403s and a cross-tenant one 404s. Supplier payments
// are a tenant-wide figure (payables carry no station) and are labelled as such.

// handleCreditCashflowReport returns a station's credit & cashflow picture over a
// period as a ReportEnvelope: cash / mobile-money / card / credit / voucher
// sales, collections, outstanding + overdue receivables, supplier payments, cash
// variance, and the realized (projected) cash position. Station-scoped, gated by
// revenue.read. ?period selects the date window (this-month default).
func (s *Server) handleCreditCashflowReport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, ok := s.resolveStationScoped(w, r, actor, "revenue.read")
	if !ok {
		return
	}
	ctx := r.Context()
	from, to, period := resolveReportPeriod(r.URL.Query().Get("period"), time.Now())
	sid := stationID.String()
	env := newEnvelope("credit-cashflow", "Credit & Cashflow", period, &sid)
	env.FiltersUsed["station_id"] = sid
	env.FiltersUsed["period"] = period
	env.FiltersUsed["from"] = from.Format(dateLayout)
	env.FiltersUsed["to"] = to.Format(dateLayout)

	totals, terr := s.revenue.Cashflow(ctx, s.deps.DB, actor.TenantID, stationID, from, to)
	if terr != nil {
		s.logger.Error("credit-cashflow report: totals", "error", terr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	days, unlocked, lerr := s.revenue.WindowLockState(ctx, actor.TenantID, stationID, from, to)
	if lerr != nil {
		s.logger.Error("credit-cashflow report: lock state", "error", lerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	env.Summary = []summaryMetric{
		{Label: "Cash sales", Value: totals.CashSales, Unit: "TZS"},
		{Label: "Mobile-money sales", Value: totals.MobileMoneySales, Unit: "TZS"},
		{Label: "Card sales", Value: totals.CardSales, Unit: "TZS"},
		{Label: "Credit sales", Value: totals.CreditSales, Unit: "TZS"},
		{Label: "Total tendered", Value: totals.TotalTendered, Unit: "TZS"},
		{Label: "Collections", Value: totals.Collections, Unit: "TZS"},
		{Label: "Outstanding receivables", Value: totals.OutstandingAR, Unit: "TZS"},
		{Label: "Overdue receivables", Value: totals.OverdueAR, Unit: "TZS"},
		{Label: "Supplier payments (network)", Value: totals.SupplierPayments, Unit: "TZS"},
		{Label: "Cash variance", Value: totals.CashVariance, Unit: "TZS"},
		{Label: "Projected cash position", Value: totals.ProjectedCashPos, Unit: "TZS"},
	}

	// Tender-mix chart + a flat fact table (decimal strings throughout).
	type tenderSlice struct {
		Tender string `json:"tender"`
		Amount string `json:"amount"`
	}
	env.ChartData = []tenderSlice{
		{Tender: "Cash", Amount: totals.CashSales},
		{Tender: "Mobile money", Amount: totals.MobileMoneySales},
		{Tender: "Card", Amount: totals.CardSales},
		{Tender: "Credit", Amount: totals.CreditSales},
		{Tender: "Voucher", Amount: totals.VoucherSales},
	}

	env.Table.Columns = []string{"metric", "amount"}
	env.Table.Rows = [][]string{
		{"Cash sales", totals.CashSales},
		{"Mobile-money sales", totals.MobileMoneySales},
		{"Card sales", totals.CardSales},
		{"Credit sales", totals.CreditSales},
		{"Voucher sales", totals.VoucherSales},
		{"Total tendered", totals.TotalTendered},
		{"Collections", totals.Collections},
		{"Outstanding receivables", totals.OutstandingAR},
		{"Overdue receivables", totals.OverdueAR},
		{"Supplier payments (network)", totals.SupplierPayments},
		{"Cash variance", totals.CashVariance},
		{"Projected cash position", totals.ProjectedCashPos},
	}

	hasActivity := totals.TenderCount > 0 ||
		nonZeroMoney(totals.Collections) || nonZeroMoney(totals.OutstandingAR)
	env.applyReport(reporting.CreditCashflow(reporting.CashflowInput{
		CreditSales:      totals.CreditSales,
		TotalTendered:    totals.TotalTendered,
		Collections:      totals.Collections,
		OutstandingAR:    totals.OutstandingAR,
		OverdueAR:        totals.OverdueAR,
		SupplierPayments: totals.SupplierPayments,
		CashVariance:     totals.CashVariance,
		ProjectedCashPos: totals.ProjectedCashPos,
		HasActivity:      hasActivity,
		PeriodLocked:     days > 0 && unlocked == 0,
	}))
	if days == 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level: "warning", Message: "No revenue days recorded for this station in the period.",
		})
	}
	env.FiltersUsed["reconciliations"] = strconv.Itoa(totals.ReconciliationDays)

	env.Drilldown = []drilldownLink{
		{Label: "Cash reconciliation report", Href: "/api/v1/reports/cash-reconciliation?station_id=" + sid},
		{Label: "Receivables aging", Href: "/api/v1/reports/customer-aging/insights"},
		{Label: "Daily station close", Href: "/api/v1/reports/station-close?station_id=" + sid},
	}
	env.ExportOptions = []exportOption{
		{Format: "csv", URL: "/api/v1/reports/financials.csv?period=" + period},
		{Format: "xlsx", URL: "/api/v1/reports/financials.xlsx?period=" + period},
		{Format: "pdf", URL: "/api/v1/reports/financials.pdf?period=" + period},
	}
	writeJSON(w, http.StatusOK, env)
}

// nonZeroMoney reports whether a decimal money string is a non-zero amount.
func nonZeroMoney(s string) bool {
	v, ok := parseFloatSafe(s)
	return ok && v != 0
}
