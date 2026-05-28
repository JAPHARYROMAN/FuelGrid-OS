package server

import (
	"net/http"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// handleEnterpriseExceptions aggregates unresolved operational exceptions
// across domains for the enterprise command queue (Stage 12): open incidents,
// unresolved shift exceptions, unmatched bank lines, unposted cash
// reconciliations, open approval requests, and open credit alerts.
func (s *Server) handleEnterpriseExceptions(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx := r.Context()
	counts := map[string]int{}
	checks := []struct {
		key string
		sql string
	}{
		{"open_incidents", `SELECT count(*) FROM incidents WHERE tenant_id = $1 AND status NOT IN ('resolved','closed')`},
		{"unresolved_shift_exceptions", `SELECT count(*) FROM shift_exceptions WHERE tenant_id = $1 AND status <> 'resolved'`},
		{"unmatched_bank_lines", `SELECT count(*) FROM bank_statement_lines WHERE tenant_id = $1 AND status = 'unmatched'`},
		{"unposted_cash_reconciliations", `SELECT count(*) FROM cash_reconciliations WHERE tenant_id = $1 AND status <> 'posted'`},
		{"approvals_waiting", `SELECT count(*) FROM approval_requests WHERE tenant_id = $1 AND status = 'requested'`},
		{"open_credit_alerts", `SELECT count(*) FROM customer_credit_alerts WHERE tenant_id = $1 AND status IN ('open','acknowledged')`},
	}
	total := 0
	for _, c := range checks {
		var n int
		if err := s.deps.DB.QueryRow(ctx, c.sql, actor.TenantID).Scan(&n); err != nil {
			s.logger.Error("enterprise exceptions", "error", err, "check", c.key)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		counts[c.key] = n
		total += n
	}
	writeJSON(w, http.StatusOK, map[string]any{"checks": counts, "total": total})
}
