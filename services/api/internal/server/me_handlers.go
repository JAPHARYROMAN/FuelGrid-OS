package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

type sessionDTO struct {
	ID        uuid.UUID `json:"id"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
	UserAgent string    `json:"user_agent,omitempty"`
	IsCurrent bool      `json:"is_current"`
}

// handleListMySessions returns the actor's active sessions. Used by the
// profile UI to show a "logged in here" table with revoke buttons.
func (s *Server) handleListMySessions(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.sessionRepo.ListActiveForUserPage(r.Context(), actor.UserID, limit+1, offset)
	if err != nil {
		s.logger.Error("list my sessions", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	out := make([]sessionDTO, 0, len(rows))
	for _, sr := range rows {
		out = append(out, sessionDTO{
			ID: sr.ID, IssuedAt: sr.IssuedAt, ExpiresAt: sr.ExpiresAt,
			UserAgent: sr.UserAgent, IsCurrent: sr.ID == actor.SessionID,
		})
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

// handleRevokeMySession revokes a single session by id, scoped to the
// actor. The identity service deletes the live Redis entry alongside
// the durable revocation, so the session stops authenticating
// immediately — revoking the current device 401s its next request,
// which is the intended "log this device out" UX.
func (s *Server) handleRevokeMySession(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}

	if err := s.identity.RevokeSession(r.Context(), actor.UserID, id); err != nil {
		if errors.Is(err, identity.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		s.logger.Error("revoke my session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleChangeMyPassword is a placeholder for the profile change-password
// flow. The identity.Service.ChangePassword method exists from Stage 4;
// wiring the handler here keeps Stage-9 done-when achievable from the UI.
type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (s *Server) handleChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req changePasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "old_password and new_password are required")
		return
	}

	if err := s.identity.ChangePassword(r.Context(), actor.TenantID, actor.UserID, req.OldPassword, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, identity.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "current password is incorrect")
		case errors.Is(err, identity.ErrPasswordWeak):
			writeError(w, http.StatusBadRequest, "password does not meet minimum strength requirements")
		default:
			s.logger.Error("change password", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
