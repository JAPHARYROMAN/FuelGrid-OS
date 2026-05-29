package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/inventory"
	"github.com/japharyroman/fuelgrid-os/internal/operations"
	"github.com/japharyroman/fuelgrid-os/internal/reconciliation"
	"github.com/japharyroman/fuelgrid-os/internal/tanks"
)

// reconciliationDTO carries every litre/percent figure as a decimal STRING —
// the exact numeric round-tripped from the DB, never a float64 — per the house
// money/litre rule. over_tolerance is the inverse of the SQL within-tolerance
// decision (an exact numeric comparison), not a float epsilon test.
type reconciliationDTO struct {
	ID               *uuid.UUID `json:"id,omitempty"`
	TankID           uuid.UUID  `json:"tank_id"`
	OperatingDayID   uuid.UUID  `json:"operating_day_id"`
	OpeningBook      string     `json:"opening_book"`
	DeliveriesTotal  string     `json:"deliveries_total"`
	SalesTotal       string     `json:"sales_total"`
	AdjustmentsTotal string     `json:"adjustments_total"`
	ClosingBook      string     `json:"closing_book"`
	ClosingPhysical  string     `json:"closing_physical"`
	VarianceLitres   string     `json:"variance_litres"`
	VariancePercent  string     `json:"variance_percent"`
	TolerancePercent string     `json:"tolerance_percent"`
	OverTolerance    bool       `json:"over_tolerance"`
	Status           string     `json:"status"`
	SealedBy         *uuid.UUID `json:"sealed_by,omitempty"`
	SealedAt         *string    `json:"sealed_at,omitempty"`
}

func toReconciliationDTO(rec *reconciliation.Reconciliation) reconciliationDTO {
	id := rec.ID
	return reconciliationDTO{
		ID: &id, TankID: rec.TankID, OperatingDayID: rec.OperatingDayID,
		OpeningBook: rec.OpeningBook, DeliveriesTotal: rec.DeliveriesTotal,
		SalesTotal: rec.SalesTotal, AdjustmentsTotal: rec.AdjustmentsTotal,
		ClosingBook: rec.ClosingBook, ClosingPhysical: rec.ClosingPhysical,
		VarianceLitres: rec.VarianceLitres, VariancePercent: rec.VariancePercent,
		TolerancePercent: rec.TolerancePercent,
		// A persisted 'exception' is exactly the over-tolerance state; this is
		// the SQL within-tolerance decision frozen at compute time, not a
		// recomputed float comparison.
		OverTolerance: rec.Status == reconciliation.StatusException,
		Status:        rec.Status, SealedBy: rec.SealedBy, SealedAt: fmtTime(rec.SealedAt),
	}
}

// computedReconciliation is the live result of comparing ledger book stock to
// the closing dip for a (tank, day), before persistence. Every figure is a
// decimal string computed in SQL numeric.
type computedReconciliation struct {
	OpeningBook      string
	DeliveriesTotal  string
	SalesTotal       string
	AdjustmentsTotal string
	ClosingBook      string
	ClosingPhysical  string
	VarianceLitres   string
	VariancePercent  string
	TolerancePercent string
	WriteOff         string // physical − book; the seal write-off litres
	WriteOffNonZero  bool   // exact numeric test (write-off <> 0) from SQL
	ThroughSeq       int64
	OverTolerance    bool
}

func (c *computedReconciliation) draftInput(tankID, dayID uuid.UUID, status string) reconciliation.DraftInput {
	return reconciliation.DraftInput{
		TankID: tankID, OperatingDayID: dayID,
		OpeningBook: c.OpeningBook, DeliveriesTotal: c.DeliveriesTotal,
		SalesTotal: c.SalesTotal, AdjustmentsTotal: c.AdjustmentsTotal,
		ClosingBook: c.ClosingBook, ClosingPhysical: c.ClosingPhysical,
		VarianceLitres: c.VarianceLitres, VariancePercent: c.VariancePercent,
		TolerancePercent: c.TolerancePercent, ThroughSeq: c.ThroughSeq, Status: status,
	}
}

