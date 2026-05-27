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
	"github.com/japharyroman/fuelgrid-os/internal/events"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// grantRoleRequest is the JSON body for POST /api/v1/admin/users/{userID}/roles.
type grantRoleRequest struct {
	RoleCode string `json:"role_code"`
}

// handleGrantRole grants a system role to a user.
//
// This handler is the canonical example of the Stage-7 pattern: a single
// DB transaction wraps the business change (user_roles INSERT), the
// audit log row, and the outbox event so all three either commit
// together or roll back together.
func (s *Server) handleGrantRole(w http.ResponseWriter, r *http.Request) {
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

	var req grantRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.RoleCode == "" {
		writeError(w, http.StatusBadRequest, "role_code is required")
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		s.logger.Error("grant role: begin tx", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Look up the role. System roles only — custom tenant roles are
	// assigned via a different (future) endpoint.
	var roleID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT id FROM roles WHERE code = $1 AND is_system = true`,
		req.RoleCode,
	).Scan(&roleID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "unknown role_code")
		return
	}
	if err != nil {
		s.logger.Error("grant role: lookup role", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Confirm the target user exists in the actor's tenant. Returning
	// 404 here means a system_admin can't enumerate users across
	// tenants — the response is identical whether the user is missing
	// or belongs to someone else.
	var targetExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
		)
	`, targetID, actor.TenantID).Scan(&targetExists); err != nil {
		s.logger.Error("grant role: lookup target", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !targetExists {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	// Idempotent insert. ON CONFLICT means re-granting the same role is
	// a no-op for the user_roles row, but we still write the audit log
	// + outbox event — operators frequently want to see "the grant was
	// reaffirmed at T".
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id, tenant_id, granted_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, role_id) DO NOTHING
	`, targetID, roleID, actor.TenantID, actor.UserID); err != nil {
		s.logger.Error("grant role: insert user_roles", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payload, err := json.Marshal(map[string]any{
		"user_id":   targetID,
		"role_id":   roleID,
		"role_code": req.RoleCode,
	})
	if err != nil {
		s.logger.Error("grant role: marshal payload", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tenantID := actor.TenantID
	actorID := actor.UserID
	requestID := chimiddleware.GetReqID(ctx)

	if err := audit.Write(ctx, tx, audit.Entry{
		TenantID:   &tenantID,
		ActorID:    &actorID,
		Action:     "user.role.granted",
		EntityType: "user_role",
		EntityID:   targetID.String(),
		NewValue:   payload,
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
		RequestID:  requestID,
	}); err != nil {
		s.logger.Error("grant role: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := events.WriteOutbox(ctx, tx, events.Event{
		TenantID:      &tenantID,
		Type:          "UserRoleGranted",
		AggregateType: "user",
		AggregateID:   targetID.String(),
		ActorID:       &actorID,
		Payload:       payload,
		CorrelationID: requestID,
	}); err != nil {
		s.logger.Error("grant role: outbox", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		s.logger.Error("grant role: commit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"user_id":   targetID,
		"role_id":   roleID,
		"role_code": req.RoleCode,
	})
}
