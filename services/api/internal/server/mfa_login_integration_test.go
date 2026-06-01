package server_test

// DB-backed test for the TOTP MFA self-service flow: enroll → confirm (which
// enables MFA and issues one-time backup codes) → log in with a TOTP code, and
// → log in with a one-time backup code (single-use). Gated on the same
// TEST_DATABASE_URL + TEST_REDIS_URL as the rest of the suite (skips without
// infra).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// loginRaw performs a login attempt and returns the status + decoded body so a
// test can inspect mfa_required / token without t.Fatal on a non-200.
func (h *harness) loginRaw(t *testing.T, slug, email, password, mfaCode string) (int, map[string]any) {
	t.Helper()
	payload := map[string]string{"tenant_slug": slug, "email": email, "password": password}
	if mfaCode != "" {
		payload["mfa_code"] = mfaCode
	}
	raw, _ := json.Marshal(payload)
	code, out := h.do(t, http.MethodPost, "/api/v1/auth/login", "", bytes.NewReader(raw), "application/json")
	var m map[string]any
	if len(out) > 0 {
		_ = json.Unmarshal(out, &m)
	}
	return code, m
}

func TestMfa_EnrollConfirmLoginWithCode(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, tenantSlug, admin := h.adminContext(t, ctx)

	// 1) Begin enrollment — get the base32 secret for code generation.
	code, raw := h.do(t, http.MethodPost, "/api/v1/me/mfa/enroll", admin, nil, "")
	if code != http.StatusOK {
		t.Fatalf("enroll: %d %s", code, raw)
	}
	var enr struct {
		Secret     string `json:"secret"`
		OTPAuthURL string `json:"otpauth_url"`
	}
	if err := json.Unmarshal(raw, &enr); err != nil || enr.Secret == "" {
		t.Fatalf("enroll body: %v %s", err, raw)
	}

	// 2) Confirm with a freshly generated TOTP code — enables MFA, returns codes.
	otpCode, err := totp.GenerateCode(enr.Secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	code, raw = h.do(t, http.MethodPost, "/api/v1/me/mfa/confirm", admin,
		bytes.NewReader([]byte(fmt.Sprintf(`{"code":%q}`, otpCode))), "application/json")
	if code != http.StatusOK {
		t.Fatalf("confirm: %d %s", code, raw)
	}
	var confirm struct {
		BackupCodes []string `json:"backup_codes"`
	}
	if err := json.Unmarshal(raw, &confirm); err != nil || len(confirm.BackupCodes) == 0 {
		t.Fatalf("confirm body: %v %s", err, raw)
	}

	// 3) Login WITHOUT a code now reports mfa_required (no token yet).
	if c, m := h.loginRaw(t, tenantSlug, h.ids.adminEmail, testPassword, ""); c != http.StatusOK || m["mfa_required"] != true || m["token"] != nil {
		t.Fatalf("login w/o code: code=%d body=%v (want 200 mfa_required, no token)", c, m)
	}

	// 4) Login WITH a TOTP code succeeds and issues a token.
	otpCode2, err := totp.GenerateCode(enr.Secret, time.Now())
	if err != nil {
		t.Fatalf("generate code 2: %v", err)
	}
	if c, m := h.loginRaw(t, tenantSlug, h.ids.adminEmail, testPassword, otpCode2); c != http.StatusOK || m["token"] == nil {
		t.Fatalf("login w/ TOTP: code=%d body=%v (want 200 + token)", c, m)
	}

	// 5) Login WITH a one-time backup code succeeds...
	backup := confirm.BackupCodes[0]
	if c, m := h.loginRaw(t, tenantSlug, h.ids.adminEmail, testPassword, backup); c != http.StatusOK || m["token"] == nil {
		t.Fatalf("login w/ backup code: code=%d body=%v (want 200 + token)", c, m)
	}
	// ...and that same code is now consumed: re-using it is rejected.
	if c, m := h.loginRaw(t, tenantSlug, h.ids.adminEmail, testPassword, backup); c == http.StatusOK && m["token"] != nil {
		t.Fatalf("reused backup code unexpectedly succeeded: body=%v", m)
	}

	// The remaining backup-code count dropped by exactly one.
	code, raw = h.do(t, http.MethodGet, "/api/v1/me", admin, nil, "")
	if code != http.StatusOK {
		t.Fatalf("me: %d %s", code, raw)
	}
	var me struct {
		MfaEnabled    bool `json:"mfa_enabled"`
		Remaining     int  `json:"mfa_backup_codes_remaining"`
		MfaRequiredBy bool `json:"mfa_required"`
	}
	if err := json.Unmarshal(raw, &me); err != nil {
		t.Fatalf("me body: %v %s", err, raw)
	}
	if !me.MfaEnabled {
		t.Fatal("me.mfa_enabled = false after confirm")
	}
	if want := len(confirm.BackupCodes) - 1; me.Remaining != want {
		t.Fatalf("remaining backup codes = %d (want %d)", me.Remaining, want)
	}
	// The seeded admin holds system_admin — a role that mandates MFA.
	if !me.MfaRequiredBy {
		t.Fatal("me.mfa_required = false for system_admin (role policy regression)")
	}
}

func TestMfa_DisableRequiresCode(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	// Enroll + confirm to enable MFA.
	_, raw := h.do(t, http.MethodPost, "/api/v1/me/mfa/enroll", admin, nil, "")
	var enr struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(raw, &enr)
	otpCode, err := totp.GenerateCode(enr.Secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	if c, r := h.do(t, http.MethodPost, "/api/v1/me/mfa/confirm", admin,
		bytes.NewReader([]byte(fmt.Sprintf(`{"code":%q}`, otpCode))), "application/json"); c != http.StatusOK {
		t.Fatalf("confirm: %d %s", c, r)
	}

	// Disable with a bogus code is rejected (the second factor can't be
	// silently stripped).
	if c, _ := h.do(t, http.MethodPost, "/api/v1/me/mfa/disable", admin,
		bytes.NewReader([]byte(`{"code":"000000"}`)), "application/json"); c != http.StatusUnauthorized {
		t.Fatalf("disable w/ bad code: code=%d (want 401)", c)
	}

	// Disable with a valid TOTP code succeeds.
	good, err := totp.GenerateCode(enr.Secret, time.Now())
	if err != nil {
		t.Fatalf("generate good code: %v", err)
	}
	if c, r := h.do(t, http.MethodPost, "/api/v1/me/mfa/disable", admin,
		bytes.NewReader([]byte(fmt.Sprintf(`{"code":%q}`, good))), "application/json"); c != http.StatusNoContent {
		t.Fatalf("disable w/ good code: code=%d %s (want 204)", c, r)
	}
}
