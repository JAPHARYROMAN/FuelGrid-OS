package server

import "github.com/go-chi/chi/v5"

// registerReportExcelRoutes (REPORTING): .xlsx exports mirroring the existing
// revenue / reconciliation / financials CSV exports. Each is permission-gated
// and audited identically to its CSV counterpart (see reports_excel_handlers.go
// and writeExportFile). Mounted inside the admin-console group.
func (s *Server) registerReportExcelRoutes(r chi.Router) {
	r.With(s.requirePermission("revenue.read", stationFromURLParam("stationID"))).
		Get("/stations/{stationID}/reports/revenue.xlsx", s.handleExportRevenueXLSX)
	r.With(s.requirePermission("reconciliation.read", stationFromURLParam("stationID"))).
		Get("/stations/{stationID}/reports/reconciliation.xlsx", s.handleExportReconciliationXLSX)
	r.With(s.requirePermissionHeld("finance.read")).
		Get("/reports/financials.xlsx", s.handleExportFinancialsXLSX)
}
