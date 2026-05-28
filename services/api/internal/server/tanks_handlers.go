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
	"github.com/japharyroman/fuelgrid-os/internal/tanks"
)

const dateLayout = "2006-01-02"

type tankDTO struct {
	ID               uuid.UUID `json:"id"`
	TenantID         uuid.UUID `json:"tenant_id"`
	StationID        uuid.UUID `json:"station_id"`
	ProductID        uuid.UUID `json:"product_id"`
	Name             string    `json:"name"`
	Code             string    `json:"code"`
	CapacityLitres   float64   `json:"capacity_litres"`
	SafeMinLitres    float64   `json:"safe_min_litres"`
	SafeMaxLitres    float64   `json:"safe_max_litres"`
	DeadStockLitres  float64   `json:"dead_stock_litres"`
	HasWaterSensor   bool      `json:"has_water_sensor"`
	HasTempSensor    bool      `json:"has_temp_sensor"`
	Status           string    `json:"status"`
	InstallationDate *string   `json:"installation_date,omitempty"`
	DecommissionDate *string   `json:"decommission_date,omitempty"`
	// CurrentLitres is the latest dip-resolved volume. Populated only by the
	// station overview (from tank_dip_readings); nil elsewhere.
	CurrentLitres *float64 `json:"current_litres,omitempty"`
}

func fmtDate(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(dateLayout)
	return &s
}

func toTankDTO(t *tanks.Tank) tankDTO {
	return tankDTO{
		ID: t.ID, TenantID: t.TenantID, StationID: t.StationID, ProductID: t.ProductID,
		Name: t.Name, Code: t.Code,
		CapacityLitres: t.CapacityLitres, SafeMinLitres: t.SafeMinLitres,
		SafeMaxLitres: t.SafeMaxLitres, DeadStockLitres: t.DeadStockLitres,
		HasWaterSensor: t.HasWaterSensor, HasTempSensor: t.HasTempSensor,
		Status:           t.Status,
		InstallationDate: fmtDate(t.InstallationDate),
		DecommissionDate: fmtDate(t.DecommissionDate),
	}
}

