package server

import "github.com/go-chi/chi/v5"

// registerReportInsightsRoutes (REPORTING): deterministic insight + data-quality
// annotations for the signature reports. Each report key is mounted as its own
// path with the same read permission as the dashboard it mirrors, so the
// station-scoped reports are gated by the matching *.read permission and the
// tenant-wide customer-aging report by customer.read. Mounted inside the
// admin-console group (requireAuth + rateLimitPerTenant).
//
// The reportKey is fixed per route (not read from the URL) so permission gating
// is static and unambiguous; the handler factory closes over the key.
func (s *Server) registerReportInsightsRoutes(r chi.Router) {
	r.With(s.requirePermissionHeld("revenue.read")).
		Get("/reports/daily-close/insights", s.handleReportInsights("daily-close"))
	r.With(s.requirePermissionHeld("revenue.read")).
		Get("/reports/sales-summary/insights", s.handleReportInsights("sales-summary"))
	r.With(s.requirePermissionHeld("reconciliation.read")).
		Get("/reports/stock-reconciliation/insights", s.handleReportInsights("stock-reconciliation"))
	r.With(s.requirePermissionHeld("finance.read")).
		Get("/reports/cash-reconciliation/insights", s.handleReportInsights("cash-reconciliation"))
	r.With(s.requirePermissionHeld("customer.read")).
		Get("/reports/customer-aging/insights", s.handleReportInsights("customer-aging"))
}
