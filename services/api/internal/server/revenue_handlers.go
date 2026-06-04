package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/revenue"
)

type saleDTO struct {
	ID             uuid.UUID `json:"id"`
	ShiftID        uuid.UUID `json:"shift_id"`
	StationID      uuid.UUID `json:"station_id"`
	OperatingDayID uuid.UUID `json:"operating_day_id"`
	NozzleID       uuid.UUID `json:"nozzle_id"`
	ProductID      uuid.UUID `json:"product_id"`
	TankID         uuid.UUID `json:"tank_id"`
	Litres         float64   `json:"litres"`
	UnitPrice      string    `json:"unit_price"`
	GrossAmount    string    `json:"gross_amount"`
	TaxRate        string    `json:"tax_rate"`
	TaxAmount      string    `json:"tax_amount"`
	NetAmount      string    `json:"net_amount"`
	UnitCost       *string   `json:"unit_cost,omitempty"`
	CogsAmount     *string   `json:"cogs_amount,omitempty"`
	MarginAmount   *string   `json:"margin_amount,omitempty"`
	RecordedAt     string    `json:"recorded_at"`
	// VoidStatus is the sale's current (non-rejected) void status —
	// "requested" while a void awaits approval, "approved" once it is reversed,
	// or omitted when the sale has no active void. It lets the list/reports
	// visibly distinguish a reversed sale (Feature 4.3).
	VoidStatus *string `json:"void_status,omitempty"`
}

func toSaleDTO(s *revenue.Sale) saleDTO {
	return saleDTO{
		ID: s.ID, ShiftID: s.ShiftID, StationID: s.StationID, OperatingDayID: s.OperatingDayID,
		NozzleID: s.NozzleID, ProductID: s.ProductID, TankID: s.TankID, Litres: s.Litres,
		UnitPrice: s.UnitPrice, GrossAmount: s.GrossAmount, TaxRate: s.TaxRate,
		TaxAmount: s.TaxAmount, NetAmount: s.NetAmount, UnitCost: s.UnitCost,
		CogsAmount: s.CogsAmount, MarginAmount: s.MarginAmount,
		RecordedAt: s.RecordedAt.Format(time.RFC3339),
	}
}

// recognizeShiftRevenue values an approved shift's metered litres into sale
// records inside the approval tx, auditing the recognition. Returns false
// (after writing the response) only on an internal error.
func (s *Server) recognizeShiftRevenue(w http.ResponseWriter, r *http.Request, actor identity.Actor, tx pgx.Tx, shiftID uuid.UUID) bool {
	ctx := r.Context()
	n, err := s.revenue.RecognizeShiftSales(ctx, tx, actor.TenantID, shiftID, actor.UserID)
	if err != nil {
		s.logger.Error("approve shift: recognize revenue", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	if n == 0 {
		return true
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "revenue.recognized", EventType: "RevenueRecognized",
		EntityType: "shift", EntityID: shiftID.String(),
		NewValue: map[string]any{"shift_id": shiftID, "sales_recognized": n},
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("approve shift: revenue audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	return true
}

// saleDTOs maps a slice of sales to their DTOs, attaching each sale's current
// (non-rejected) void status from voidStatuses when present.
func saleDTOs(rows []revenue.Sale, voidStatuses map[uuid.UUID]string) []saleDTO {
	out := make([]saleDTO, 0, len(rows))
	for i := range rows {
		dto := toSaleDTO(&rows[i])
		if st, ok := voidStatuses[rows[i].ID]; ok {
			s := st
			dto.VoidStatus = &s
		}
		out = append(out, dto)
	}
	return out
}

// saleVoidStatuses fetches the current void status for a page of sales,
// returning an empty map (and logging) on error so the list still renders.
func (s *Server) saleVoidStatuses(r *http.Request, tenantID uuid.UUID, rows []revenue.Sale) map[uuid.UUID]string {
	ids := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}
	statuses, err := s.revenue.VoidStatuses(r.Context(), s.deps.DB, tenantID, ids)
	if err != nil {
		s.logger.Error("sale void statuses", "error", err)
		return map[uuid.UUID]string{}
	}
	return statuses
}

func (s *Server) handleListShiftSales(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid shift id")
		return
	}
	ctx := r.Context()
	shift, err := s.operations.GetShift(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "shift not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "revenue.read", shift.StationID) {
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.revenue.ListForShiftPage(ctx, actor.TenantID, id, limit+1, offset)
	if err != nil {
		s.logger.Error("list shift sales", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := saleDTOs(rows, s.saleVoidStatuses(r, actor.TenantID, rows))
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

func (s *Server) handleListStationSales(w http.ResponseWriter, r *http.Request) {
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
	dayID, err := uuid.Parse(r.URL.Query().Get("operating_day_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "operating_day_id query param is required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.revenue.ListForStationDayPage(r.Context(), actor.TenantID, stationID, dayID, limit+1, offset)
	if err != nil {
		s.logger.Error("list station sales", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := saleDTOs(rows, s.saleVoidStatuses(r, actor.TenantID, rows))
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

type tankValuationDTO struct {
	TankID     uuid.UUID `json:"tank_id"`
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	ProductID  uuid.UUID `json:"product_id"`
	BookLitres float64   `json:"book_litres"`
	AvgCost    *string   `json:"avg_cost,omitempty"`
	StockValue *string   `json:"stock_value,omitempty"`
}

func (s *Server) handleInventoryValuation(w http.ResponseWriter, r *http.Request) {
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
	rows, err := s.revenue.InventoryValuation(r.Context(), actor.TenantID, stationID)
	if err != nil {
		s.logger.Error("inventory valuation", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]tankValuationDTO, 0, len(rows))
	for i := range rows {
		v := rows[i]
		out = append(out, tankValuationDTO{
			TankID: v.TankID, Code: v.Code, Name: v.Name, ProductID: v.ProductID,
			BookLitres: v.BookLitres, AvgCost: v.AvgCost, StockValue: v.StockValue,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}
