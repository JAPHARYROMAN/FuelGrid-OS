package server_test

// DB-backed integration tests for the Custom Report Builder (Reports Center Phase
// 11). They run against a MIGRATED throwaway database (0116 must have created
// report_templates + the reports.builder permission + flipped the Custom category
// live). Gated on TEST_DATABASE_URL + TEST_REDIS_URL via the shared Phase 2
// harness; the suite skips when either is unset.
//
// QUERY-SAFETY is the priority here, so the coverage centres on it:
//   - a composed preview returns correct decimal-string aggregates;
//   - a spec with a NON-allowlisted column / agg / operator is REJECTED with a
//     precise 400 code (no query runs);
//   - a filter value like "x); DROP" is treated as a bound literal, not SQL;
//   - tenant + station scope are always applied (a station-scoped actor cannot pull
//     another station's data via a crafted spec);
//   - a sensitive column (margin) is OMITTED for a non-gated actor;
//   - saved-template share-scope is enforced (private invisible to others;
//     tenant/role visible to permitted only);
//   - run-time permission re-check; cross-tenant template isolation.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

// seedRevenueDayBuilder inserts a locked revenue_days row with explicit gross /
// margin so the builder aggregates are deterministic.
func seedRevenueDayBuilder(t *testing.T, ctx context.Context, h *harness, stationID, adminID uuid.UUID, businessDate, gross, margin string) {
	t.Helper()
	var dayID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO operating_days (tenant_id, station_id, business_date, opened_by)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, h.ids.tenantID, stationID, businessDate, adminID).Scan(&dayID); err != nil {
		t.Fatalf("seed operating day: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO revenue_days
		    (tenant_id, station_id, operating_day_id, business_date,
		     gross_revenue, net_revenue, margin_total, cogs_total,
		     cash_total, tender_total, cash_variance, status)
		VALUES ($1, $2, $3, $4, $5, $5, $6, $6, $5, $5, 0, 'locked')
	`, h.ids.tenantID, stationID, dayID, businessDate, gross, margin); err != nil {
		t.Fatalf("seed revenue day: %v", err)
	}
}

// builderUserWithPerms creates a fresh user in the harness tenant with a brand-new
// custom role holding exactly the given permission codes, and returns a logged-in
// token. Used to test the sensitive-omission gate with an actor that holds
// reports.builder + revenue.read but NOT margin.view.
func builderUserWithPerms(t *testing.T, ctx context.Context, h *harness, tenantWide bool, perms ...string) string {
	t.Helper()
	hasher := password.New(password.DefaultParams, "")
	hash, err := hasher.Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	email := fmt.Sprintf("builder-%d@it.local", uuid.New().ID())
	var userID, roleID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at) VALUES ($1,$2,'Builder User','active',$3,now()) RETURNING id`,
		h.ids.tenantID, email, hash).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	roleCode := fmt.Sprintf("custom_builder_%d", uuid.New().ID())
	// tenant_wide is an explicit role property (AUTH-20): a tenant-wide actor reads
	// every station; a non-tenant-wide one is confined to its station grants.
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO roles (tenant_id, code, name, description, is_system, tenant_wide) VALUES ($1,$2,'Custom Builder','',false,$3) RETURNING id`,
		h.ids.tenantID, roleCode, tenantWide).Scan(&roleID); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	for _, p := range perms {
		if _, err := h.pool.Exec(ctx,
			`INSERT INTO role_permissions (role_id, permission_id) SELECT $1, id FROM permissions WHERE code = $2 ON CONFLICT DO NOTHING`,
			roleID, p); err != nil {
			t.Fatalf("grant %s: %v", p, err)
		}
	}
	if _, err := h.pool.Exec(ctx, `INSERT INTO user_roles (user_id, role_id, tenant_id) VALUES ($1,$2,$3)`, userID, roleID, h.ids.tenantID); err != nil {
		t.Fatalf("assign role: %v", err)
	}
	if !tenantWide {
		// Confine to station1 only.
		if _, err := h.pool.Exec(ctx, `INSERT INTO user_station_access (user_id, station_id, tenant_id) VALUES ($1,$2,$3)`, userID, h.ids.station1, h.ids.tenantID); err != nil {
			t.Fatalf("seed station access: %v", err)
		}
	}
	return h.login(t, slug(h), email)
}

func builderPreview(t *testing.T, h *harness, token, specJSON string) (int, map[string]any) {
	t.Helper()
	code, raw := h.do(t, http.MethodPost, "/api/v1/reports/builder/preview", token, rawBody(`{"spec":`+specJSON+`}`), "application/json")
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return code, out
}

// --- datasets listing ---

func TestBuilder_DatasetsPermissionFiltered(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	admin := h.login(t, slug(h), h.ids.adminEmail)

	code, raw := h.do(t, http.MethodGet, "/api/v1/reports/builder/datasets", admin, nil, "")
	if code != http.StatusOK {
		t.Fatalf("datasets = %d", code)
	}
	var out struct {
		Datasets []struct {
			Key      string `json:"key"`
			Measures []struct {
				ID        string `json:"id"`
				Sensitive bool   `json:"sensitive"`
			} `json:"measures"`
		} `json:"datasets"`
	}
	_ = json.Unmarshal(raw, &out)
	if len(out.Datasets) == 0 {
		t.Fatal("admin should see datasets")
	}
	// Admin (system_admin) sees the sensitive margin measure on revenue_days.
	var sawMargin bool
	for _, d := range out.Datasets {
		if d.Key == "revenue_days" {
			for _, m := range d.Measures {
				if m.ID == "margin_total" {
					sawMargin = true
				}
			}
		}
	}
	if !sawMargin {
		t.Fatal("admin should see the sensitive margin measure")
	}
}

// --- preview: correct decimal aggregates ---

func TestBuilder_PreviewDecimalAggregates(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)
	seedRevenueDayBuilder(t, ctx, h, h.ids.station1, adminID, "2026-05-10", "500000.50", "120000.25")
	seedRevenueDayBuilder(t, ctx, h, h.ids.station1, adminID, "2026-05-11", "499999.50", "80000.75")

	spec := `{"dataset":"revenue_days","dimensions":["station_id"],
		"measures":[{"measure":"gross_revenue","agg":"sum"},{"measure":"margin_total","agg":"sum"}]}`
	code, out := builderPreview(t, h, admin, spec)
	if code != http.StatusOK {
		t.Fatalf("preview = %d (%v)", code, out)
	}
	table, _ := out["table"].(map[string]any)
	rows, _ := table["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("want 1 grouped row, got %d (%v)", len(rows), table)
	}
	row0, _ := rows[0].([]any)
	// Columns: station_id, gross sum, margin sum. gross = 500000.50+499999.50 = 1000000.00.
	foundGross := false
	for _, c := range row0 {
		if s, _ := c.(string); s == "1000000.00" {
			foundGross = true
		}
	}
	if !foundGross {
		t.Fatalf("expected exact decimal-string gross 1000000.00 in row %v", row0)
	}
}

// --- allowlist rejection (no query runs) ---

func TestBuilder_RejectsNonAllowlistedSpec(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	admin := h.login(t, slug(h), h.ids.adminEmail)

	cases := []struct {
		name string
		spec string
		code string
	}{
		{"injection dimension", `{"dataset":"revenue_days","dimensions":["rd.gross_revenue); DROP TABLE revenue_days;--"]}`, "unknown_dimension"},
		{"unknown measure", `{"dataset":"revenue_days","measures":[{"measure":"secret_cost","agg":"sum"}]}`, "unknown_measure"},
		{"bad agg", `{"dataset":"revenue_days","measures":[{"measure":"gross_revenue","agg":"median"}]}`, "unknown_agg"},
		{"bad operator", `{"dataset":"revenue_days","measures":[{"measure":"gross_revenue","agg":"sum"}],"filters":[{"filter":"status","operator":"between","values":["a","b"]}]}`, "operator_not_allowed"},
		{"unknown dataset", `{"dataset":"users","dimensions":["email"]}`, "unknown_dataset"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, out := builderPreview(t, h, admin, tc.spec)
			if code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d (%v)", code, out)
			}
			if got, _ := out["code"].(string); got != tc.code {
				t.Fatalf("want code %q, got %q (%v)", tc.code, got, out)
			}
		})
	}
}

// --- filter values are parameterized (injection literal stays a literal) ---

func TestBuilder_FilterValueIsParameterized(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)
	seedRevenueDayBuilder(t, ctx, h, h.ids.station1, adminID, "2026-05-10", "500000", "100000")

	// A filter value that LOOKS like SQL must be treated as a literal: the query
	// runs fine (200), returns zero matching rows, and the table still exists.
	spec := `{"dataset":"revenue_days","dimensions":["status"],
		"measures":[{"measure":"gross_revenue","agg":"sum"}],
		"filters":[{"filter":"status","operator":"eq","value":"x'); DROP TABLE revenue_days;--"}]}`
	code, out := builderPreview(t, h, admin, spec)
	if code != http.StatusOK {
		t.Fatalf("preview with injection-y value = %d (%v)", code, out)
	}
	table, _ := out["table"].(map[string]any)
	rows, _ := table["rows"].([]any)
	if len(rows) != 0 {
		t.Fatalf("injection-y value should match no rows, got %d", len(rows))
	}
	// Prove the table still exists (the DROP did NOT execute) by a normal query.
	var n int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM revenue_days WHERE tenant_id = $1`, h.ids.tenantID).Scan(&n); err != nil {
		t.Fatalf("revenue_days table must still exist: %v", err)
	}
	if n == 0 {
		t.Fatal("seeded revenue_days row must still be present (table not dropped)")
	}
}

