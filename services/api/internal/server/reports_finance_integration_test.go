package server_test

// DB-backed integration tests for the Finance P&L (§5.8) structured report.
// Reuses the Phase 2 harness; gated on TEST_DATABASE_URL + TEST_REDIS_URL.
//
//	TEST_DATABASE_URL=postgres://fuelgrid:fuelgrid@localhost:5447/fuelgrid_fin_test?sslmode=disable \
//	TEST_REDIS_URL=redis://localhost:6390/0 \
//	go test ./services/api/internal/server -run ReportsFinance -v
//
// They assert:
//
//	(a) the P&L KPIs + waterfall are summed in SQL ::numeric and surfaced to a
//	    margin.view holder (admin): net revenue, gross margin = net − COGS, net
//	    operating = margin − expenses, cash position, and a full revenue → COGS →
//	    gross margin → expenses → net operating waterfall with cost_shown:true;
//	(b) the COGS / gross-margin gate: a finance.read holder WITHOUT margin.view
//	    sees revenue / expenses / cash but never COGS or margin — the KPI is
//	    omitted, the waterfall has no COGS step, cost_shown:false, and a
//	    margin-hidden data-quality note is raised; and
//	(c) a freshly-created attendant (no finance.read) is forbidden (403).

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

// financeWaterfallKeys returns the ordered step keys of the report's
// chart_data.waterfall (or nil), for asserting the cascade shape.
func financeWaterfallKeys(body map[string]any) []string {
	chart, ok := body["chart_data"].(map[string]any)
	if !ok {
		return nil
	}
	steps, ok := chart["waterfall"].([]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(steps))
	for _, s := range steps {
		row, _ := s.(map[string]any)
		if k, ok := row["key"].(string); ok {
			keys = append(keys, k)
		}
	}
	return keys
}

func TestReportsFinance_PnlAndWaterfall(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	adminID := adminUserID(t, ctx, h.pool, h.ids.tenantID)

	// One recognized sale (net 1000, cogs 700 -> margin 300) plus a posted
	// expense of 100 -> net operating = 300 - 100 = 200. The same figures the
	// Profitability report ties out to (reused, not recomputed).
	seedProfitSale(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, h.ids.tankPMS, h.ids.pmsProduct, adminID,
		"1180", "180", "1000", "700", "400.000")
	seedExpense(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, adminID, "100")

	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	code, body := h.getJSON(t, "/api/v1/reports/finance?station_id="+h.ids.station1.String()+"&period=this-month", admin)
	if code != http.StatusOK {
		t.Fatalf("finance report = %d, want 200 (%v)", code, body)
	}

	// (a) KPI hero — figures reused from the SQL ::numeric Profitability totals.
	if got := summaryValue(body, "Net revenue"); got != "1000.00" {
		t.Fatalf("net revenue = %q, want 1000.00", got)
	}
	if got := summaryValue(body, "Operating expenses"); got != "100.00" {
		t.Fatalf("expenses = %q, want 100.00", got)
	}
	if got := summaryValue(body, "Gross margin"); got != "300.00" {
		t.Fatalf("gross margin = %q, want 300.00 (net − cogs)", got)
	}
	if got := summaryValue(body, "Net operating result"); got != "200.00" {
		t.Fatalf("net operating = %q, want 200.00 (margin − expenses)", got)
	}

	// The full P&L waterfall: revenue → COGS → gross margin → expenses → net.
	chart, _ := body["chart_data"].(map[string]any)
	if chart["cost_shown"] != true {
		t.Fatalf("chart_data.cost_shown = %v, want true for a margin.view holder", chart["cost_shown"])
	}
	keys := financeWaterfallKeys(body)
	wantKeys := []string{"revenue", "cogs", "gross_margin", "expenses", "net_operating"}
	if strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
		t.Fatalf("waterfall keys = %v, want %v", keys, wantKeys)
	}
	// The COGS step carries the magnitude (700) flagged negative — never recomputed.
	steps, _ := chart["waterfall"].([]any)
	for _, s := range steps {
		row, _ := s.(map[string]any)
		if row["key"] == "cogs" {
			if row["value"] != "700.00" {
				t.Fatalf("cogs waterfall step value = %v, want 700.00", row["value"])
			}
			if row["negative"] != true {
				t.Fatalf("cogs waterfall step negative = %v, want true", row["negative"])
			}
		}
	}

	// The embedded financial statements are surfaced (trial-balance / P&L / balance
	// sheet / GL), each pointing at its existing /finance/reports/* endpoint.
	stmts, _ := chart["statements"].([]any)
	if len(stmts) != 4 {
		t.Fatalf("chart_data.statements = %d, want the 4 finance statements", len(stmts))
	}
}

