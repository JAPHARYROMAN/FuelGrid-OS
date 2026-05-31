package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/pricing"
)

type priceChangeDTO struct {
	ID            uuid.UUID `json:"id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	StationID     uuid.UUID `json:"station_id"`
	ProductID     uuid.UUID `json:"product_id"`
	UnitPrice     string    `json:"unit_price"`
	EffectiveFrom string    `json:"effective_from"`
	PreviousPrice *string   `json:"previous_price,omitempty"`
	Reason        *string   `json:"reason,omitempty"`
	SetBy         uuid.UUID `json:"set_by"`
	CreatedAt     string    `json:"created_at"`
}

func toPriceChangeDTO(p *pricing.PriceChange) priceChangeDTO {
	return priceChangeDTO{
		ID: p.ID, TenantID: p.TenantID, StationID: p.StationID, ProductID: p.ProductID,
		UnitPrice: p.UnitPrice, EffectiveFrom: p.EffectiveFrom.Format(time.RFC3339),
		PreviousPrice: p.PreviousPrice, Reason: p.Reason, SetBy: p.SetBy,
		CreatedAt: p.CreatedAt.Format(time.RFC3339),
	}
}

type priceBoardEntryDTO struct {
	ProductID         uuid.UUID `json:"product_id"`
	ProductCode       string    `json:"product_code"`
	ProductName       string    `json:"product_name"`
	ProductColor      string    `json:"product_color"`
	ActivePrice       *string   `json:"active_price,omitempty"`
	NextPrice         *string   `json:"next_price,omitempty"`
	NextEffectiveFrom *string   `json:"next_effective_from,omitempty"`
}

type setPriceRequest struct {
	ProductID      uuid.UUID `json:"product_id"`
	UnitPrice      string    `json:"unit_price"`
	EffectiveFrom  *string   `json:"effective_from,omitempty"`
	Reason         *string   `json:"reason,omitempty"`
	AllowBelowCost bool      `json:"allow_below_cost"`
}

func (s *Server) handleSetPrice(w http.ResponseWriter, r *http.Request) {
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
	var req setPriceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.ProductID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "product_id is required")
		return
	}
	price, ok := parseDecimal(req.UnitPrice)
	if !ok || price < 0 {
		writeError(w, http.StatusBadRequest, "unit_price must be a non-negative decimal")
		return
	}
	var effFrom *time.Time
	if req.EffectiveFrom != nil && *req.EffectiveFrom != "" {
		t, err := time.Parse(time.RFC3339, *req.EffectiveFrom)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid effective_from (want RFC3339)")
			return
		}
		effFrom = &t
	}

	ctx := r.Context()
	if _, err := s.products.Get(ctx, actor.TenantID, req.ProductID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "product not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Below-cost guard: refuse a selling price under the product's cumulative
	// weighted-average landed cost at this station (lifetime average over posted
	// deliveries, not a perpetual moving average; see docs/costing-policy.md)
	// unless explicitly overridden.
	if !req.AllowBelowCost {
		cost, found, err := s.inventory.AverageLandedCostForStationProduct(ctx, actor.TenantID, stationID, req.ProductID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if found {
			if c, ok := parseDecimal(cost); ok && price < c {
				writeError(w, http.StatusUnprocessableEntity,
					"selling price "+req.UnitPrice+" is below landed cost "+cost+"; set allow_below_cost to override")
				return
			}
		}
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	pc, err := s.pricing.SetPrice(ctx, tx, actor.TenantID, pricing.SetPriceInput{
		StationID: stationID, ProductID: req.ProductID, UnitPrice: req.UnitPrice,
		EffectiveFrom: effFrom, Reason: req.Reason, SetBy: actor.UserID,
	})
	if isForeignKeyViolation(err) {
		writeError(w, http.StatusBadRequest, "product is not at this station's tenant")
		return
	}
	if err != nil {
		s.logger.Error("set price", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	action := "price.changed"
	event := "PriceChanged"
	if effFrom != nil && effFrom.After(time.Now()) {
		action, event = "price.scheduled", "PriceScheduled"
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: action, EventType: event,
		EntityType: "price_change", EntityID: pc.ID.String(),
		NewValue: toPriceChangeDTO(pc),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toPriceChangeDTO(pc))
}

func (s *Server) handlePriceBoard(w http.ResponseWriter, r *http.Request) {
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
	rows, err := s.pricing.PriceBoard(r.Context(), actor.TenantID, stationID)
	if err != nil {
		s.logger.Error("price board", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]priceBoardEntryDTO, 0, len(rows))
	for i := range rows {
		e := rows[i]
		dto := priceBoardEntryDTO{
			ProductID: e.ProductID, ProductCode: e.ProductCode, ProductName: e.ProductName,
			ProductColor: e.ProductColor, ActivePrice: e.ActivePrice, NextPrice: e.NextPrice,
		}
		if e.NextEffectiveFrom != nil {
			s := e.NextEffectiveFrom.Format(time.RFC3339)
			dto.NextEffectiveFrom = &s
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handlePriceHistory(w http.ResponseWriter, r *http.Request) {
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
	productID, err := uuid.Parse(r.URL.Query().Get("product_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "product_id query param is required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.pricing.HistoryPage(r.Context(), actor.TenantID, stationID, productID, limit+1, offset)
	if err != nil {
		s.logger.Error("price history", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]priceChangeDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toPriceChangeDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

// parseDecimal parses a money/price decimal string for validation and soft
// comparisons only — stored values keep their exact decimal form in the DB.
func parseDecimal(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
