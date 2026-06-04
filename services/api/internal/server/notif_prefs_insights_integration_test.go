package server_test

// DB-backed integration tests for Feature 11.1 (notification preferences,
// self-service) and Feature 11.3 (persisted deterministic insights with
// source-record links). Gated on TEST_DATABASE_URL + TEST_REDIS_URL.

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

// TestNotificationPreferences_SelfService covers the 11.1 contract: any
// authenticated user reads and writes their OWN preferences (no permission
// gate), the upsert is keyed per (category, channel), quiet hours validate as a
// pair, and the change is audited as notification.preference_changed.
func TestNotificationPreferences_SelfService(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)

	// A freshly-created attendant — the lowest-privilege real user — can use the
	// self-service preferences surface, proving there is no permission gate.
	email := fmt.Sprintf("pref-att-%d@it.local", time.Now().UnixNano())
	att := seedAttendant(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, email)
	token := h.login(t, tenantSlug, email)

	// Empty to start; the response still advertises the valid keys.
	code, list := h.getJSON(t, "/api/v1/notifications/preferences", token)
	if code != http.StatusOK {
		t.Fatalf("list prefs = %d %v", code, list)
	}
	if cats, _ := list["categories"].([]any); len(cats) == 0 {
		t.Fatalf("expected category keys, got %v", list["categories"])
	}
	if items, _ := list["items"].([]any); len(items) != 0 {
		t.Fatalf("expected no stored prefs initially, got %v", items)
	}

	// Disable risk in-app delivery, with a quiet window.
	code, up := h.do(t, http.MethodPut, "/api/v1/notifications/preferences", token,
		jsonBody(map[string]any{
			"category": "risk", "channel": "in_app", "enabled": false,
			"quiet_hours_start": "22:00", "quiet_hours_end": "06:00",
		}), "application/json")
	if code != http.StatusOK {
		t.Fatalf("upsert = %d %s", code, up)
	}

	// Re-toggle the same key: must UPDATE the existing row, not duplicate.
	if code, _ := h.do(t, http.MethodPut, "/api/v1/notifications/preferences", token,
		jsonBody(map[string]any{"category": "risk", "channel": "in_app", "enabled": true}),
		"application/json"); code != http.StatusOK {
		t.Fatalf("re-upsert = %d", code)
	}
	code, list = h.getJSON(t, "/api/v1/notifications/preferences", token)
	items, _ := list["items"].([]any)
	if code != http.StatusOK || len(items) != 1 {
		t.Fatalf("expected exactly 1 pref after re-toggle, got %v", list)
	}
	pref := items[0].(map[string]any)
	if pref["category"] != "risk" || pref["channel"] != "in_app" || pref["enabled"] != true {
		t.Fatalf("pref not updated in place: %v", pref)
	}

	// Validation: unknown category, unknown channel, and an unpaired/invalid
	// quiet window are each a 400.
	for _, bad := range []map[string]any{
		{"category": "bogus", "channel": "in_app", "enabled": true},
		{"category": "risk", "channel": "sms", "enabled": true},
		{"category": "risk", "channel": "email", "enabled": true, "quiet_hours_start": "22:00"},
		{"category": "risk", "channel": "email", "enabled": true, "quiet_hours_start": "25:00", "quiet_hours_end": "06:00"},
	} {
		if code, _ := h.do(t, http.MethodPut, "/api/v1/notifications/preferences", token,
			jsonBody(bad), "application/json"); code != http.StatusBadRequest {
			t.Fatalf("bad upsert %v = %d (want 400)", bad, code)
		}
	}

	// The change is audited under the actor.
	var auditN int
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_logs
		WHERE tenant_id = $1 AND actor_id = $2 AND action = 'notification.preference_changed'`,
		h.ids.tenantID, att).Scan(&auditN); err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if auditN < 1 {
		t.Fatalf("expected notification.preference_changed audit, got %d", auditN)
	}
}

// TestInsights_SourceLinks_AndForbidden covers 11.3: a persisted alert is
// surfaced as an insight with a source-record link (and href for a tank
// subject), and a user without risk.read gets 403.
func TestInsights_SourceLinks_AndForbidden(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	// Insert a persisted deterministic alert linked to a tank source record.
	alertID := uuid.New()
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO risk_alerts
			(id, tenant_id, rule_code, alert_type, severity, status, station_id,
			 subject_type, subject_id, detail, amount, recommended_action, score)
		VALUES ($1, $2, 'stockout_coverage', 'stockout_coverage', 'medium', 'open', $3,
		        'tank', $4, 'PMS T1 may reach minimum within ~6 hours.', 1200.00,
		        'Create a purchase order or schedule a delivery.', 55)`,
		alertID, h.ids.tenantID, h.ids.station1, h.ids.tankPMS); err != nil {
		t.Fatalf("seed alert: %v", err)
	}

	code, list := h.getJSON(t, "/api/v1/insights", admin)
	if code != http.StatusOK {
		t.Fatalf("list insights = %d %v", code, list)
	}
	items, _ := list["items"].([]any)
	if len(items) < 1 {
		t.Fatalf("expected at least one insight, got %v", list)
	}
	var found map[string]any
	for _, it := range items {
		m := it.(map[string]any)
		if m["id"] == alertID.String() {
			found = m
			break
		}
	}
	if found == nil {
		t.Fatalf("seeded insight not returned: %v", items)
	}
	src, _ := found["source"].(map[string]any)
	if src == nil || src["kind"] != "tank" || src["id"] != h.ids.tankPMS.String() {
		t.Fatalf("insight missing tank source link: %v", found)
	}
	if src["href"] != "/inventory" {
		t.Fatalf("tank source href = %v (want /inventory)", src["href"])
	}
	// Amount stays a decimal string, never a float.
	if _, ok := found["amount"].(string); !ok {
		t.Fatalf("amount should be a decimal string, got %T", found["amount"])
	}

	// A freshly-created attendant holds no risk.read and is 403 on /insights.
	email := fmt.Sprintf("ins-att-%d@it.local", time.Now().UnixNano())
	hash, err := password.New(password.DefaultParams, "").Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	var uid uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'No-Risk Attendant', 'active', $3, now()) RETURNING id`,
		h.ids.tenantID, email, hash).Scan(&uid); err != nil {
		t.Fatalf("seed attendant: %v", err)
	}
	grantRole(t, ctx, h.pool, h.ids.tenantID, uid, "attendant")
	att := h.login(t, tenantSlug, email)
	if code, _ := h.getJSON(t, "/api/v1/insights", att); code != http.StatusForbidden {
		t.Fatalf("attendant /insights = %d (want 403)", code)
	}
}
