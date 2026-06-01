package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// registerMfaRoutes mounts the self-service MFA group under /api/v1/me/mfa.
// Every route is session-authenticated (the actor manages only their own
// second factor) and tenant-rate-limited, mirroring the rest of /me.
//
// One call line in registerRoutes wires this in.
func (s *Server) registerMfaRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Use(s.rateLimitPerTenant)
		r.Post("/me/mfa/enroll", s.handleMfaEnrollBegin)
		r.Post("/me/mfa/confirm", s.handleMfaConfirm)
		r.Post("/me/mfa/disable", s.handleMfaDisable)
		r.Post("/me/mfa/backup-codes", s.handleMfaBackupCodes)
	})
}

// handleMfaEnrollBegin starts enrollment: generates a fresh TOTP secret,
// stores it disabled (encrypted at rest), and returns the base32 secret +
// otpauth:// URI for the client to render as a manual key / QR code. The user
// proves possession via /me/mfa/confirm before MFA is enabled.
func (s *Server) handleMfaEnrollBegin(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	e, err := s.identity.EnrollMfa(r.Context(), actor.UserID, actor.TenantID)
	if err != nil {
		mapAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"secret":      e.Secret,
		"otpauth_url": e.OTPAuthURL,
	})
}

type mfaCodeRequest struct {
	Code string `json:"code"`
}

// handleMfaConfirm verifies the first TOTP code, enables MFA, and returns the
// one-time backup recovery codes (shown to the user exactly once).
func (s *Server) handleMfaConfirm(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req mfaCodeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	codes, err := s.identity.ConfirmEnroll(r.Context(), actor.UserID, actor.TenantID, req.Code)
	if err != nil {
		mapAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backup_codes": codes})
}

// handleMfaDisable turns MFA off. A current TOTP or backup code is required so
// a hijacked session can't strip the second factor.
func (s *Server) handleMfaDisable(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req mfaCodeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	if err := s.identity.DisableMfa(r.Context(), actor.UserID, actor.TenantID, req.Code); err != nil {
		if errors.Is(err, identity.ErrMfaNotEnabled) {
			writeError(w, http.StatusConflict, "MFA is not enabled")
			return
		}
		mapAuthError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMfaBackupCodes regenerates the user's backup recovery codes, replacing
// any existing set, and returns the fresh plaintext codes once.
func (s *Server) handleMfaBackupCodes(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	codes, err := s.identity.RegenerateBackupCodes(r.Context(), actor.UserID, actor.TenantID)
	if err != nil {
		if errors.Is(err, identity.ErrMfaNotEnabled) {
			writeError(w, http.StatusConflict, "MFA is not enabled")
			return
		}
		mapAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backup_codes": codes})
}
