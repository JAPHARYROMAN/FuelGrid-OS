package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// TestSRL3_PasswordResetConfirmRateLimited covers SR-L3: the public
// password-reset endpoints sit outside both the per-tenant limiter and the
// identity login buckets, so they are guarded by a dedicated per-IP limiter.
// With a small max configured, repeated /password-reset/confirm calls from the
// same IP must eventually be rejected with 429.
//
// The reset confirm itself returns 400 for an unknown/garbage token (no token
// is leaked here); the point of this test is that the limiter trips regardless
// of the per-request outcome, before the handler's own logic, once the per-IP
// budget is exhausted.
func TestSRL3_PasswordResetConfirmRateLimited(t *testing.T) {
	const maxAttempts = 3
	h, cleanup := setupHarnessOpts(t, harnessOpts{
		pwResetRateMax: maxAttempts,
		pwResetRateWin: time.Minute,
	})
	defer cleanup()

	body, _ := json.Marshal(map[string]string{
		"token":        "not-a-real-reset-token",
		"new_password": "Sup3rSecretP@ss!",
	})

	var got429 bool
	// Fire more than the budget; the first maxAttempts may be 400 (bad token),
	// then the limiter must start returning 429.
	for i := 0; i < maxAttempts+3; i++ {
		code, _ := h.do(t, http.MethodPost, "/api/v1/auth/password-reset/confirm", "",
			bytes.NewReader(body), "application/json")
		if code == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatalf("expected a 429 after exceeding %d password-reset-confirm attempts from the same IP", maxAttempts)
	}
}

// TestSRL3_PasswordResetNotLimitedWhenDisabled is a guard: with the per-IP
// limit left at its harness default (0 = disabled), repeated confirm calls are
// never throttled — so the guard is strictly opt-in and the rest of the suite
// is unaffected.
func TestSRL3_PasswordResetNotLimitedWhenDisabled(t *testing.T) {
	h, cleanup := setupHarness(t) // pwResetRateMax defaults to 0 (disabled)
	defer cleanup()

	body, _ := json.Marshal(map[string]string{
		"token":        "not-a-real-reset-token",
		"new_password": "Sup3rSecretP@ss!",
	})
	for i := 0; i < 8; i++ {
		code, raw := h.do(t, http.MethodPost, "/api/v1/auth/password-reset/confirm", "",
			bytes.NewReader(body), "application/json")
		if code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d: limiter must be disabled by default, got 429 body=%s", i, raw)
		}
	}
}
