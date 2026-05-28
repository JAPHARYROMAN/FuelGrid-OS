package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/revenue"
)

type revenueDayDTO struct {
	ID               uuid.UUID  `json:"id"`
	StationID        uuid.UUID  `json:"station_id"`
	OperatingDayID   uuid.UUID  `json:"operating_day_id"`
	BusinessDate     string     `json:"business_date"`
	GrossRevenue     string     `json:"gross_revenue"`
	NetRevenue       string     `json:"net_revenue"`
	TaxTotal         string     `json:"tax_total"`
	CogsTotal        string     `json:"cogs_total"`
	MarginTotal      string     `json:"margin_total"`
	CashTotal        string     `json:"cash_total"`
	MobileMoneyTotal string     `json:"mobile_money_total"`
	CardTotal        string     `json:"card_total"`
	CreditTotal      string     `json:"credit_total"`
	VoucherTotal     string     `json:"voucher_total"`
	TenderTotal      string     `json:"tender_total"`
	CashVariance     string     `json:"cash_variance"`
	Status           string     `json:"status"`
	LockedBy         *uuid.UUID `json:"locked_by,omitempty"`
	LockedAt         *string    `json:"locked_at,omitempty"`
}

func toRevenueDayDTO(d *revenue.RevenueDay) revenueDayDTO {
	return revenueDayDTO{
		ID: d.ID, StationID: d.StationID, OperatingDayID: d.OperatingDayID,
		BusinessDate: d.BusinessDate.Format(dateLayout),
		GrossRevenue: d.GrossRevenue, NetRevenue: d.NetRevenue, TaxTotal: d.TaxTotal,
		CogsTotal: d.CogsTotal, MarginTotal: d.MarginTotal, CashTotal: d.CashTotal,
		MobileMoneyTotal: d.MobileMoneyTotal, CardTotal: d.CardTotal, CreditTotal: d.CreditTotal,
		VoucherTotal: d.VoucherTotal, TenderTotal: d.TenderTotal, CashVariance: d.CashVariance,
		Status: d.Status, LockedBy: d.LockedBy, LockedAt: fmtTime(d.LockedAt),
	}
}

type computeRevenueDayRequest struct {
	OperatingDayID uuid.UUID `json:"operating_day_id"`
}

func (s *Server) handleComputeRevenueDay(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}
	var req computeRevenueDayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OperatingDayID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "operating_day_id is required")
		return
	}
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	d, err := s.revenue.ComputeDay(ctx, tx, actor.TenantID, stationID, req.OperatingDayID)
	if errors.Is(err, revenue.ErrLocked) {
		writeError(w, http.StatusConflict, "revenue day is locked")
		return
	}
	if isForeignKeyViolation(err) {
		writeError(w, http.StatusBadRequest, "operating day not found for this station")
		return
	}
	if err != nil {
		s.logger.Error("compute revenue day", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "revenue_day.computed", EventType: "RevenueDayComputed",
		EntityType: "revenue_day", EntityID: d.ID.String(), NewValue: toRevenueDayDTO(d),
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toRevenueDayDTO(d))
}

func (s *Server) handleLockRevenueDay(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid revenue day id")
		return
	}
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	d, err := s.revenue.LockDay(ctx, tx, actor.TenantID, id, actor.UserID)
	if errors.Is(err, revenue.ErrNotFound) {
		writeError(w, http.StatusNotFound, "revenue day not found")
		return
	}
	if errors.Is(err, revenue.ErrLocked) {
		writeError(w, http.StatusConflict, "revenue day already locked")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "period.locked", EventType: "RevenueDayLocked",
		EntityType: "revenue_day", EntityID: d.ID.String(), NewValue: toRevenueDayDTO(d),
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toRevenueDayDTO(d))
}

// handleRevenueOverview is the one-call /revenue dashboard: the latest active
// day's live revenue + tender, plus the recent revenue-day trend.
func (s *Server) handleRevenueOverview(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}
	ctx := r.Context()
	station, err := s.stations.Get(ctx, actor.TenantID, stationID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := map[string]any{"station": toStationDTO(station)}

	day, err := s.operations.LatestActiveDayForStation(ctx, actor.TenantID, stationID)
	if err == nil {
		dd := toOperatingDayDTO(day)
		out["day"] = dd
		summary, err := s.revenue.DaySummary(ctx, s.deps.DB, actor.TenantID, stationID, day.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		tenders, err := s.revenue.DayTenders(ctx, actor.TenantID, stationID, day.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		out["summary"] = map[string]any{
			"gross_revenue": summary.GrossAmount, "net_revenue": summary.NetAmount,
			"tax_total": summary.TaxAmount, "cogs_total": summary.CogsAmount,
			"margin_total": summary.MarginAmount, "litres_sold": summary.LitresSold,
			"sale_count": summary.SaleCount,
		}
		out["tenders"] = map[string]any{
			"cash": tenders.Cash, "mobile_money": tenders.MobileMoney, "card": tenders.Card,
			"credit": tenders.Credit, "voucher": tenders.Voucher, "total": tenders.Total,
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	recent, err := s.revenue.RecentDays(ctx, actor.TenantID, stationID, 14)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	trend := make([]revenueDayDTO, 0, len(recent))
	for i := range recent {
		trend = append(trend, toRevenueDayDTO(&recent[i]))
	}
	out["recent_days"] = trend

	writeJSON(w, http.StatusOK, out)
}

type customerBalanceDTO struct {
	CustomerID uuid.UUID `json:"customer_id"`
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	Balance    string    `json:"balance"`
}

func (s *Server) handleARaging(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.receivables.Aging(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("ar aging", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]customerBalanceDTO, 0, len(rows))
	for i := range rows {
		out = append(out, customerBalanceDTO{
			CustomerID: rows[i].CustomerID, Code: rows[i].Code, Name: rows[i].Name, Balance: rows[i].Balance,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}