func TestReportsFinance_MarginGatedAndPermission(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	adminID := adminUserID(t, ctx, h.pool, h.ids.tenantID)

	seedProfitSale(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, h.ids.tankPMS, h.ids.pmsProduct, adminID,
		"1180", "180", "1000", "700", "400.000")
	seedExpense(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, adminID, "100")

	// A user holding ONLY finance.read (no margin.view): the report must show
	// revenue / expenses / cash but omit COGS + gross margin, drop the COGS
	// waterfall step, flag cost_shown:false and raise the margin-hidden note.
	fin := freshFinanceOnly(t, ctx, h, tenantSlug)
	code, body := h.getJSON(t, "/api/v1/reports/finance?station_id="+h.ids.station1.String(), fin)
	if code != http.StatusOK {
		t.Fatalf("finance-only finance report = %d, want 200 (%v)", code, body)
	}
	// Revenue + expenses stay visible.
	if got := summaryValue(body, "Net revenue"); got != "1000.00" {
		t.Fatalf("net revenue = %q, want 1000.00 (always visible)", got)
	}
	if got := summaryValue(body, "Operating expenses"); got != "100.00" {
		t.Fatalf("expenses = %q, want 100.00 (always visible)", got)
	}
	// COGS / gross margin / net margin are OMITTED (not zeroed).
	if got := summaryValue(body, "Gross margin"); got != "" {
		t.Fatalf("gross margin = %q, want omitted for a non-margin.view actor", got)
	}
	if got := summaryValue(body, "Net operating result"); got != "" {
		t.Fatalf("net operating = %q, want omitted for a non-margin.view actor", got)
	}
	chart, _ := body["chart_data"].(map[string]any)
	if chart["cost_shown"] != false {
		t.Fatalf("chart_data.cost_shown = %v, want false for a non-margin.view actor", chart["cost_shown"])
	}
	// The waterfall has NO COGS / gross-margin step — only revenue → expenses → net.
	keys := financeWaterfallKeys(body)
	for _, k := range keys {
		if k == "cogs" || k == "gross_margin" {
			t.Fatalf("waterfall leaked a cost step %q to a non-margin.view actor: %v", k, keys)
		}
	}
	wantKeys := []string{"revenue", "expenses", "net_of_expenses"}
	if strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
		t.Fatalf("non-cost waterfall keys = %v, want %v", keys, wantKeys)
	}
	// The per-product rows omit cogs + margin entirely.
	if rows, ok := chart["by_product"].([]any); ok {
		for _, r := range rows {
			row, _ := r.(map[string]any)
			if _, present := row["cogs"]; present {
				t.Fatalf("by_product row leaked a cogs field to a non-margin.view actor: %v", row)
			}
			if _, present := row["margin"]; present {
				t.Fatalf("by_product row leaked a margin field to a non-margin.view actor: %v", row)
			}
		}
	}
	// The margin-hidden data-quality note is present.
	var hasMarginNote bool
	if dq, ok := body["data_quality"].([]any); ok {
		for _, d := range dq {
			item, _ := d.(map[string]any)
			if msg, _ := item["message"].(string); strings.Contains(msg, "margin.view") {
				hasMarginNote = true
			}
		}
	}
	if !hasMarginNote {
		t.Fatalf("expected a margin-hidden data-quality note for a non-margin.view actor: %v", body["data_quality"])
	}

	// A freshly-created attendant holds no finance.read: the report is 403.
	att := freshAttendant(t, ctx, h, tenantSlug)
	if code, _ := h.getJSON(t, "/api/v1/reports/finance?station_id="+h.ids.station1.String(), att); code != http.StatusForbidden {
		t.Fatalf("attendant finance report = %d, want 403", code)
	}
}

// freshFinanceOnly creates a brand-new user holding ONLY finance.read (via a
// bespoke tenant-scoped role granted exactly that one permission, so it does NOT
// inherit margin.view the way every seeded finance.read system role does) with a
// station-1 grant, and logs in. Used to exercise the COGS / margin sensitive-
// metric gate from a pure finance reader.
func freshFinanceOnly(t *testing.T, ctx context.Context, h *harness, tenantSlug string) string {
	t.Helper()
	hash, err := password.New(password.DefaultParams, "").Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	email := "fin-report-" + uuid.NewString()[:8] + "@it.local"
	var uid uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'Report Finance', 'active', $3, now()) RETURNING id`,
		h.ids.tenantID, email, hash).Scan(&uid); err != nil {
		t.Fatalf("seed finance user: %v", err)
	}
	grantPermissionRole(t, ctx, h.pool, h.ids.tenantID, uid, "finance_read_only", "finance.read")
	if _, err := h.pool.Exec(ctx,
		`INSERT INTO user_station_access (user_id, station_id, tenant_id) VALUES ($1, $2, $3)`,
		uid, h.ids.station1, h.ids.tenantID); err != nil {
		t.Fatalf("station access: %v", err)
	}
	return h.login(t, tenantSlug, email)
}

// grantPermissionRole creates (idempotently) a tenant-scoped role holding exactly
// the named permission and grants it to the user. Unlike grantRole (system roles
// with fixed permission bundles), this lets a test hold one permission in
// isolation — here finance.read WITHOUT margin.view, to exercise the cost gate.
func grantPermissionRole(t *testing.T, ctx context.Context, pool *database.Pool, tenantID, userID uuid.UUID, roleCode, permCode string) {
	t.Helper()
	var roleID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO roles (tenant_id, code, name, is_system)
		VALUES ($1, $2, $2, false)
		ON CONFLICT (tenant_id, code) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`, tenantID, roleCode).Scan(&roleID); err != nil {
		t.Fatalf("create role %s: %v", roleCode, err)
	}
	var permID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM permissions WHERE code = $1`, permCode).Scan(&permID); err != nil {
		t.Fatalf("lookup permission %s: %v", permCode, err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO role_permissions (role_id, permission_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		roleID, permID); err != nil {
		t.Fatalf("grant permission: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_roles (user_id, role_id, tenant_id) VALUES ($1, $2, $3)`,
		userID, roleID, tenantID); err != nil {
		t.Fatalf("grant role: %v", err)
	}
}
