package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/reporting"
)

// Deterministic report insights (REPORTING).
//
// GET /api/v1/reports/{reportKey}/insights?station_id=&period= returns the
// {insights, data_quality} annotations for a signature report. The figures are
// re-read from the SAME services the dashboards/CSV exports use; the internal
// reporting package's pure functions then derive the deterministic insights.
// Nothing here recomputes money — it only annotates already-computed figures.
//
// reportKey ∈ daily-close | stock-reconciliation | sales-summary |
// cash-reconciliation | customer-aging. The station-scoped reports require a
// ?station_id and are gated by that station's read permission; the tenant-wide
// customer-aging report is gated by customer.read.

// insightsResponse is the wire shape (reporting.Report with non-null slices).
type insightsResponse struct {
	Insights    []reporting.Insight            `json:"insights"`
	DataQuality []reporting.DataQualityWarning `json:"data_quality"`
}

func toInsightsResponse(rep reporting.Report) insightsResponse {
	out := insightsResponse{Insights: rep.Insights, DataQuality: rep.DataQuality}
	if out.Insights == nil {
		out.Insights = []reporting.Insight{}
	}
	if out.DataQuality == nil {
		out.DataQuality = []reporting.DataQualityWarning{}
	}
	return out
}

// requireStationParam parses a required ?station_id and loads the station,
// returning false (after writing the error) when missing/invalid/not found.
func (s *Server) requireStationParam(w http.ResponseWriter, r *http.Request, actor identity.Actor) (uuid.UUID, bool) {
	raw := r.URL.Query().Get("station_id")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "station_id is required")
		return uuid.Nil, false
	}
	stationID, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station_id")
		return uuid.Nil, false
	}
	if _, err := s.stations.Get(r.Context(), actor.TenantID, stationID); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "station not found")
		return uuid.Nil, false
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return uuid.Nil, false
	}
	return stationID, true
}

// grossSeriesFromRevenue builds an oldest→newest gross series from the recent
// revenue days (RecentDays returns newest-first), reading a chosen field.
func grossSeries(days []reportingRevenuePoint, pick func(reportingRevenuePoint) string) []reporting.PeriodPoint {
	pts := make([]reporting.PeriodPoint, 0, len(days))
	// Reverse to chronological order.
	for i := len(days) - 1; i >= 0; i-- {
		pts = append(pts, reporting.PeriodPoint{Label: days[i].date, Value: pick(days[i])})
	}
	return pts
}

// reportingRevenuePoint is a tiny projection of a revenue day for the series
// builders, kept local so the handler doesn't leak the revenue model shape.
type reportingRevenuePoint struct {
	date   string
	gross  string
	margin string
	cash   string
}

// loadRevenuePoints reads a station's recent revenue days as projection points.
func (s *Server) loadRevenuePoints(ctx context.Context, tenantID, stationID uuid.UUID) ([]reportingRevenuePoint, bool, error) {
	days, err := s.revenue.RecentDays(ctx, tenantID, stationID, 30)
	if err != nil {
		return nil, false, err
	}
	pts := make([]reportingRevenuePoint, 0, len(days))
	latestLocked := false
	for i := range days {
		d := days[i]
		pts = append(pts, reportingRevenuePoint{
			date: d.BusinessDate.Format(dateLayout), gross: d.GrossRevenue,
			margin: d.MarginTotal, cash: d.CashTotal,
		})
		if i == 0 {
			latestLocked = d.Status == "locked"
		}
	}
	return pts, latestLocked, nil
}

