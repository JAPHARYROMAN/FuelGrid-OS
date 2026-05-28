package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/events"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// requirePlatformAdmin gates the platform routes on a static bearer token
// (PLATFORM_ADMIN_TOKEN), checked in constant time. This is an
// operator/IaC credential, not a user session — platform provisioning
// happens before any tenant or user exists.
//
// When the token is unconfigured the route 404s, so the endpoint's
// existence isn't advertised on deployments that don't use it.
func (s *Server) requirePlatformAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := s.cfg.PlatformAdminToken
		if want == "" {
			http.NotFound(w, r)
			return
		}
		got := extractBearer(r)
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid platform admin token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type createTenantRequest struct {
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	AdminEmail    string `json:"admin_email"`
	AdminFullName string `json:"admin_full_name"`
}

type createTenantResponse struct {
	TenantID           uuid.UUID `json:"tenant_id"`
	TenantSlug         string    `json:"tenant_slug"`
	AdminUserID        uuid.UUID `json:"admin_user_id"`
	AdminEmail         string    `json:"admin_email"`
	PasswordResetToken string    `json:"password_reset_token"`
}

// handleCreateTenant provisions a new tenant plus its first admin user.
//
// In one transaction it inserts the tenant, an invited admin user (no
// password), and a system_admin role grant, with a platform-scoped audit
// + outbox row (actor is NULL — there is no logged-in principal). After
// commit it mints a password-reset token so the admin can set their
// password through the standard reset flow, and returns it in the
// response (production wires this to an invite email instead).
func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	if s.deps.DB == nil || s.identity == nil {
		writeError(w, http.StatusServiceUnavailable, "provisioning unavailable")
		return
	}

	var req createTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" || req.Slug == "" || req.AdminEmail == "" || req.AdminFullName == "" {
		writeError(w, http.StatusBadRequest, "name, slug, admin_email, and admin_full_name are required")
		return
	}
	if !slugPattern.MatchString(req.Slug) {
		writeError(w, http.StatusBadRequest, "slug must be lowercase letters, digits, and dashes")
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var tenantID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO tenants (name, slug) VALUES ($1, $2) RETURNING id
	`, req.Name, req.Slug).Scan(&tenantID)
	if err != nil {
		// The slug unique index rejects duplicates; surface a clean 409.
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a tenant with that slug already exists")
			return
		}
		s.logger.Error("create tenant", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var adminUserID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO users (tenant_id, email, full_name, status)
		VALUES ($1, $2, $3, 'invited')
		RETURNING id
	`, tenantID, req.AdminEmail, req.AdminFullName).Scan(&adminUserID)
	if err != nil {
		s.logger.Error("create tenant admin", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var roleID uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT id FROM roles WHERE code = 'system_admin' AND is_system = true`,
	).Scan(&roleID); err != nil {
		s.logger.Error("resolve system_admin role", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id, tenant_id) VALUES ($1, $2, $3)
	`, adminUserID, roleID, tenantID); err != nil {
		s.logger.Error("grant system_admin", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Platform-scoped audit: tenant_id is the new tenant; actor is NULL
	// because no user performed this — an operator token did.
	payload, _ := json.Marshal(map[string]any{
		"tenant_id":  tenantID,
		"slug":       req.Slug,
		"admin_user": adminUserID,
	})
	if err := audit.Write(ctx, tx, audit.Entry{
		TenantID:   &tenantID,
		Action:     "platform.tenant_created",
		EntityType: "tenant",
		EntityID:   tenantID.String(),
		NewValue:   payload,
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
		RequestID:  chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("audit tenant create", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := events.WriteOutbox(ctx, tx, events.Event{
		TenantID:      &tenantID,
		Type:          "TenantCreated",
		AggregateType: "tenant",
		AggregateID:   tenantID.String(),
		Payload:       payload,
		CorrelationID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("outbox tenant create", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		s.logger.Error("commit tenant create", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Mint the password-setup token after commit so the admin can set
	// their password via the standard reset flow.
	resetToken, err := s.identity.IssueResetToken(ctx, adminUserID)
	if err != nil {
		// The tenant + admin exist; only the convenience token failed.
		// The admin can still use the forgot-password flow.
		s.logger.Error("issue reset token for new admin", "error", err)
	}

	writeJSON(w, http.StatusCreated, createTenantResponse{
		TenantID:           tenantID,
		TenantSlug:         req.Slug,
		AdminUserID:        adminUserID,
		AdminEmail:         req.AdminEmail,
		PasswordResetToken: resetToken,
	})
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
