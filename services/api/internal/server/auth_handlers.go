package server

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// trustedProxyDepth is the number of trusted reverse proxies in front of the
// API, set at server construction from API_TRUSTED_PROXY_DEPTH. clientIP reads
// it to decide whether (and how far into) X-Forwarded-For to trust. It's atomic
// so concurrent New() calls (e.g. multiple test servers) don't race the reads.
var trustedProxyDepth atomic.Int64

// loginRequest is the JSON body for POST /api/v1/auth/login.
type loginRequest struct {
	TenantSlug string `json:"tenant_slug"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	MfaCode    string `json:"mfa_code,omitempty"`
}

type loginResponse struct {
	Token       string    `json:"token,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	MfaRequired bool      `json:"mfa_required,omitempty"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.TenantSlug == "" || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "tenant_slug, email, and password are required")
		return
	}

	result, err := s.identity.Login(r.Context(), identity.LoginRequest{
		TenantSlug: req.TenantSlug,
		Email:      req.Email,
		Password:   req.Password,
		MfaCode:    req.MfaCode,
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
	})
	if err != nil {
		mapAuthError(w, err)
		return
	}
	if result.MfaRequired {
		writeJSON(w, http.StatusOK, loginResponse{MfaRequired: true})
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		Token:     result.Token,
		ExpiresAt: result.Session.ExpiresAt,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := extractBearer(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
		return
	}
	if err := s.identity.Logout(r.Context(), token); err != nil {
		s.logger.Error("logout", "error", err)
		writeError(w, http.StatusInternalServerError, "logout failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	token := extractBearer(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
		return
	}
	sess, err := s.identity.Refresh(r.Context(), token)
	if err != nil {
		mapAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"expires_at": sess.ExpiresAt,
	})
}

type passwordResetRequest struct {
	TenantSlug string `json:"tenant_slug"`
	Email      string `json:"email"`
}

func (s *Server) handlePasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	var req passwordResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.TenantSlug == "" || req.Email == "" {
		writeError(w, http.StatusBadRequest, "tenant_slug and email are required")
		return
	}

	token, delivered, err := s.identity.RequestPasswordReset(r.Context(), req.TenantSlug, req.Email)
	if err != nil {
		s.logger.Error("password reset request", "error", err)
		writeError(w, http.StatusInternalServerError, "could not process reset")
		return
	}
	if delivered {
		// Deliver the reset token by email (best-effort — never block the
		// request). The raw token is a bearer credential, so it goes only into
		// the email body and (in dev) a log line; never logged outside dev.
		s.sendPasswordResetEmail(r.Context(), req.Email, token)
		if s.cfg.Env == "development" {
			s.logger.Info("password reset token issued (dev only; deliver via email in prod)",
				"tenant", req.TenantSlug,
				"email", req.Email,
				"token", token,
			)
		} else {
			s.logger.Info("password reset token issued", "tenant", req.TenantSlug)
		}
	}

	// Always respond with 202 regardless of whether the email matched a
	// real user — prevents account enumeration.
	w.WriteHeader(http.StatusAccepted)
}

type passwordResetConfirmRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

func (s *Server) handlePasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	var req passwordResetConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Token == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "token and new_password are required")
		return
	}
	if err := s.identity.ConfirmPasswordReset(r.Context(), req.Token, req.NewPassword); err != nil {
		mapAuthError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMfaEnroll(w http.ResponseWriter, r *http.Request) {
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

type mfaVerifyRequest struct {
	Code string `json:"code"`
}

func (s *Server) handleMfaVerify(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req mfaVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	if err := s.identity.VerifyMfa(r.Context(), actor.UserID, actor.TenantID, req.Code); err != nil {
		mapAuthError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":       actor.UserID,
		"tenant_id":     actor.TenantID,
		"session_id":    actor.SessionID,
		"mfa_satisfied": actor.MfaSatisfied,
	})
}

// mapAuthError translates identity sentinel errors into uniform HTTP
// responses. Any error not in the lookup is treated as 500.
func mapAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrInvalidCredentials),
		errors.Is(err, identity.ErrMfaInvalid):
		writeError(w, http.StatusUnauthorized, "invalid credentials")
	case errors.Is(err, identity.ErrMfaRequired):
		writeError(w, http.StatusUnauthorized, "mfa code required")
	case errors.Is(err, identity.ErrUserLocked):
		writeError(w, http.StatusForbidden, "account is locked, try again later")
	case errors.Is(err, identity.ErrUserSuspended):
		writeError(w, http.StatusForbidden, "account is suspended")
	case errors.Is(err, identity.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "too many attempts")
	case errors.Is(err, identity.ErrPasswordWeak):
		writeError(w, http.StatusBadRequest, "password does not meet minimum strength requirements")
	case errors.Is(err, identity.ErrResetTokenInvalid):
		writeError(w, http.StatusBadRequest, "reset token invalid or expired")
	case errors.Is(err, identity.ErrMfaAlreadyEnabled):
		writeError(w, http.StatusConflict, "MFA is already enabled")
	case errors.Is(err, identity.ErrSessionNotFound),
		errors.Is(err, identity.ErrSessionExpired):
		writeError(w, http.StatusUnauthorized, "session invalid")
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{
		"error":  msg,
		"status": status,
	})
}

// clientIP returns the best-effort client IP for audit / rate-limit bucketing.
// X-Forwarded-For is honored only when trustedProxyDepth > 0 (set from
// API_TRUSTED_PROXY_DEPTH) — otherwise the header is client-spoofable and is
// ignored in favour of r.RemoteAddr (AUTH-09).
func clientIP(r *http.Request) string {
	if depth := int(trustedProxyDepth.Load()); depth > 0 {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			// Each trusted proxy appends the address that connected to it, so
			// the rightmost `depth` entries are proxy-attested. The real client
			// is the entry just left of them — index len-depth. Entries further
			// left are client-supplied and spoofable. Clamp to 0 if depth is
			// mis-set higher than the chain length.
			idx := len(parts) - depth
			if idx < 0 {
				idx = 0
			}
			if ip := strings.TrimSpace(parts[idx]); ip != "" {
				return ip
			}
		}
	}
	// SplitHostPort strips the port and unwraps the IPv6 brackets, so a
	// RemoteAddr of "[::1]:54321" yields "::1" rather than "[::1]" (which
	// is not valid INET syntax and would fail an audit insert).
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