func (c *computedReconciliation) status() string {
	if c.OverTolerance {
		return reconciliation.StatusException
	}
	return reconciliation.StatusDraft
}

func computedToDTO(c *computedReconciliation, tankID, dayID uuid.UUID) reconciliationDTO {
	return reconciliationDTO{
		TankID: tankID, OperatingDayID: dayID,
		OpeningBook: c.OpeningBook, DeliveriesTotal: c.DeliveriesTotal,
		SalesTotal: c.SalesTotal, AdjustmentsTotal: c.AdjustmentsTotal,
		ClosingBook: c.ClosingBook, ClosingPhysical: c.ClosingPhysical,
		VarianceLitres: c.VarianceLitres, VariancePercent: c.VariancePercent,
		TolerancePercent: c.TolerancePercent, OverTolerance: c.OverTolerance,
		Status: c.status(),
	}
}

// computeReconciliation gathers the live book-vs-physical figures for a
// (tank, day): book is summed forward from the last sealed reconciliation (or
// the genesis opening), physical comes from the day's closing dip, and the
// variance is classified against the product's loss tolerance. invQ reads the
// ledger — pass a tx to see movements posted earlier in the same transaction.
// Returns (computed, 0, "") on success, or (nil, httpCode, message) on a guard
// failure or error.
func (s *Server) computeReconciliation(ctx context.Context, invQ database.Querier, tenantID uuid.UUID, tank *tanks.Tank, dayID uuid.UUID) (*computedReconciliation, int, string) {
	// Guard: the day's shifts must all be approved (so all metered sales are
	// on the ledger) before book stock is meaningful.
	unapproved, err := s.operations.UnapprovedShiftCountForDay(ctx, tenantID, dayID)
	if err != nil {
		s.logger.Error("reconcile: unapproved count", "error", err)
		return nil, http.StatusInternalServerError, "internal error"
	}
	if unapproved > 0 {
		return nil, http.StatusConflict, "approve all the day's shifts before reconciling"
	}

	// Opening book + period watermark, sourced as EXACT decimal strings: the
	// last sealed reconciliation's closing_physical is the trust anchor; failing
	// that, the tank's genesis opening movement litres. Neither passes through
	// float64.
	var openingBook string
	var fromSeq int64
	prior, err := s.reconciliation.LastSealedForTank(ctx, tenantID, tank.ID)
	switch {
	case err == nil:
		openingBook, fromSeq = prior.ClosingPhysical, prior.ThroughSeq
	case errors.Is(err, reconciliation.ErrNotFound):
		litres, seq, ok, gerr := s.reconciliation.GenesisOpeningLitres(ctx, tenantID, tank.ID)
		if gerr != nil {
			s.logger.Error("reconcile: opening balance", "error", gerr)
			return nil, http.StatusInternalServerError, "internal error"
		}
		if ok {
			openingBook, fromSeq = litres, seq
		} else {
			openingBook, fromSeq = "0", 0
		}
	default:
		s.logger.Error("reconcile: last sealed", "error", err)
		return nil, http.StatusInternalServerError, "internal error"
	}

	// Physical figure: the day's closing dip volume, read as exact numeric text.
	physical, ok, derr := s.reconciliation.ClosingDipVolumeText(ctx, invQ, tenantID, tank.ID, dayID)
	if derr != nil {
		s.logger.Error("reconcile: closing dip", "error", derr)
		return nil, http.StatusInternalServerError, "internal error"
	}
	if !ok {
		return nil, http.StatusUnprocessableEntity, "tank has no closing dip for this operating day"
	}

	tolerance, err := s.reconciliation.ProductTolerancePercentText(ctx, tenantID, tank.ProductID)
	if err != nil {
		s.logger.Error("reconcile: product", "error", err)
		return nil, http.StatusInternalServerError, "internal error"
	}

	// All arithmetic — closing_book, variance, variance_percent, tolerance band,
	// the within-tolerance decision, and the write-off — happens in SQL numeric.
	c, err := s.reconciliation.Compute(ctx, invQ, tenantID, reconciliation.ComputeInput{
		TankID: tank.ID, OpeningBook: openingBook, ClosingPhysical: physical,
		TolerancePercent: tolerance, FromSeq: fromSeq,
	})
	if err != nil {
		s.logger.Error("reconcile: compute", "error", err)
		return nil, http.StatusInternalServerError, "internal error"
	}
	return &computedReconciliation{
		OpeningBook: c.OpeningBook, DeliveriesTotal: c.DeliveriesTotal,
		SalesTotal: c.SalesTotal, AdjustmentsTotal: c.AdjustmentsTotal,
		ClosingBook: c.ClosingBook, ClosingPhysical: c.ClosingPhysical,
		VarianceLitres: c.VarianceLitres, VariancePercent: c.VariancePercent,
		TolerancePercent: c.TolerancePercent, WriteOff: c.WriteOff, WriteOffNonZero: c.WriteOffNonZero,
		ThroughSeq: c.ThroughSeq, OverTolerance: !c.WithinTolerance,
	}, 0, ""
}