// parseDate turns an optional "2006-01-02" string into a *time.Time. A nil
// or empty input yields (nil, nil); a malformed value is an error.
func parseDate(s *string) (*time.Time, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	t, err := time.Parse(dateLayout, *s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Server) handleListTanks(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	filter, ok := s.stationReadFilter(w, r, actor)
	if !ok {
		return
	}
	rows, err := s.tanks.List(r.Context(), actor.TenantID, filter)
	if err != nil {
		s.logger.Error("list tanks", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]tankDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toTankDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleGetTank(w http.ResponseWriter, r *http.Request) {
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
	t, err := s.tanks.Get(r.Context(), actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "tank not found")
		return
	}
	if err != nil {
		s.logger.Error("get tank", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "station.read", t.StationID) {
		return
	}
	writeJSON(w, http.StatusOK, toTankDTO(t))
}

type createTankRequest struct {
	StationID        uuid.UUID `json:"station_id"`
	ProductID        uuid.UUID `json:"product_id"`
	Name             string    `json:"name"`
	Code             string    `json:"code"`
	CapacityLitres   float64   `json:"capacity_litres"`
	SafeMinLitres    float64   `json:"safe_min_litres"`
	SafeMaxLitres    float64   `json:"safe_max_litres"`
	DeadStockLitres  float64   `json:"dead_stock_litres"`
	HasWaterSensor   bool      `json:"has_water_sensor"`
	HasTempSensor    bool      `json:"has_temp_sensor"`
	InstallationDate *string   `json:"installation_date,omitempty"`
}

func (s *Server) handleCreateTank(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req createTankRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.StationID == uuid.Nil || req.ProductID == uuid.Nil || req.Name == "" || req.Code == "" {
		writeError(w, http.StatusBadRequest, "station_id, product_id, name, and code are required")
		return
	}
	if req.CapacityLitres <= 0 {
		writeError(w, http.StatusBadRequest, "capacity_litres must be positive")
		return
	}
	if req.SafeMinLitres > req.SafeMaxLitres || req.SafeMaxLitres > req.CapacityLitres {
		writeError(w, http.StatusBadRequest, "require safe_min <= safe_max <= capacity")
		return
	}
	installDate, err := parseDate(req.InstallationDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid installation_date (want YYYY-MM-DD)")
		return
	}

	// Station-scoped authorization before any work.
	if !s.authorizeStation(w, r, actor, "tanks.manage", req.StationID) {
		return
	}

	ctx := r.Context()

	// Prove the parent station and product belong to the tenant. The
	// composite FKs are the backstop; these guards return clean 404s.
	if _, err := s.stations.Get(ctx, actor.TenantID, req.StationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "station not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if _, err := s.products.Get(ctx, actor.TenantID, req.ProductID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "product not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	t, err := s.tanks.Create(ctx, tx, actor.TenantID, tanks.CreateInput{
		StationID: req.StationID, ProductID: req.ProductID,
		Name: req.Name, Code: req.Code,
		CapacityLitres: req.CapacityLitres, SafeMinLitres: req.SafeMinLitres,
		SafeMaxLitres: req.SafeMaxLitres, DeadStockLitres: req.DeadStockLitres,
		HasWaterSensor: req.HasWaterSensor, HasTempSensor: req.HasTempSensor,
		InstallationDate: installDate,
	})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "a tank with that code already exists at this station")
		return
	}
	if err != nil {
		s.logger.Error("create tank", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "tank.created", EventType: "TankCreated",
		EntityType: "tank", EntityID: t.ID.String(),
		NewValue: toTankDTO(t),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("create tank: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toTankDTO(t))
}

type updateTankRequest struct {
	ProductID        *uuid.UUID `json:"product_id,omitempty"`
	Name             *string    `json:"name,omitempty"`
	Code             *string    `json:"code,omitempty"`
	CapacityLitres   *float64   `json:"capacity_litres,omitempty"`
	SafeMinLitres    *float64   `json:"safe_min_litres,omitempty"`
	SafeMaxLitres    *float64   `json:"safe_max_litres,omitempty"`
	DeadStockLitres  *float64   `json:"dead_stock_litres,omitempty"`
	HasWaterSensor   *bool      `json:"has_water_sensor,omitempty"`
	HasTempSensor    *bool      `json:"has_temp_sensor,omitempty"`
	InstallationDate *string    `json:"installation_date,omitempty"`
	DecommissionDate *string    `json:"decommission_date,omitempty"`
	// status is intentionally not editable here — lifecycle changes go
	// through PATCH /tanks/{id}/status so transition rules apply.
}

func (s *Server) handleUpdateTank(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req updateTankRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	installDate, err := parseDate(req.InstallationDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid installation_date (want YYYY-MM-DD)")
		return
	}
	decomDate, err := parseDate(req.DecommissionDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid decommission_date (want YYYY-MM-DD)")
		return
	}

	ctx := r.Context()

	before, err := s.tanks.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Authorize against the tank's own station.
	if !s.authorizeStation(w, r, actor, "tanks.manage", before.StationID) {
		return
	}

	// A product reassignment must stay within the tenant.
	if req.ProductID != nil {
		if _, err := s.products.Get(ctx, actor.TenantID, *req.ProductID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "product not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	after, err := s.tanks.Update(ctx, tx, actor.TenantID, id, tanks.UpdateInput{
		ProductID: req.ProductID, Name: req.Name, Code: req.Code,
		CapacityLitres: req.CapacityLitres, SafeMinLitres: req.SafeMinLitres,
		SafeMaxLitres: req.SafeMaxLitres, DeadStockLitres: req.DeadStockLitres,
		HasWaterSensor: req.HasWaterSensor, HasTempSensor: req.HasTempSensor,
		InstallationDate: installDate, DecommissionDate: decomDate,
	})
	if errors.Is(err, tanks.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "a tank with that code already exists at this station")
		return
	}
	if err != nil {
		s.logger.Error("update tank", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "tank.updated", EventType: "TankUpdated",
		EntityType: "tank", EntityID: after.ID.String(),
		PreviousValue: toTankDTO(before), NewValue: toTankDTO(after),
		IP: clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toTankDTO(after))
}

func (s *Server) handleDeleteTank(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	ctx := r.Context()

	before, err := s.tanks.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !s.authorizeStation(w, r, actor, "tanks.manage", before.StationID) {
		return
	}

	// Don't orphan nozzles: a tank still feeding live nozzles can't be
	// deleted. (Calibration charts are pure history and don't block.)
	if n, err := s.nozzles.CountActiveForTank(ctx, actor.TenantID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	} else if n > 0 {
		writeError(w, http.StatusConflict, "tank feeds live nozzles; remove them first")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.tanks.SoftDelete(ctx, tx, actor.TenantID, id); err != nil {
		if errors.Is(err, tanks.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "tank.deleted", EventType: "TankDeleted",
		EntityType: "tank", EntityID: id.String(),
		PreviousValue: toTankDTO(before),
		IP:            clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateTankStatus(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req statusChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ctx := r.Context()
	before, err := s.tanks.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !s.authorizeStation(w, r, actor, "tanks.manage", before.StationID) {
		return
	}

	if code, msg := checkLifecycleTransition(before.Status, req.Status, req.Reason); code != 0 {
		writeError(w, code, msg)
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	after, err := s.tanks.Update(ctx, tx, actor.TenantID, id, tanks.UpdateInput{Status: &req.Status})
	if errors.Is(err, tanks.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.logger.Error("update tank status", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "tank.status_changed", EventType: "TankStatusChanged",
		EntityType: "tank", EntityID: after.ID.String(),
		PreviousValue: toTankDTO(before), NewValue: toTankDTO(after),
		Reason: req.Reason,
		IP:     clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toTankDTO(after))
}