// handleReportInsights dispatches on {reportKey}. Auth + per-report permission
// gating is applied at the route; here we only re-read data and compose.
func (s *Server) handleReportInsights(reportKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, err := identity.Require(r.Context())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		ctx := r.Context()

		switch reportKey {
		case "customer-aging":
			rows, err := s.receivables.Aging(ctx, actor.TenantID)
			if err != nil {
				s.logger.Error("insights: ar aging", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			custs := make([]reporting.AgingCustomer, 0, len(rows))
			for i := range rows {
				custs = append(custs, reporting.AgingCustomer{Name: rows[i].Name, Balance: rows[i].Balance})
			}
			rep := reporting.CustomerAging(reporting.CustomerAgingInput{Customers: custs})
			writeJSON(w, http.StatusOK, toInsightsResponse(rep))
			return

		case "daily-close", "sales-summary", "cash-reconciliation":
			stationID, ok := s.requireStationParam(w, r, actor)
			if !ok {
				return
			}
			pts, latestLocked, err := s.loadRevenuePoints(ctx, actor.TenantID, stationID)
			if err != nil {
				s.logger.Error("insights: revenue points", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			grossPts := grossSeries(pts, func(p reportingRevenuePoint) string { return p.gross })

			switch reportKey {
			case "sales-summary":
				marginPts := grossSeries(pts, func(p reportingRevenuePoint) string { return p.margin })
				rep := reporting.SalesSummary(reporting.SalesInput{
					GrossSeries: grossPts, MarginSeries: marginPts, PeriodLocked: latestLocked,
				})
				writeJSON(w, http.StatusOK, toInsightsResponse(rep))
			case "cash-reconciliation":
				variance := s.latestCashVariance(ctx, actor.TenantID, stationID)
				cashPts := grossSeries(pts, func(p reportingRevenuePoint) string { return p.cash })
				rep := reporting.CashReconciliation(reporting.CashReconInput{
					Variance: variance, GrossSeries: cashPts, PeriodLocked: latestLocked,
				})
				writeJSON(w, http.StatusOK, toInsightsResponse(rep))
			default: // daily-close
				variance := s.latestCashVariance(ctx, actor.TenantID, stationID)
				unclosed := s.unclosedShiftCount(ctx, actor.TenantID, stationID)
				rep := reporting.DailyClose(reporting.DailyCloseInput{
					GrossSeries: grossPts, CashVariance: variance,
					UnclosedShiftCount: unclosed, DayLocked: latestLocked,
				})
				writeJSON(w, http.StatusOK, toInsightsResponse(rep))
			}
			return

		case "stock-reconciliation":
			stationID, ok := s.requireStationParam(w, r, actor)
			if !ok {
				return
			}
			rep := s.stockReconInsights(ctx, actor.TenantID, stationID)
			writeJSON(w, http.StatusOK, toInsightsResponse(rep))
			return

		default:
			writeError(w, http.StatusNotFound, "unknown report")
		}
	}
}

// latestCashVariance returns the most recent cash reconciliation's variance for
// the station (decimal string), or "" when none exists. The insight treats any
// non-zero variance as at least informational, so no tolerance is needed here.
func (s *Server) latestCashVariance(ctx context.Context, tenantID, stationID uuid.UUID) string {
	rows, err := s.banking.ListCashReconciliations(ctx, tenantID, stationID)
	if err != nil || len(rows) == 0 {
		return ""
	}
	return rows[0].Variance
}

// unclosedShiftCount returns the count of unapproved shifts for the station's
// latest active day, or 0 when there is no active day.
func (s *Server) unclosedShiftCount(ctx context.Context, tenantID, stationID uuid.UUID) int {
	day, err := s.operations.LatestActiveDayForStation(ctx, tenantID, stationID)
	if err != nil {
		return 0
	}
	n, err := s.operations.UnapprovedShiftCountForDay(ctx, tenantID, day.ID)
	if err != nil {
		return 0
	}
	return n
}

// stockReconInsights composes the Fuel Stock Reconciliation annotations from
// the station's latest active day reconciliations + dips.
func (s *Server) stockReconInsights(ctx context.Context, tenantID, stationID uuid.UUID) reporting.Report {
	var in reporting.StockReconInput
	in.AllShiftsClosed = true

	day, err := s.operations.LatestActiveDayForStation(ctx, tenantID, stationID)
	if err != nil {
		return reporting.StockReconciliation(in)
	}
	if n, err := s.operations.UnapprovedShiftCountForDay(ctx, tenantID, day.ID); err == nil {
		in.AllShiftsClosed = n == 0
	}
	recs, err := s.reconciliation.ListForStationDay(ctx, tenantID, stationID, day.ID)
	if err != nil {
		return reporting.StockReconciliation(in)
	}
	latest, _ := s.readings.LatestDipsForStation(ctx, tenantID, stationID)
	for i := range recs {
		rec := recs[i]
		_, hasDip := latest[rec.TankID]
		status := "within_tolerance"
		if rec.Status == "exception" {
			status = "over_tolerance"
		}
		in.Tanks = append(in.Tanks, reporting.TankRecon{
			TankLabel:        rec.TankID.String()[:8],
			VariancePercent:  rec.VariancePercent,
			TolerancePercent: rec.TolerancePercent,
			Status:           status,
			HasPhysicalDip:   hasDip,
		})
	}
	return reporting.StockReconciliation(in)
}
