package server

import "github.com/go-chi/chi/v5"

// registerReportsStructuredRoutes (REPORTS-STRUCTURED): the structured,
// insight-bearing report API that returns the drillable ReportEnvelope (not just
// CSV). Each route is permission-gated by the read permission of the domain it
// reports on and tenant-scoped by the repos; the station-scoped reports take the
// station from ?station_id and re-check it in-handler via authorizeStation (so an
// out-of-scope station 403s, a cross-tenant one 404s). Mounted inside the
// admin-console group (requireAuth + rateLimitPerTenant) established in
// registerRoutes. The pre-existing CSV/PDF/XLSX export endpoints stay mounted and
// authoritative; the unified POST /reports/export only delegates to their URLs.
func (s *Server) registerReportsStructuredRoutes(r chi.Router) {
	// Reports landing: categories + live headline metric + alert/DQ count.
	r.With(s.requirePermissionHeld("finance.read")).
		Get("/reports/overview", s.handleReportsOverview)

	// Reports & Intelligence Center catalog (Phase 1): the 16 blueprint
	// categories as data, each permission-filtered, with a live key metric +
	// alert count and a hub-level data-quality band. Coarse-gated by
	// reports.read; each category is additionally filtered by its own
	// required_permission inside the handler.
	r.With(s.requirePermissionHeld("reports.read")).
		Get("/reports/catalog", s.handleReportCatalog)

	// Inventory reconciliation waterfall (station-scoped via ?station_id).
	r.With(s.requirePermissionHeld("reconciliation.read")).
		Get("/reports/inventory/reconciliation", s.handleReconciliationReport)

	// Daily station close (station-scoped via ?station_id).
	r.With(s.requirePermissionHeld("revenue.read")).
		Get("/reports/station-close", s.handleStationCloseReport)

	// Cash reconciliation (station-scoped via ?station_id).
	r.With(s.requirePermissionHeld("finance.read")).
		Get("/reports/cash-reconciliation", s.handleCashReconciliationReport)

	// Fuel loss (station-scoped via ?station_id).
	r.With(s.requirePermissionHeld("reconciliation.read")).
		Get("/reports/fuel-loss", s.handleFuelLossReport)

	// Risk & Loss intelligence (§5.11 / §20.4) — the signature loss report:
	// loss litres + value (value gated by margin.view), variance %, open
	// alerts/investigations, repeated incidents and highest-risk station KPIs; the
	// DETERMINISTIC §5.11 pattern intelligence (variance events by
	// station/product/pump/shift/attendant → "% of related events" findings); a
	// risk heatmap, loss trend, station ranking, root-cause donut, alert-severity
	// board and investigation timeline; and a read-only risk-rules tuning context.
	// Station-scoped via ?station_id, gated by reconciliation.read; the loss VALUE
	// is margin.view-gated in-handler and OMITTED for non-holders.
	r.With(s.requirePermissionHeld("reconciliation.read")).
		Get("/reports/risk-loss", s.handleRiskLossReport)

	// Sales report (§5.2) — litres/revenue/avg-price/txn-count/growth KPIs, the
	// revenue trend, product / payment / shift / attendant / nozzle breakdowns, a
	// peak-hours grid and an optional cross-station ranking. Station-scoped via
	// ?station_id; margin/cost are margin.view-gated in-handler.
	r.With(s.requirePermissionHeld("revenue.read")).
		Get("/reports/sales", s.handleSalesReport)

	// Delivery & Procurement report (§5.7) — ordered/loaded/received comparison,
	// delivery variance, delivery delays, PO pipeline and a deterministic supplier
	// scorecard. Station-scoped via ?station_id; supplier cost / price
	// competitiveness are margin.view-gated in-handler.
	r.With(s.requirePermissionHeld("station.read")).
		Get("/reports/delivery", s.handleDeliveryReport)

	// Customer Credit (§5.9) — tenant-wide receivables aging into Current /
	// 1-30 / 31-60 / 61-90 / 90+ buckets, credit-limit utilization, top-overdue
	// ranking, risk badges and a per-customer drilldown. Gated by customer.read
	// (the receivables permission); CREDIT EXPOSURE figures are gated in-handler
	// by customer_credit.read.
	r.With(s.requirePermissionHeld("customer.read")).
		Get("/reports/customer-credit", s.handleCustomerCreditReport)
	r.With(s.requirePermissionHeld("customer.read")).
		Get("/reports/customer-credit/drilldown", s.handleCustomerCreditDrilldown)

	// Profitability P&L (station-scoped via ?station_id; Feature 10.4).
	r.With(s.requirePermissionHeld("revenue.read")).
		Get("/reports/profitability", s.handleProfitabilityReport)

	// Finance P&L (§5.8) — station-scoped via ?station_id: a revenue → COGS →
	// gross margin → expenses → net operating result P&L waterfall, period
	// comparison, cash position, per-product breakdown, settlement/period status
	// chips and the embedded finance statements. Gated by finance.read; COGS /
	// margin are margin.view-gated in-handler.
	r.With(s.requirePermissionHeld("finance.read")).
		Get("/reports/finance", s.handleFinanceReport)

	// Credit & cashflow (station-scoped via ?station_id; Feature 10.5).
	r.With(s.requirePermissionHeld("revenue.read")).
		Get("/reports/credit-cashflow", s.handleCreditCashflowReport)

	// Station comparison (tenant-wide gate; rows filtered to the actor's
	// accessible stations in-handler; Feature 10.6).
	r.With(s.requirePermissionHeld("revenue.read")).
		Get("/reports/station-comparison", s.handleStationComparisonReport)

	// Executive Business Report (§5.1 / §20.1) — the cross-domain leadership
	// cockpit that CONSOLIDATES the per-domain reports into one drillable view: a
	// company-wide (or scope-wide) revenue / litres / margin (gated) / loss (value
	// gated) / cash / credit (gated) / risk / approvals KPI hero, the
	// DETERMINISTIC §5.1 automated management narrative (period-over-period prose,
	// every sentence traceable to a computed figure — no AI), and the reusable
	// visuals (revenue+volume ranking, P&L waterfall, period-comparison cards,
	// loss summary). Tenant-wide gate (finance.read held anywhere); the ROLLUP is
	// restricted to the actor's accessible stations (stationScope) so cross-scope
	// leakage is impossible. Margin / loss value / credit exposure are gated and
	// OMITTED for non-holders.
	r.With(s.requirePermissionHeld("finance.read")).
		Get("/reports/executive", s.handleExecutiveReport)

	// Attendance dataset (station-scoped via ?station_id + ?from/?to window;
	// Mobile Attendant Phase 7): roster vs check-in/out with late / no-show
	// derivation. Rides station.read, the operations-domain read permission.
	r.With(s.requirePermissionHeld("station.read")).
		Get("/reports/attendance", s.handleAttendanceReport)

	// Corrections & variances dataset (station-scoped via ?station_id +
	// ?from/?to window; Mobile Attendant Phase 7): submitted vs final approved
	// readings + reason, and expected vs received collections + difference.
	r.With(s.requirePermissionHeld("station.read")).
		Get("/reports/corrections-variances", s.handleCorrectionsVariancesReport)

	// Unified export entry point — delegates to the existing export endpoints.
	r.With(s.requirePermissionHeld("finance.read")).
		Post("/reports/export", s.handleExportReport)

	// Export-jobs surface (Feature 10.7 + Reports Center Phase 13 — the Export
	// Center): the async export queue + history, gated by reports.export. POST
	// enqueues a job (the worker re-checks permission at generation, re-runs the
	// report, renders + stores the file bytes in Postgres); GET lists/reads job
	// status; GET .../download streams the stored bytes (permission re-checked at
	// delivery). A report the worker cannot render falls back to a legacy receipt.
	r.With(s.requirePermissionHeld("reports.export")).Group(func(r chi.Router) {
		r.Post("/exports", s.handleCreateExportJob)
		r.Get("/exports", s.handleListExportJobs)
		r.Get("/exports/{id}", s.handleGetExportJob)
		r.Get("/exports/{id}/download", s.handleDownloadExportJob)
	})
}
