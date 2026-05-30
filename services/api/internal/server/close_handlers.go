package server

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// periodCloseChecks are the close-checklist queries — open items that should be
// cleared before a period is closed or locked. `blocker` marks the ones that
// hard-block the close (ACCT-004); the others (open payables / issued invoices)
// are advisory because they legitimately age across periods.
var periodCloseChecks = []struct {
	key     string
	sql     string
	blocker bool
}{
	{"unposted_cash_reconciliations", `SELECT count(*) FROM cash_reconciliations WHERE tenant_id = $1 AND status <> 'posted'`, true},
	{"open_deposits", `SELECT count(*) FROM bank_deposits WHERE tenant_id = $1 AND status NOT IN ('posted', 'voided')`, true},
	{"unmatched_bank_lines", `SELECT count(*) FROM bank_statement_lines WHERE tenant_id = $1 AND status = 'unmatched'`, true},
	{"open_payables", `SELECT count(*) FROM payables WHERE tenant_id = $1 AND status IN ('open', 'partially_paid')`, false},
	{"expenses_awaiting_posting", `SELECT count(*) FROM expenses WHERE tenant_id = $1 AND status IN ('draft', 'submitted', 'approved')`, true},
	{"unissued_customer_invoices", `SELECT count(*) FROM customer_invoices WHERE tenant_id = $1 AND status = 'draft'`, true},
	{"open_customer_invoices", `SELECT count(*) FROM customer_invoices WHERE tenant_id = $1 AND status IN ('issued', 'partially_paid')`, false},
}

// periodCloseChecklist runs the checks against q (a pool or a tx) and returns
// the per-check counts plus the number of hard blockers. Used both by the
// read-only checklist endpoint and by the close/lock gate (ACCT-004).
func (s *Server) periodCloseChecklist(ctx context.Context, q database.Querier, tenantID uuid.UUID) (map[string]int, int, error) {
	counts := map[string]int{}
	blockers := 0
	for _, c := range periodCloseChecks {
		var n int
		if err := q.QueryRow(ctx, c.sql, tenantID).Scan(&n); err != nil {
			return nil, 0, err
		}
		counts[c.key] = n
		if c.blocker {
			blockers += n
		}
	}
	return counts, blockers, nil
}

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

	counts, blockers, err := s.periodCloseChecklist(ctx, s.deps.DB, actor.TenantID)
	if err != nil {
		s.logger.Error("close checklist", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
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
