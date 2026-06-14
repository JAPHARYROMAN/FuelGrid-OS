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

	// Profitability P&L (station-scoped via ?station_id; Feature 10.4).
	r.With(s.requirePermissionHeld("revenue.read")).
		Get("/reports/profitability", s.handleProfitabilityReport)

	// Credit & cashflow (station-scoped via ?station_id; Feature 10.5).
	r.With(s.requirePermissionHeld("revenue.read")).
		Get("/reports/credit-cashflow", s.handleCreditCashflowReport)

	// Station comparison (tenant-wide gate; rows filtered to the actor's
	// accessible stations in-handler; Feature 10.6).
	r.With(s.requirePermissionHeld("revenue.read")).
		Get("/reports/station-comparison", s.handleStationComparisonReport)

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

	// Export-jobs surface (Feature 10.7): a durable receipt + history of report
	// exports, gated by reports.export. POST records a job and maps it onto the
	// existing synchronous export file URL; GET lists/reads the history.
	r.With(s.requirePermissionHeld("reports.export")).Group(func(r chi.Router) {
		r.Post("/exports", s.handleCreateExportJob)
		r.Get("/exports", s.handleListExportJobs)
		r.Get("/exports/{id}", s.handleGetExportJob)
	})
}