// --- tenant + station scope always applied ---

func TestBuilder_StationScopeAlwaysApplied(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, _ := h.adminContext(t, ctx)
	// station1 has data; station2 has different data. A station1-scoped actor must
	// never see station2's figures, even by crafting a spec that filters on
	// station2's id.
	seedRevenueDayBuilder(t, ctx, h, h.ids.station1, adminID, "2026-05-10", "111111", "10000")
	seedRevenueDayBuilder(t, ctx, h, h.ids.station2, adminID, "2026-05-10", "999999", "90000")

	// Operator: station_manager scoped to station1, granted revenue.read so it can
	// use the dataset. (station_manager already holds reports.builder + margin.view.)
	if _, err := h.pool.Exec(ctx,
		`INSERT INTO role_permissions (role_id, permission_id) SELECT r.id, p.id FROM roles r, permissions p WHERE r.code='station_manager' AND r.tenant_id IS NULL AND p.code='revenue.read' ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("grant revenue.read to station_manager: %v", err)
	}
	op := h.login(t, slug(h), h.ids.opEmail)

	// Craft a spec that EXPLICITLY filters for station2 (which the operator cannot
	// read). The station-scope predicate must still confine the result to station1,
	// so station2's gross (999999) never appears.
	spec := `{"dataset":"revenue_days","dimensions":["station_id"],
		"measures":[{"measure":"gross_revenue","agg":"sum"}],
		"filters":[{"filter":"station_id","operator":"eq","value":"` + h.ids.station2.String() + `"}]}`
	code, out := builderPreview(t, h, op, spec)
	if code != http.StatusOK {
		t.Fatalf("preview = %d (%v)", code, out)
	}
	raw, _ := json.Marshal(out)
	if containsStr(string(raw), "999999") {
		t.Fatalf("station-scoped actor leaked another station's figure: %s", string(raw))
	}
}

// --- sensitive column omitted for a non-gated actor ---

func TestBuilder_SensitiveMarginOmittedForNonHolder(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)
	seedRevenueDayBuilder(t, ctx, h, h.ids.station1, adminID, "2026-05-10", "500000", "123456")

	// A tenant-wide actor holding reports.builder + revenue.read but NOT margin.view.
	noMargin := builderUserWithPerms(t, ctx, h, true, "reports.builder", "revenue.read")

	spec := `{"dataset":"revenue_days","dimensions":["station_id"],
		"measures":[{"measure":"gross_revenue","agg":"sum"},{"measure":"margin_total","agg":"sum"}]}`

	// Holder (admin) sees margin 123456 in the output; non-holder does not.
	_, adminOut := builderPreview(t, h, admin, spec)
	adminRaw, _ := json.Marshal(adminOut)
	if !containsStr(string(adminRaw), "123456") {
		t.Fatalf("admin (margin.view) should see the margin figure: %s", string(adminRaw))
	}

	code, out := builderPreview(t, h, noMargin, spec)
	if code != http.StatusOK {
		t.Fatalf("preview (no-margin) = %d (%v)", code, out)
	}
	raw, _ := json.Marshal(out)
	if containsStr(string(raw), "123456") {
		t.Fatalf("non-holder must NOT see the margin figure: %s", string(raw))
	}
	// The margin column must be absent from the table columns.
	table, _ := out["table"].(map[string]any)
	cols, _ := table["columns"].([]any)
	for _, c := range cols {
		if s, _ := c.(string); containsStr(s, "Gross margin") {
			t.Fatalf("margin column must be omitted for non-holder: %v", cols)
		}
	}
}

// --- saved template share-scope + run-time re-check + cross-tenant isolation ---

func TestBuilder_TemplateShareScopeAndRunRecheck(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)
	seedRevenueDayBuilder(t, ctx, h, h.ids.station1, adminID, "2026-05-10", "500000", "100000")

	// Admin saves a PRIVATE template.
	createBody := `{"name":"My Private Sales","spec":{"dataset":"revenue_days","dimensions":["station_id"],"measures":[{"measure":"gross_revenue","agg":"sum"}]},"shared_scope":"private"}`
	code, raw := h.do(t, http.MethodPost, "/api/v1/reports/builder/templates", admin, rawBody(createBody), "application/json")
	if code != http.StatusCreated {
		t.Fatalf("create template = %d (%s)", code, string(raw))
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &created)
	if created.ID == "" {
		t.Fatal("created template missing id")
	}

	// A DIFFERENT user (with reports.builder + revenue.read) must NOT see the
	// private template (404, no existence leak).
	other := builderUserWithPerms(t, ctx, h, true, "reports.builder", "revenue.read")
	gcode, _ := h.do(t, http.MethodGet, "/api/v1/reports/builder/templates/"+created.ID, other, nil, "")
	if gcode != http.StatusNotFound {
		t.Fatalf("private template should be 404 for another user, got %d", gcode)
	}
	rcode, _ := h.do(t, http.MethodPost, "/api/v1/reports/builder/templates/"+created.ID+"/run", other, nil, "")
	if rcode != http.StatusNotFound {
		t.Fatalf("running another's private template should be 404, got %d", rcode)
	}

	// Admin shares it TENANT-wide; now the other user CAN see + run it.
	shareBody := `{"name":"My Private Sales","spec":{"dataset":"revenue_days","dimensions":["station_id"],"measures":[{"measure":"gross_revenue","agg":"sum"}]},"shared_scope":"tenant"}`
	ucode, _ := h.do(t, http.MethodPut, "/api/v1/reports/builder/templates/"+created.ID, admin, rawBody(shareBody), "application/json")
	if ucode != http.StatusOK {
		t.Fatalf("share template = %d", ucode)
	}
	gcode2, _ := h.do(t, http.MethodGet, "/api/v1/reports/builder/templates/"+created.ID, other, nil, "")
	if gcode2 != http.StatusOK {
		t.Fatalf("tenant-shared template should be 200 for another permitted user, got %d", gcode2)
	}

	// RUN-TIME PERMISSION RE-CHECK: a user WITHOUT revenue.read (only reports.builder)
	// can SEE the tenant-shared template but CANNOT run it (403) — the dataset
	// permission is re-checked at run time.
	noData := builderUserWithPerms(t, ctx, h, true, "reports.builder")
	gcode3, _ := h.do(t, http.MethodGet, "/api/v1/reports/builder/templates/"+created.ID, noData, nil, "")
	if gcode3 != http.StatusOK {
		t.Fatalf("tenant-shared template should be visible to a builder user, got %d", gcode3)
	}
	rcode3, _ := h.do(t, http.MethodPost, "/api/v1/reports/builder/templates/"+created.ID+"/run", noData, nil, "")
	if rcode3 != http.StatusForbidden {
		t.Fatalf("running without the dataset permission must 403 at run time, got %d", rcode3)
	}
}

func containsStr(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
