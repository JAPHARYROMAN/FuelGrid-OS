package server_test

// DB-backed integration tests for SR-M1: server-side MFA enforcement on the
// privileged admin-console surface.
//
// The requireMFASatisfied middleware refuses a request with 403 + a
// machine-readable "mfa_required" code when the actor's role mandates a second
// factor (identity.RoleRequiresMfa) but the session has not satisfied MFA. The
// MFA enrollment routes, /me and /auth stay reachable so a privileged-but-
// unenrolled user can still enroll (no chicken-and-egg lockout).
//
// Enforcement is config-gated (AUTH_ENFORCE_MFA_FOR_PRIVILEGED_ROLES, default
// true in production). The shared harness leaves it off so the multi-approver
// maker-checker suites are unaffected; this test opts it on explicitly.
//
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL like the rest of the suite.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"

	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

// seedPrivilegedUnenrolled creates an active user holding a role that mandates
// MFA (finance_officer) with NO second factor enrolled, mirroring the SR-M1
// threat: a privileged token whose user never set up MFA. Returns the user id.
func seedPrivilegedUnenrolled(t *testing.T, ctx context.Context, h *harness, email string) uuid.UUID {
	t.Helper()
	hasher := password.New(password.DefaultParams, "")
	hash, err := hasher.Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	var uid uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		VALUES ($1, $2, 'Finance Officer', 'active', $3, now()) RETURNING id`,
		h.ids.tenantID, email, hash).Scan(&uid); err != nil {
		t.Fatalf("seed finance officer: %v", err)
	}
	grantRole(t, ctx, h.pool, h.ids.tenantID, uid, "finance_officer")
	return uid
}

// enrollAndLoginAdminWithMfa enrolls a TOTP second factor for the seeded admin
// over the real (ungated) /me/mfa routes, then logs in with a fresh code to
// obtain a session whose MfaSatisfied=true. Returns that admin token.
func enrollAndLoginAdminWithMfa(t *testing.T, h *harness, slug string) string {
	t.Helper()
	// Pre-enrollment login (MFA off) — succeeds without a challenge but yields
	// an MfaSatisfied=false session, which is enough to drive the /me/mfa routes
	// (those are not MFA-gated).
	pre := h.login(t, slug, h.ids.adminEmail)

	code, raw := h.do(t, http.MethodPost, "/api/v1/me/mfa/enroll", pre, nil, "")
	if code != http.StatusOK {
		t.Fatalf("admin enroll: status %d: %s", code, raw)
	}
	var enr struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(raw, &enr); err != nil || enr.Secret == "" {
		t.Fatalf("admin enroll body: %v %s", err, raw)
	}
	otp, err := totp.GenerateCode(enr.Secret, time.Now())
	if err != nil {
		t.Fatalf("admin enroll: generate code: %v", err)
	}
	if c, r := h.do(t, http.MethodPost, "/api/v1/me/mfa/confirm", pre,
		bytes.NewReader([]byte(fmt.Sprintf(`{"code":%q}`, otp))), "application/json"); c != http.StatusOK {
		t.Fatalf("admin confirm: status %d: %s", c, r)
	}

	// Now log in again WITH a fresh TOTP code so the new session satisfies MFA.
	// The login replay guard rejects a code already consumed this window, so try
	// across windows if needed.
	for attempt := 0; attempt < 3; attempt++ {
		loginCode, gerr := totp.GenerateCode(enr.Secret, time.Now())
		if gerr != nil {
			t.Fatalf("admin login: generate code: %v", gerr)
		}
		payload, _ := json.Marshal(map[string]string{
			"tenant_slug": slug, "email": h.ids.adminEmail, "password": testPassword, "mfa_code": loginCode,
		})
		status, body := h.do(t, http.MethodPost, "/api/v1/auth/login", "", bytes.NewReader(payload), "application/json")
		if status == http.StatusOK {
			var out struct {
				Token string `json:"token"`
			}
			_ = json.Unmarshal(body, &out)
			if out.Token != "" {
				return out.Token
			}
		}
		time.Sleep(31 * time.Second)
	}
	t.Fatal("admin login with MFA: could not obtain a satisfied session")
	return ""
}

// TestMfaEnforcement_SRM1 proves the four SR-M1 acceptance cases with
// enforcement turned on.
func TestMfaEnforcement_SRM1(t *testing.T) {
	h, cleanup := setupHarnessOpts(t, harnessOpts{enforceMFA: true})
	defer cleanup()
	ctx := context.Background()

	var slug string
	if err := h.pool.QueryRow(ctx, `SELECT slug FROM tenants WHERE id = $1`, h.ids.tenantID).Scan(&slug); err != nil {
		t.Fatalf("lookup slug: %v", err)
	}

	// A privileged (finance_officer) user with NO MFA enrolled. Login succeeds
	// (MFA is off, so no challenge) and yields a session with MfaSatisfied=false.
	foEmail := fmt.Sprintf("fo-%d@it.local", time.Now().UnixNano())
	seedPrivilegedUnenrolled(t, ctx, h, foEmail)
	fo := h.login(t, slug, foEmail)

	// (a) A RoleRequiresMfa actor WITHOUT a satisfied second factor is refused a
	// protected admin-console route with 403 + code "mfa_required".
	t.Run("privileged_unenrolled_blocked_on_protected_route", func(t *testing.T) {
		code, raw := h.do(t, http.MethodGet, "/api/v1/accounts", fo, nil, "")
		if code != http.StatusForbidden {
			t.Fatalf("GET /accounts as unenrolled finance officer = %d (want 403): %s", code, raw)
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(raw, &body)
		if body.Code != "mfa_required" {
			t.Fatalf("error code = %q (want mfa_required): %s", body.Code, raw)
		}
	})

	// (b) The SAME actor CAN still reach the MFA-enrollment route — no lockout —
	// and /me stays reachable so the UI can detect required-but-unenrolled.
	t.Run("privileged_unenrolled_can_reach_enrollment", func(t *testing.T) {
		code, raw := h.do(t, http.MethodPost, "/api/v1/me/mfa/enroll", fo, nil, "")
		if code != http.StatusOK {
			t.Fatalf("POST /me/mfa/enroll as unenrolled finance officer = %d (want 200): %s", code, raw)
		}
		var enr struct {
			Secret string `json:"secret"`
		}
		if err := json.Unmarshal(raw, &enr); err != nil || enr.Secret == "" {
			t.Fatalf("enroll body: %v %s", err, raw)
		}
		if c, r := h.do(t, http.MethodGet, "/api/v1/me", fo, nil, ""); c != http.StatusOK {
			t.Fatalf("GET /me as unenrolled finance officer = %d (want 200): %s", c, r)
		}
	})

	// (c) A RoleRequiresMfa actor WITH a satisfied second factor passes. Enroll
	// MFA for the admin (system_admin) and log in with a code so the session is
	// MfaSatisfied, then reach the same protected route.
	t.Run("privileged_mfa_satisfied_passes", func(t *testing.T) {
		admin := enrollAndLoginAdminWithMfa(t, h, slug)
		code, raw := h.do(t, http.MethodGet, "/api/v1/accounts", admin, nil, "")
		if code != http.StatusOK {
			t.Fatalf("GET /accounts as MFA-satisfied admin = %d (want 200): %s", code, raw)
		}
	})

	// (d) A role that does NOT require MFA is unaffected by the gate: it is never
	// answered with mfa_required (it may still hit a permission 403, but not the
	// MFA gate). An attendant lacks finance.read, so /accounts returns a plain
	// forbidden with NO mfa_required code — proving the MFA gate did not fire.
	t.Run("non_mfa_role_not_gated", func(t *testing.T) {
		attEmail := fmt.Sprintf("att-mfa-%d@it.local", time.Now().UnixNano())
		seedAttendant(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, attEmail)
		att := h.login(t, slug, attEmail)

		code, raw := h.do(t, http.MethodGet, "/api/v1/accounts", att, nil, "")
		if code != http.StatusForbidden {
			t.Fatalf("GET /accounts as attendant = %d (want 403 forbidden): %s", code, raw)
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(raw, &body)
		if body.Code == "mfa_required" {
			t.Fatalf("attendant (no MFA-required role) was wrongly gated by MFA: %s", raw)
		}
	})
}
