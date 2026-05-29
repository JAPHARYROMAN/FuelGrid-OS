package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/session"
)

// requireAuth resolves the bearer token in the Authorization header against
// the session store and injects the actor onto the request context.
// Requests without an Authorization header — or with one that doesn't
// resolve to an active session — get 401 with a structured error body.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearer(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
			return
		}

		sess, err := s.identity.Resolve(r.Context(), token)
		if err != nil {
			switch {
			case errors.Is(err, identity.ErrSessionNotFound):
				writeError(w, http.StatusUnauthorized, "session not found")
			case errors.Is(err, identity.ErrSessionExpired):
				writeError(w, http.StatusUnauthorized, "session expired")
			case errors.Is(err, identity.ErrSessionRevoked):
				writeError(w, http.StatusUnauthorized, "session revoked")
			default:
				s.logger.Error("session resolve", "error", err)
				writeError(w, http.StatusInternalServerError, "auth error")
			}
			return
		}

		actor := identity.Actor{
			UserID:       sess.UserID,
			TenantID:     sess.TenantID,
			SessionID:    sess.ID,
			MfaSatisfied: sess.MfaSatisfied,
		}
		ctx := identity.WithActor(r.Context(), actor)

		// When RLS is enforced (DATABASE_APP_URL set), bind a tenant-scoped DB
		// connection to the request: every query through the pool then runs as
		// the non-owner role with app.current_tenant set, so Postgres isolates
		// the tenant. The connection is released (and the GUC reset) when the
		// handler returns. No-op when RLS is off (appDB == owner pool).
		if s.rlsEnabled && actor.TenantID != uuid.Nil {
			scopedCtx, release, derr := s.appDB.AcquireTenant(ctx, actor.TenantID)
			if derr != nil {
				s.logger.Error("rls: acquire tenant connection", "error", derr)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			defer release()
			ctx = scopedCtx
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractBearer returns the token portion of an "Authorization: Bearer <token>"
// header, or "" when the header is absent or formatted unexpectedly.
func extractBearer(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	// Tolerate "bearer " in any case as some clients lowercase it.
	if !strings.HasPrefix(strings.ToLower(h), strings.ToLower(prefix)) {
		return ""
	}
	tok := strings.TrimSpace(h[len(prefix):])
	if tok == "" {
		return ""
	}
	return tok

	// Note: we intentionally don't accept ?token=... query strings; tokens
	// don't belong in URLs (logged by intermediaries).
}

// session.Session is used in the closure above; keep an import line tidy.
var _ = session.Session{}