// tankAndDayForReconcile loads the tank (URL :id) and the operating day
// (?operating_day_id), authorizes perm against the tank's station, and checks
// the day belongs to that station. Returns ok=false after writing the response.
func (s *Server) tankAndDayForReconcile(w http.ResponseWriter, r *http.Request, actor identity.Actor, perm string) (*tanks.Tank, *operations.OperatingDay, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid tank id")
		return nil, nil, false
	}
	dayID, err := uuid.Parse(r.URL.Query().Get("operating_day_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "operating_day_id query param is required")
		return nil, nil, false
	}
	ctx := r.Context()
	tank, err := s.tanks.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "tank not found")
		return nil, nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, nil, false
	}
	if !s.authorizeStation(w, r, actor, perm, tank.StationID) {
		return nil, nil, false
	}
	day, err := s.operations.GetDay(ctx, actor.TenantID, dayID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "operating day not found")
		return nil, nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, nil, false
	}
	if day.StationID != tank.StationID {
		writeError(w, http.StatusBadRequest, "operating day is at a different station than the tank")
		return nil, nil, false
	}
	return tank, day, true
}

func (s *Server) handleReconciliationPreview(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tank, day, ok := s.tankAndDayForReconcile(w, r, actor, "reconciliation.read")
	if !ok {
		return
	}
	computed, code, msg := s.computeReconciliation(r.Context(), s.deps.DB, actor.TenantID, tank, day.ID)
	if code != 0 {
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, computedToDTO(computed, tank.ID, day.ID))
}

