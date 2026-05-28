package server

import (
	"net/http"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// handleCloseChecklist aggregates the open items that block a clean period
// close: unposted cash reconciliations, deposits in flight, unmatched bank
// lines, open payables, expenses awaiting posting, and unissued customer
// invoices. The console surfaces these before a finance user closes/locks a
// period.
func (s *Server) handleCloseChecklist(w http.ResponseWriter, r *http.Request) {
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
		{"unposted_cash_reconciliations", `SELECT count(*) FROM cash_reconciliations WHERE tenant_id = $1 AND status <> 'posted'`},
		{"open_deposits", `SELECT count(*) FROM bank_deposits WHERE tenant_id = $1 AND status NOT IN ('posted', 'voided')`},
		{"unmatched_bank_lines", `SELECT count(*) FROM bank_statement_lines WHERE tenant_id = $1 AND status = 'unmatched'`},
		{"open_payables", `SELECT count(*) FROM payables WHERE tenant_id = $1 AND status IN ('open', 'partially_paid')`},
		{"expenses_awaiting_posting", `SELECT count(*) FROM expenses WHERE tenant_id = $1 AND status IN ('draft', 'submitted', 'approved')`},
		{"unissued_customer_invoices", `SELECT count(*) FROM customer_invoices WHERE tenant_id = $1 AND status = 'draft'`},
		{"open_customer_invoices", `SELECT count(*) FROM customer_invoices WHERE tenant_id = $1 AND status IN ('issued', 'partially_paid')`},
	}
	blockers := 0
	for _, c := range checks {
		var n int
		if err := s.deps.DB.QueryRow(ctx, c.sql, actor.TenantID).Scan(&n); err != nil {
			s.logger.Error("close checklist", "error", err, "check", c.key)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		counts[c.key] = n
		// Unissued invoices and awaiting-posting expenses block the close;
		// open payables/invoices are informational (they age across periods).
		switch c.key {
		case "unposted_cash_reconciliations", "open_deposits", "unmatched_bank_lines",
			"expenses_awaiting_posting", "unissued_customer_invoices":
			blockers += n
		}
	}

	periods, err := s.accounting.ListPeriods(ctx, actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	periodOut := make([]map[string]any, 0, len(periods))
	for i := range periods {
		p := periods[i]
		periodOut = append(periodOut, map[string]any{
			"id": p.ID, "start_date": p.StartDate.Format(dateLayout), "end_date": p.EndDate.Format(dateLayout), "status": p.Status,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"checks":    counts,
		"blockers":  blockers,
		"can_close": blockers == 0,
		"periods":   periodOut,
	})
}
