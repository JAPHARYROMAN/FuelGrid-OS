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
)

type userSummaryDTO struct {
	ID          uuid.UUID   `json:"id"`
	Email       string      `json:"email"`
	FullName    string      `json:"full_name"`
	Status      string      `json:"status"`
	MfaEnabled  bool        `json:"mfa_enabled"`
	LastLoginAt *time.Time  `json:"last_login_at,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	Roles       []string    `json:"roles"`
	StationIDs  []uuid.UUID `json:"station_ids"`
	TenantWide  bool        `json:"tenant_wide"`
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.userRepo.List(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("list users", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]userSummaryDTO, 0, len(rows))
	for _, u := range rows {
		roles, _ := s.userRepo.ListRoles(r.Context(), u.ID)
		stations, _ := s.userRepo.ListStationAccess(r.Context(), u.ID)
		out = append(out, userSummaryDTO{
			ID: u.ID, Email: u.Email, FullName: u.FullName, Status: u.Status,
			MfaEnabled: u.MfaEnabled, LastLoginAt: u.LastLoginAt, CreatedAt: u.CreatedAt,
			Roles: roles, StationIDs: stations, TenantWide: len(stations) == 0,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

type inviteUserRequest struct {
	Email    string `json:"email"`
	FullName string `json:"full_name"`
}

func (s *Server) handleInviteUser(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req inviteUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Email == "" || req.FullName == "" {
		writeError(w, http.StatusBadRequest, "email and full_name are required")
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	userID, err := s.userRepo.Invite(ctx, tx, actor.TenantID, req.Email, req.FullName)
	if err != nil {
		s.logger.Error("invite user", "error", err)
		writeError(w, http.StatusInternalServerError, "could not invite user")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "user.invited", EventType: "UserInvited",
		EntityType: "user", EntityID: userID.String(),
		NewValue: map[string]string{"email": req.Email, "full_name": req.FullName},
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
	writeJSON(w, http.StatusCreated, map[string]any{"id": userID, "email": req.Email, "full_name": req.FullName})
}

type updateUserStatusRequest struct {
	Status string `json:"status"`
}

func (s *Server) handleUpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	targetID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var req updateUserStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	switch req.Status {
	case "active", "suspended":
	default:
		writeError(w, http.StatusBadRequest, "status must be 'active' or 'suspended'")
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.userRepo.UpdateStatus(ctx, tx, actor.TenantID, targetID, req.Status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "user.status_changed", EventType: "UserStatusChanged",
		EntityType: "user", EntityID: targetID.String(),
		NewValue: map[string]string{"status": req.Status},
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
	writeJSON(w, http.StatusOK, map[string]any{"id": targetID, "status": req.Status})
}

// userInTenant reports whether the target user exists in the actor's
// tenant. Returns (false, nil) when the user is missing or belongs to
// another tenant — callers map that to 404 so existence in another
// tenant never leaks. Wraps the tenant-scoped FindByID.
func (s *Server) userInTenant(r *http.Request, tenantID, userID uuid.UUID) (bool, error) {
	_, err := s.userRepo.FindByID(r.Context(), tenantID, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Server) handleRevokeUserRole(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	targetID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	roleCode := chi.URLParam(r, "roleCode")
	if roleCode == "" {
		writeError(w, http.StatusBadRequest, "role code is required")
		return
	}

	ctx := r.Context()

	// Guard: the target user must be in the actor's tenant. Without this
	// the DELETE matched on (user_id, role_id) alone, letting an admin in
	// tenant A revoke a role grant in tenant B.
	if ok, err := s.userInTenant(r, actor.TenantID, targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	roleID, err := s.userRepo.RoleIDByCode(ctx, roleCode)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "role not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := s.userRepo.RevokeRole(ctx, tx, targetID, roleID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "user.role.revoked", EventType: "UserRoleRevoked",
		EntityType: "user_role", EntityID: targetID.String(),
		PreviousValue: map[string]string{"role_code": roleCode},
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

type grantStationAccessRequest struct {
	StationID uuid.UUID `json:"station_id"`
}

func (s *Server) handleGrantStationAccess(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	targetID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var req grantStationAccessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.StationID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "station_id is required")
		return
	}

	ctx := r.Context()

	// Both the target user and the station must live in the actor's
	// tenant. The composite FKs in migration 0008 are the backstop;
	// these guards return clean 404s and stop a cross-tenant grant from
	// being silently swallowed by ON CONFLICT DO NOTHING.
	if ok, err := s.userInTenant(r, actor.TenantID, targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if _, err := s.stations.Get(ctx, actor.TenantID, req.StationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "station not found")
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

	if err := s.userRepo.GrantStationAccess(ctx, tx, targetID, req.StationID, actor.TenantID, actor.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "user.station_access.granted", EventType: "UserStationAccessGranted",
		EntityType: "user_station_access", EntityID: targetID.String(),
		NewValue: map[string]uuid.UUID{"user_id": targetID, "station_id": req.StationID},
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
	writeJSON(w, http.StatusCreated, map[string]any{"user_id": targetID, "station_id": req.StationID})
}

func (s *Server) handleRevokeStationAccess(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	targetID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}

	ctx := r.Context()

	// Guard: the target user must be in the actor's tenant; otherwise the
	// DELETE matched on (user_id, station_id) alone, letting tenant A
	// strip a tenant-B user's access.
	if ok, err := s.userInTenant(r, actor.TenantID, targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.userRepo.RevokeStationAccess(ctx, tx, targetID, stationID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "user.station_access.revoked", EventType: "UserStationAccessRevoked",
		EntityType: "user_station_access", EntityID: targetID.String(),
		PreviousValue: map[string]uuid.UUID{"user_id": targetID, "station_id": stationID},
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
