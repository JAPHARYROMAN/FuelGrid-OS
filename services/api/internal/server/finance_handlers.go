package server

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/accounting"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// parseDateParam reads a YYYY-MM-DD query param, falling back to def.
func parseDateParam(r *http.Request, key string, def time.Time) time.Time {
	if v := r.URL.Query().Get(key); v != "" {
		if t, err := time.Parse(dateLayout, v); err == nil {
			return t
		}
	}
	return def
}

func (s *Server) handleTrialBalance(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	asOf := parseDateParam(r, "as_of", time.Now())
	rows, err := s.accounting.TrialBalance(r.Context(), actor.TenantID, asOf)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	var totalDebit, totalCredit float64
	for i := range rows {
		t := rows[i]
		if d, ok := parseDecimal(t.Debit); ok {
			totalDebit += d
		}
		if c, ok := parseDecimal(t.Credit); ok {
			totalCredit += c
		}
		out = append(out, map[string]any{
			"account_id": t.AccountID, "code": t.Code, "name": t.Name, "type": t.Type,
			"normal_balance": t.NormalBalance, "debit": t.Debit, "credit": t.Credit, "balance": t.Balance,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"as_of": asOf.Format(dateLayout), "rows": out,
		"balanced": (totalDebit-totalCredit) < 0.005 && (totalCredit-totalDebit) < 0.005,
	})
}

func (s *Server) handleIncomeStatement(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	from := parseDateParam(r, "from", time.Now().AddDate(0, -1, 0))
	to := parseDateParam(r, "to", time.Now())
	is, err := s.accounting.IncomeStatement(r.Context(), actor.TenantID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from.Format(dateLayout), "to": to.Format(dateLayout),
		"revenue": is.Revenue, "expenses": is.Expenses, "net_profit": is.NetProfit,
	})
}

func (s *Server) handleBalanceSheet(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	asOf := parseDateParam(r, "as_of", time.Now())
	bs, err := s.accounting.BalanceSheet(r.Context(), actor.TenantID, asOf)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"as_of":  asOf.Format(dateLayout),
		"assets": bs.Assets, "liabilities": bs.Liabilities, "equity": bs.Equity,
	})
}

func (s *Server) handleGeneralLedger(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	accountID, err := uuid.Parse(r.URL.Query().Get("account_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "account_id query param is required")
		return
	}
	rows, err := s.accounting.GeneralLedger(r.Context(), actor.TenantID, accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		g := rows[i]
		out = append(out, map[string]any{
			"entry_id": g.EntryID, "entry_number": g.EntryNumber, "entry_date": g.EntryDate.Format(dateLayout),
			"source_type": g.SourceType, "memo": g.Memo, "debit": g.Debit, "credit": g.Credit,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

// handleFinanceOverview is the one-call /finance dashboard (Phase 7, Stage 15):
// balance sheet, P&L, AP aging, open periods, and recent journal entries.
func (s *Server) handleFinanceOverview(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx := r.Context()
	now := time.Now()

	bs, err := s.accounting.BalanceSheet(ctx, actor.TenantID, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	is, err := s.accounting.IncomeStatement(ctx, actor.TenantID, now.AddDate(0, -1, 0), now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	apAging, err := s.payables.Aging(ctx, actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	periods, err := s.accounting.ListPeriods(ctx, actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	openPeriods := 0
	for i := range periods {
		if periods[i].Status == accounting.PeriodOpen || periods[i].Status == accounting.PeriodClosing {
			openPeriods++
		}
	}
	entries, err := s.accounting.ListEntries(ctx, actor.TenantID, 10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	recent := make([]journalEntryDTO, 0, len(entries))
	for i := range entries {
		recent = append(recent, toJournalEntryDTO(&entries[i]))
	}
	apOpen := 0
	for range apAging {
		apOpen++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"balance_sheet":     map[string]any{"assets": bs.Assets, "liabilities": bs.Liabilities, "equity": bs.Equity},
		"income_statement":  map[string]any{"revenue": is.Revenue, "expenses": is.Expenses, "net_profit": is.NetProfit},
		"ap_supplier_count": apOpen,
		"open_periods":      openPeriods,
		"recent_entries":    recent,
	})
}
