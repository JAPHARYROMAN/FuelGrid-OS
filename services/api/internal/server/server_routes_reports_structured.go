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

	// Report snapshots & locking (Reports Center Phase 14 — blueprint §15). Each
	// route is gated INSIDE the handler by the SAME permission as running the
	// underlying report live (resolved from the report key / the snapshot's
	// captured filters via reportSpecFor + policy.Can) — a snapshot must never
	// expose data the actor cannot run live. The coarse route gate is reports.read
	// (every reports user reaches the surface); the in-handler per-report re-check
	// is the authoritative gate (403 when the actor cannot run that report).
	//
	// The literal "snapshots/{id}" routes are registered alongside the param
	// "{key}/snapshots" route; chi prefers the static "snapshots" segment over the
	// "{key}" wildcard, so a snapshot id is never mistaken for a report key.
	// Per-tenant Scheduled Reports (Reports Center Phase 12 — blueprint §8). The
	// management surface is gated by reports.schedule at the route; each write ALSO
	// re-checks the underlying report's OWN run permission in-handler (so a manager
	// can only schedule reports they can run), and the dispatcher re-checks per
	// recipient at delivery. Tenant-isolated; a cross-tenant {id} is a clean 404.
	r.With(s.requirePermissionHeld("reports.schedule")).Group(func(r chi.Router) {
		r.Post("/reports/scheduled", s.handleCreateScheduledReport)
		r.Get("/reports/scheduled", s.handleListScheduledReports)
		r.Get("/reports/scheduled/{id}", s.handleGetScheduledReport)
		r.Put("/reports/scheduled/{id}", s.handleUpdateScheduledReport)
		r.Delete("/reports/scheduled/{id}", s.handleDeleteScheduledReport)
		r.Post("/reports/scheduled/{id}/enabled", s.handleSetScheduledReportEnabled)
		r.Post("/reports/scheduled/{id}/run-now", s.handleRunScheduledReportNow)
		r.Get("/reports/scheduled/{id}/runs", s.handleListScheduledReportRuns)
	})

	// Report insight rules (Reports Center Phase 15 — blueprint §9 / §9.3 / §21.3 /
	// §23): the config-driven, tunable, auditable surface for the deterministic
	// rules that drive report insights. Mirrors /risk/rules — gated by
	// reports.rules.manage, tenant-isolated, audited; DELETE refuses a seeded
	// system rule (disable it instead). The engine itself is wired into each
	// report envelope (runReportRules), additive over the composer output.
	r.With(s.requirePermissionHeld("reports.rules.manage")).Group(func(r chi.Router) {
		r.Get("/reports/rules", s.handleListReportRules)
		r.Get("/reports/rules/{id}", s.handleGetReportRule)
		r.Post("/reports/rules", s.handleCreateReportRule)
		r.Put("/reports/rules/{id}", s.handleUpdateReportRule)
		r.Post("/reports/rules/{id}/enabled", s.handleSetReportRuleEnabled)
		r.Delete("/reports/rules/{id}", s.handleDeleteReportRule)
	})

	// Custom Report Builder (Reports Center Phase 11 — blueprint §6 / §22). The
	// whitelisted dataset registry + safe query composer: pick a dataset + a subset
	// of its allowlisted dimensions / measures / filters / sort + a visualization,
	// and the composer builds a parameterized, tenant- AND station-scoped query from
	// ONLY the registry's identifiers (NO free SQL). Coarse-gated by reports.builder
	// at the route; the DATASET's own permission is the authoritative data gate,
	// re-checked in-handler at preview AND run time, and sensitive columns (margin /
	// cost / exposure) are margin.view-gated and OMITTED for non-holders. Saved
	// templates enforce share-scope on read and ownership on edit/delete; writes are
	// audited. Results render through the shared ReportEnvelope.
	r.With(s.requirePermissionHeld("reports.builder")).Group(func(r chi.Router) {
		r.Get("/reports/builder/datasets", s.handleBuilderDatasets)
		r.Post("/reports/builder/preview", s.handleBuilderPreview)
		r.Post("/reports/builder/templates", s.handleCreateTemplate)
		r.Get("/reports/builder/templates", s.handleListTemplates)
		r.Get("/reports/builder/templates/{id}", s.handleGetTemplate)
		r.Put("/reports/builder/templates/{id}", s.handleUpdateTemplate)
		r.Delete("/reports/builder/templates/{id}", s.handleDeleteTemplate)
		r.Post("/reports/builder/templates/{id}/run", s.handleRunTemplate)
	})

	r.With(s.requirePermissionHeld("reports.read")).Group(func(r chi.Router) {
		// Recent signed-off snapshots across reports — the hub "Locked" rail. The
		// handler permission-filters each row by the underlying report's permission.
		r.Get("/reports/snapshots/recent", s.handleRecentLockedSnapshots)
		// Lock-state for a report/scope: does a signed-off snapshot exist? Drives
		// the lock badge on a report view.
		r.Get("/reports/{key}/lock-state", s.handleReportLockState)
		// Capture / list a report's snapshots (the revision chain).
		r.Post("/reports/{key}/snapshots", s.handleCaptureSnapshot)
		r.Get("/reports/{key}/snapshots", s.handleListSnapshots)
		// View a single snapshot's stored envelope + metadata.
		r.Get("/reports/snapshots/{id}", s.handleGetSnapshot)
		// Sign-off / reopen workflow.
		r.Post("/reports/snapshots/{id}/sign-off", s.handleSignOffSnapshot)
		r.Post("/reports/snapshots/{id}/reopen", s.handleReopenSnapshot)
	})
}
