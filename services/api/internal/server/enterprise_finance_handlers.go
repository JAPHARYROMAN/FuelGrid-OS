package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// handleConsolidatedFinance ties the tenant-wide Phase-7 P&L and balance sheet
// (posted journal lines) to a per-station revenue breakdown from the Phase-9
// projection — one consolidated view that reconciles to station reports
// (Stage 10).
func (s *Server) handleConsolidatedFinance(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx := r.Context()
	from := parseDateParam(r, "from", time.Now().AddDate(0, -1, 0))
	to := parseDateParam(r, "to", time.Now())
	asOf := parseDateParam(r, "as_of", time.Now())

	is, err := s.accounting.IncomeStatement(ctx, actor.TenantID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	bs, err := s.accounting.BalanceSheet(ctx, actor.TenantID, asOf)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	ranks, err := s.enterprise.StationRanking(ctx, actor.TenantID, nil, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	byStation := make([]map[string]any, 0, len(ranks))
	for i := range ranks {
		byStation = append(byStation, map[string]any{
			"station_id": ranks[i].StationID, "name": ranks[i].Name,
			"gross_revenue": ranks[i].GrossRevenue, "margin_total": ranks[i].MarginTotal,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from.Format(dateLayout), "to": to.Format(dateLayout), "as_of": asOf.Format(dateLayout),
		"income_statement": map[string]any{"revenue": is.Revenue, "expenses": is.Expenses, "net_profit": is.NetProfit},
		"balance_sheet":    map[string]any{"assets": bs.Assets, "liabilities": bs.Liabilities, "equity": bs.Equity},
		"by_station":       byStation,
	})
}

// handleStationKPIExport renders the per-station KPI report as CSV with a
// content checksum (Stage 11 — multi-station report builder, export form).
func (s *Server) handleStationKPIExport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	from := parseDateParam(r, "from", time.Now().AddDate(0, -1, 0))
	to := parseDateParam(r, "to", time.Now())
	ranks, err := s.enterprise.StationRanking(r.Context(), actor.TenantID, nil, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"station_id", "name", "gross_revenue", "margin_total"})
	for i := range ranks {
		_ = cw.Write([]string{ranks[i].StationID.String(), ranks[i].Name, ranks[i].GrossRevenue, ranks[i].MarginTotal})
	}
	cw.Flush()
	sum := sha256.Sum256(buf.Bytes())
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from.Format(dateLayout), "to": to.Format(dateLayout),
		"row_count": len(ranks), "checksum": hex.EncodeToString(sum[:]), "csv": buf.String(),
	})
}