func (s *Server) handleGetReconciliation(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tank, day, ok := s.tankAndDayForReconcile(w, r, actor, "reconciliation.read")
	if !ok {
		return
	}
	rec, err := s.reconciliation.GetForTankDay(r.Context(), actor.TenantID, tank.ID, day.ID)
	if errors.Is(err, reconciliation.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no reconciliation for this tank and day yet")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toReconciliationDTO(rec))
}

type persistReconciliationRequest struct {
	OperatingDayID uuid.UUID `json:"operating_day_id"`
}

func (s *Server) handlePersistReconciliation(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	// operating_day_id may come from the body or the query; normalize to query
	// so tankAndDayForReconcile can validate it uniformly.
	if r.URL.Query().Get("operating_day_id") == "" {
		var req persistReconciliationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.OperatingDayID != uuid.Nil {
			q := r.URL.Query()
			q.Set("operating_day_id", req.OperatingDayID.String())
			r.URL.RawQuery = q.Encode()
		}
	}
	tank, day, ok := s.tankAndDayForReconcile(w, r, actor, "reconciliation.manage")
	if !ok {
		return
	}

	ctx := r.Context()
	// A sealed reconciliation can't be re-run.
	existing, err := s.reconciliation.GetForTankDay(ctx, actor.TenantID, tank.ID, day.ID)
	if err != nil && !errors.Is(err, reconciliation.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if existing != nil && existing.Status == reconciliation.StatusSealed {
		writeError(w, http.StatusConflict, "reconciliation already sealed")
		return
	}

	computed, code, msg := s.computeReconciliation(ctx, s.deps.DB, actor.TenantID, tank, day.ID)
	if code != 0 {
		writeError(w, code, msg)
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rec, err := s.reconciliation.UpsertDraft(ctx, tx, actor.TenantID, computed.draftInput(tank.ID, day.ID, computed.status()))
	if errors.Is(err, reconciliation.ErrSealed) {
		writeError(w, http.StatusConflict, "reconciliation already sealed")
		return
	}
	if err != nil {
		s.logger.Error("persist reconciliation", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "reconciliation.computed", EventType: "ReconciliationComputed",
		EntityType: "tank_reconciliation", EntityID: rec.ID.String(),
		NewValue: toReconciliationDTO(rec),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Raise a stock_variance exception on transition into over-tolerance.
	raisedException := rec.Status == reconciliation.StatusException &&
		(existing == nil || existing.Status != reconciliation.StatusException)
	if raisedException {
		if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
			TenantID: actor.TenantID, ActorID: actor.UserID,
			Action: "stock_variance.raised", EventType: "StockVarianceRaised",
			EntityType: "tank_reconciliation", EntityID: rec.ID.String(),
			NewValue: toReconciliationDTO(rec),
			IP:       clientIP(r), UserAgent: r.UserAgent(),
			RequestID: chimiddleware.GetReqID(ctx),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	status := http.StatusOK
	if existing == nil {
		status = http.StatusCreated
	}
	writeJSON(w, status, toReconciliationDTO(rec))
}

// reconciliationForManage loads a reconciliation (URL :id) and its tank, and
// authorizes reconciliation.manage against the tank's station.
func (s *Server) reconciliationForManage(w http.ResponseWriter, r *http.Request, actor identity.Actor) (*reconciliation.Reconciliation, *tanks.Tank, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reconciliation id")
		return nil, nil, false
	}
	ctx := r.Context()
	rec, err := s.reconciliation.Get(ctx, actor.TenantID, id)
	if errors.Is(err, reconciliation.ErrNotFound) {
		writeError(w, http.StatusNotFound, "reconciliation not found")
		return nil, nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, nil, false
	}
	tank, err := s.tanks.Get(ctx, actor.TenantID, rec.TankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, nil, false
	}
	if !s.authorizeStation(w, r, actor, "reconciliation.manage", tank.StationID) {
		return nil, nil, false
	}
	return rec, tank, true
}

type reconciliationAdjustmentRequest struct {
	Litres float64 `json:"litres"`
	Reason string  `json:"reason"`
}

func (s *Server) handleAdjustReconciliation(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req reconciliationAdjustmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Litres == 0 {
		writeError(w, http.StatusBadRequest, "litres must be non-zero (sign indicates gain/loss)")
		return
	}
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required for a stock adjustment")
		return
	}

	rec, tank, ok := s.reconciliationForManage(w, r, actor)
	if !ok {
		return
	}
	if rec.Status == reconciliation.StatusSealed {
		writeError(w, http.StatusConflict, "reconciliation already sealed")
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	srcType := "reconciliation"
	reason := req.Reason
	mv, err := s.inventory.PostMovement(ctx, tx, actor.TenantID, inventory.PostInput{
		TankID: rec.TankID, MovementType: inventory.TypeAdjustment,
		SourceRefType: &srcType, SourceRefID: &rec.ID, Litres: req.Litres,
		RecordedBy: actor.UserID, Notes: &reason,
	})
	if err != nil {
		s.logger.Error("reconcile adjustment: post movement", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "stock_movement.posted", EventType: "StockMovementPosted",
		EntityType: "stock_movement", EntityID: mv.ID.String(),
		NewValue: toStockMovementDTO(mv), Reason: req.Reason,
		IP: clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Recompute the draft within the tx so it reflects the adjustment.
	computed, code, msg := s.computeReconciliation(ctx, tx, actor.TenantID, tank, rec.OperatingDayID)
	if code != 0 {
		writeError(w, code, msg)
		return
	}
	updated, err := s.reconciliation.UpsertDraft(ctx, tx, actor.TenantID, computed.draftInput(tank.ID, rec.OperatingDayID, computed.status()))
	if errors.Is(err, reconciliation.ErrSealed) {
		writeError(w, http.StatusConflict, "reconciliation already sealed")
		return
	}
	if err != nil {
		s.logger.Error("reconcile adjustment: upsert", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "reconciliation.adjusted", EventType: "ReconciliationAdjusted",
		EntityType: "tank_reconciliation", EntityID: updated.ID.String(),
		PreviousValue: toReconciliationDTO(rec), NewValue: toReconciliationDTO(updated),
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
	writeJSON(w, http.StatusOK, toReconciliationDTO(updated))
}

func (s *Server) handleSealReconciliation(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rec, tank, ok := s.reconciliationForManage(w, r, actor)
	if !ok {
		return
	}
	if rec.Status == reconciliation.StatusSealed {
		writeError(w, http.StatusConflict, "reconciliation already sealed")
		return
	}

	ctx := r.Context()
	computed, code, msg := s.computeReconciliation(ctx, s.deps.DB, actor.TenantID, tank, rec.OperatingDayID)
	if code != 0 {
		writeError(w, code, msg)
		return
	}
	if computed.OverTolerance {
		writeError(w, http.StatusConflict, "variance is over tolerance; record adjustments before sealing")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Write off the residual variance so the ledger lands exactly on the
	// sealed physical figure — the balance-forward anchor for the next period.
	// The write-off (physical − book) was computed in SQL numeric and the
	// non-zero test is an exact numeric comparison, not a float epsilon: posting
	// it as a decimal string keeps the next day's opening book exact.
	if computed.WriteOffNonZero {
		reason := "reconciliation seal: variance write-off"
		wm, err := s.reconciliation.PostWriteOff(ctx, tx, actor.TenantID, rec.TankID, rec.ID, actor.UserID, computed.WriteOff, reason)
		if err != nil {
			s.logger.Error("seal: write-off", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
			TenantID: actor.TenantID, ActorID: actor.UserID,
			Action: "stock_movement.posted", EventType: "StockMovementPosted",
			EntityType: "stock_movement", EntityID: wm.ID.String(),
			NewValue: map[string]any{
				"id": wm.ID, "tank_id": wm.TankID, "movement_type": inventory.TypeAdjustment,
				"source_ref_type": "reconciliation", "source_ref_id": wm.SourceRefID,
				"litres": wm.Litres, "balance_after": wm.BalanceAfter,
				"recorded_by": wm.RecordedBy, "recorded_at": wm.RecordedAt.Format(time.RFC3339),
				"notes": wm.Notes,
			},
			Reason: reason,
			IP:     clientIP(r), UserAgent: r.UserAgent(),
			RequestID: chimiddleware.GetReqID(ctx),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	throughSeq, err := s.inventory.MaxSeqForTank(ctx, tx, actor.TenantID, rec.TankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	in := computed.draftInput(tank.ID, rec.OperatingDayID, reconciliation.StatusSealed)
	in.ThroughSeq = throughSeq
	sealed, err := s.reconciliation.Seal(ctx, tx, actor.TenantID, rec.ID, actor.UserID, in)
	if errors.Is(err, reconciliation.ErrSealed) {
		writeError(w, http.StatusConflict, "reconciliation already sealed")
		return
	}
	if err != nil {
		s.logger.Error("seal reconciliation", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "reconciliation.sealed", EventType: "ReconciliationSealed",
		EntityType: "tank_reconciliation", EntityID: sealed.ID.String(),
		PreviousValue: toReconciliationDTO(rec), NewValue: toReconciliationDTO(sealed),
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
	writeJSON(w, http.StatusOK, toReconciliationDTO(sealed))
}

func (s *Server) handleListStationReconciliations(w http.ResponseWriter, r *http.Request) {
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
	rows, err := s.reconciliation.ListForStationDay(r.Context(), actor.TenantID, stationID, dayID)
	if err != nil {
		s.logger.Error("list station reconciliations", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]reconciliationDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toReconciliationDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}
