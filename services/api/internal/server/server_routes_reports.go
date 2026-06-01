package server

import "github.com/go-chi/chi/v5"

// registerReportsRoutes (REPORTS): standard CSV report exports built from the
// existing overview/service data. Each route reuses the matching read
// permission of the dashboard it mirrors and records the export in the audit
// log (see reports_handlers.go). Mounted inside the admin-console group
// (requireAuth + rateLimitPerTenant) established in registerRoutes.
func (s *Server) registerReportsRoutes(r chi.Router) {
	// Station-scoped operational reports — gated by the same per-station read
	// permission as the corresponding overview endpoint, scoped to the URL
	// station so an out-of-scope station is 403 / a cross-tenant one 404.
	r.With(s.requirePermission("revenue.read", stationFromURLParam("stationID"))).
		Get("/stations/{stationID}/reports/revenue.csv", s.handleExportRevenueCSV)
	r.With(s.requirePermission("inventory.read", stationFromURLParam("stationID"))).
		Get("/stations/{stationID}/reports/inventory.csv", s.handleExportInventoryCSV)
	r.With(s.requirePermission("reconciliation.read", stationFromURLParam("stationID"))).
		Get("/stations/{stationID}/reports/reconciliation.csv", s.handleExportReconciliationCSV)

	// Formal printable documents (PDF), gated and audited identically to their
	// CSV counterparts: a daily shift/close report per station (revenue.read)
	// and the tenant financial statement (finance.read).
	r.With(s.requirePermission("revenue.read", stationFromURLParam("stationID"))).
		Get("/stations/{stationID}/reports/daily-close.pdf", s.handleExportDailyClosePDF)

	// Tenant-wide financial reports — gated by finance.read / customer.read,
	// matching the JSON report endpoints they mirror.
	r.With(s.requirePermissionHeld("finance.read")).
		Get("/reports/financials.csv", s.handleExportFinancialsCSV)
	r.With(s.requirePermissionHeld("finance.read")).
		Get("/reports/financials.pdf", s.handleExportFinancialsPDF)
	r.With(s.requirePermissionHeld("customer.read")).
		Get("/reports/ar-aging.csv", s.handleExportARagingCSV)
}
