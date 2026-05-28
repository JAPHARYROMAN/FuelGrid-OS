package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/inventory"
)

type stockMovementDTO struct {
	ID            uuid.UUID  `json:"id"`
	TenantID      uuid.UUID  `json:"tenant_id"`
	TankID        uuid.UUID  `json:"tank_id"`
	MovementType  string     `json:"movement_type"`
	SourceRefType *string    `json:"source_ref_type,omitempty"`
	SourceRefID   *uuid.UUID `json:"source_ref_id,omitempty"`
	Litres        float64    `json:"litres"`
	BalanceAfter  float64    `json:"balance_after"`
	Status        string     `json:"status"`
	SupersedesID  *uuid.UUID `json:"supersedes_id,omitempty"`
	RecordedBy    uuid.UUID  `json:"recorded_by"`
	RecordedAt    string     `json:"recorded_at"`
	Notes         *string    `json:"notes,omitempty"`
}

func toStockMovementDTO(m *inventory.Movement) stockMovementDTO {
	return stockMovementDTO{
		ID: m.ID, TenantID: m.TenantID, TankID: m.TankID,
		MovementType: m.MovementType, SourceRefType: m.SourceRefType, SourceRefID: m.SourceRefID,
		Litres: m.Litres, BalanceAfter: m.BalanceAfter, Status: m.Status,
		SupersedesID: m.SupersedesID, RecordedBy: m.RecordedBy,
		RecordedAt: m.RecordedAt.Format(time.RFC3339), Notes: m.Notes,
	}
}

// tankForInventoryRead loads the tank and enforces the station-scoped
// inventory.read permission against its station. Returns ok=false after
// writing the response.
func (s *Server) tankForInventoryRead(w http.ResponseWriter, r *http.Request, actor identity.Actor) (tankID uuid.UUID, ok bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid tank id")
		return uuid.Nil, false
	}
	tank, err := s.tanks.Get(r.Context(), actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "tank not found")
		return uuid.Nil, false
	}
	if err != nil {
		s.logger.Error("inventory: get tank", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return uuid.Nil, false
	}
	if !s.authorizeStation(w, r, actor, "inventory.read", tank.StationID) {
		return uuid.Nil, false
	}
	return tank.ID, true
}

func (s *Server) handleListTankLedger(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tankID, ok := s.tankForInventoryRead(w, r, actor)
	if !ok {
		return
	}
	rows, err := s.inventory.ListMovements(r.Context(), actor.TenantID, tankID)
	if err != nil {
		s.logger.Error("list tank ledger", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]stockMovementDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toStockMovementDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleGetTankBookBalance(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tankID, ok := s.tankForInventoryRead(w, r, actor)
	if !ok {
		return
	}
	bal, err := s.inventory.CurrentBalance(r.Context(), actor.TenantID, tankID)
	if err != nil {
		s.logger.Error("tank book balance", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tank_id": tankID, "book_balance": bal})
}

type setOpeningBalanceRequest struct {
	// FromDip seeds the opening from the tank's first dip reading; otherwise
	// Litres is used.
	FromDip bool     `json:"from_dip"`
	Litres  *float64 `json:"litres,omitempty"`
	Notes   *string  `json:"notes,omitempty"`
}

func (s *Server) handleSetTankOpeningBalance(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid tank id")
		return
	}
	var req setOpeningBalanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ctx := r.Context()
	tank, err := s.tanks.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "tank not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Manual stock writes reuse the station-scoped stock.adjust permission.
	if !s.authorizeStation(w, r, actor, "stock.adjust", tank.StationID) {
		return
	}

	// Resolve the opening litres: the tank's first dip, or an explicit value.
	var litres float64
	srcType := "opening"
	if req.FromDip {
		dip, err := s.readings.FirstDipForTank(ctx, actor.TenantID, tank.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnprocessableEntity, "tank has no dip reading to seed an opening balance")
			return
		}
		if err != nil {
			s.logger.Error("opening balance: first dip", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		litres = dip.VolumeLitres
		srcType = "opening"
	} else {
		if req.Litres == nil || *req.Litres < 0 {
			writeError(w, http.StatusBadRequest, "litres must be provided and non-negative, or set from_dip")
			return
		}
		litres = *req.Litres
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	m, err := s.inventory.SetOpeningBalance(ctx, tx, actor.TenantID, inventory.OpeningInput{
		TankID: tank.ID, Litres: litres, SourceRefType: &srcType,
		RecordedBy: actor.UserID, Notes: req.Notes,
	})
	if errors.Is(err, inventory.ErrOpeningExists) {
		writeError(w, http.StatusConflict, "opening balance already set for this tank")
		return
	}
	if err != nil {
		s.logger.Error("set opening balance", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "opening_balance.set", EventType: "OpeningBalanceSet",
		EntityType: "stock_movement", EntityID: m.ID.String(),
		NewValue: toStockMovementDTO(m),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("set opening balance: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toStockMovementDTO(m))
}
