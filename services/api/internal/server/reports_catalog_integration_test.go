package server_test

// DB-backed integration tests for the Reports & Intelligence Center catalog
// (REPORTS-CATALOG, Phase 1). Reuses the Phase 2 harness; gated on
// TEST_DATABASE_URL + TEST_REDIS_URL.
//
//	TEST_DATABASE_URL=postgres://fuelgrid:fuelgrid@localhost:5440/fuelgrid?sslmode=disable \
//	TEST_REDIS_URL=redis://localhost:6379/0 \
//	go test ./services/api/internal/server -run ReportCatalog -v
//
// The catalog seeds 16 system categories (migration 0105). The guarantees:
//
//	(1) a tenant-wide admin (system_admin) sees ALL 16 categories;
//	(2) permission filtering: a station_manager (no finance/risk/audit/payable
//	    permissions) sees strictly FEWER categories and never one it lacks the
//	    permission for;
//	(3) sensitive-metric gating: a supervisor holds customer.read (sees the
//	    Customer Credit card) but NOT margin.view, so the credit-exposure metric
//	    value is omitted (null) with an honest reason — never a fabricated 0;
//	(4) placeholder categories (tank/custom/scheduled) are marked honestly with a
//	    null metric;
//	(5) cross-tenant isolation: the catalog a tenant sees never carries another
//	    tenant's tenant-scoped catalog rows;
//	(6) monetary metrics are decimal STRINGS (JSON string), never numbers.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

// getCatalog fetches /reports/catalog as the token and decodes the typed shape.
func getCatalog(t *testing.T, h *harness, token string) (int, catalogBody) {
	t.Helper()
	code, raw := h.do(t, http.MethodGet, "/api/v1/reports/catalog", token, nil, "")
	if code != http.StatusOK {
		return code, catalogBody{}
	}
	var body catalogBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode catalog: %v\nbody: %s", err, raw)
	}
	return code, body
}

type catalogBody struct {
	GeneratedAt string `json:"generated_at"`
	Categories  []struct {
		Key                string `json:"key"`
		Name               string `json:"name"`
		Availability       string `json:"availability"`
		RequiredPermission string `json:"required_permission"`
		AlertCount         int    `json:"alert_count"`
		Metric             struct {
			Label  string           `json:"label"`
			Value  *json.RawMessage `json:"value"`
			Unit   string           `json:"unit"`
			Reason string           `json:"reason"`
		} `json:"metric"`
		Reports []struct {
			Key string `json:"key"`
		} `json:"reports"`
	} `json:"categories"`
	DataQuality []struct {
		CategoryKey string `json:"category_key"`
		Level       string `json:"level"`
		Message     string `json:"message"`
	} `json:"data_quality"`
}

func (b catalogBody) keys() map[string]bool {
	m := map[string]bool{}
	for i := range b.Categories {
		m[b.Categories[i].Key] = true
	}
	return m
}

func TestReportCatalog_PermissionFilteringAndGating(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)

	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	// (1) Admin (system_admin) sees ALL 16 blueprint categories.
	code, adminCat := getCatalog(t, h, admin)
	if code != http.StatusOK {
		t.Fatalf("admin catalog = %d, want 200", code)
	}
	if len(adminCat.Categories) != 16 {
		got := make([]string, 0, len(adminCat.Categories))
		for i := range adminCat.Categories {
			got = append(got, adminCat.Categories[i].Key)
		}
		t.Fatalf("admin sees %d categories, want 16: %v", len(adminCat.Categories), got)
	}
	adminKeys := adminCat.keys()
	for _, want := range []string{
		"executive", "sales", "inventory", "tank", "pump", "shift", "delivery",
		"procurement", "finance", "customer-credit", "fleet", "risk-loss",
		"audit", "custom", "scheduled", "export-history",
	} {
		if !adminKeys[want] {
			t.Errorf("admin catalog missing category %q", want)
		}
	}

	// (4) Placeholder categories are marked honestly with a null metric.
	for i := range adminCat.Categories {
		c := adminCat.Categories[i]
		if c.Key == "tank" || c.Key == "custom" || c.Key == "scheduled" {
			if c.Availability != "placeholder" {
				t.Errorf("category %q availability = %q, want placeholder", c.Key, c.Availability)
			}
			if c.Metric.Value != nil {
				t.Errorf("placeholder category %q has a non-null metric value: %s", c.Key, *c.Metric.Value)
			}
			if c.Metric.Reason == "" {
				t.Errorf("placeholder category %q has no honest reason", c.Key)
			}
		}
		// (6) Any monetary metric that IS present must be a JSON string, never a number.
		if c.Metric.Value != nil && (c.Metric.Unit == "TZS" || c.Metric.Unit == "L") {
			raw := []byte(*c.Metric.Value)
			if len(raw) == 0 || raw[0] != '"' {
				t.Errorf("category %q monetary metric is not a decimal string: %s", c.Key, raw)
			}
		}
	}

	// (2) Permission filtering: the operator (station_manager) lacks finance.read,
	// payable.read, risk.read, audit.read, reconciliation.read, fleet_report.read,
	// inventory.read — so it must see strictly FEWER categories, and never one it
	// lacks the permission for.
	op := h.login(t, tenantSlug, h.ids.opEmail)
	_, opCat := getCatalog(t, h, op)
	if len(opCat.Categories) == 0 {
		t.Fatal("operator sees no categories — reports.read gate or filtering is wrong")
	}
	if len(opCat.Categories) >= len(adminCat.Categories) {
		t.Errorf("operator sees %d categories, want fewer than admin's %d",
			len(opCat.Categories), len(adminCat.Categories))
	}
	opKeys := opCat.keys()
	// station_manager holds station.read + revenue.read but NOT risk.read /
	// audit.read / payable.read: those categories must be hidden.
	for _, hidden := range []string{"risk-loss", "audit", "procurement"} {
		if opKeys[hidden] {
			t.Errorf("operator (station_manager) should NOT see %q (lacks its permission)", hidden)
		}
	}
	// It DOES hold revenue.read (sales) and station.read (shift): those show.
	for _, shown := range []string{"sales", "shift"} {
		if !opKeys[shown] {
			t.Errorf("operator (station_manager) should see %q", shown)
		}
	}

	// (3) Sensitive-metric gating: a supervisor holds customer.read + reports.read
	// but NOT margin.view, so the Customer Credit credit-exposure metric value is
	// omitted (null) with an honest reason — never a fabricated number.
	supEmail := seedSupervisor(t, ctx, h.pool, h.ids.tenantID)
	sup := h.login(t, tenantSlug, supEmail)
	_, supCat := getCatalog(t, h, sup)
	var sawCredit bool
	for i := range supCat.Categories {
		c := supCat.Categories[i]
		if c.Key == "customer-credit" {
			sawCredit = true
			if c.Metric.Value != nil {
				t.Errorf("supervisor (no margin.view) got a credit-exposure VALUE: %s — must be gated null", *c.Metric.Value)
			}
			if c.Metric.Reason == "" {
				t.Error("gated credit-exposure metric has no honest reason")
			}
		}
		// Supervisor must never see a finance/risk/audit category it lacks.
		if c.Key == "finance" || c.Key == "risk-loss" || c.Key == "audit" {
			t.Errorf("supervisor should NOT see %q", c.Key)
		}
	}
	if !sawCredit {
		t.Error("supervisor should see the customer-credit category (holds customer.read)")
	}
}

func TestReportCatalog_CrossTenantIsolation(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()

	// A second, fully separate tenant adds a TENANT-SCOPED catalog category of
	// its own. The first tenant's admin must never see it.
	other := seedTenant(t, ctx, h.pool)
	defer cleanupTenant(ctx, h.pool, other.tenantID)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO report_categories
		    (tenant_id, key, name, description, sort_order, icon, required_permission, availability, target_route)
		VALUES ($1, 'secret-other', 'Secret Other', 'tenant-private', 999, '', 'reports.read', 'partial', '/x')
	`, other.tenantID); err != nil {
		t.Fatalf("seed other-tenant category: %v", err)
	}

	admin := h.login(t, slug(h), h.ids.adminEmail)
	_, adminCat := getCatalog(t, h, admin)
	if adminCat.keys()["secret-other"] {
		t.Error("cross-tenant leak: first tenant's admin sees the other tenant's private category")
	}
	// And the first tenant still sees exactly the 16 system categories.
	if len(adminCat.Categories) != 16 {
		t.Errorf("first tenant admin sees %d categories, want 16 (no other-tenant rows)", len(adminCat.Categories))
	}
}

// seedSupervisor creates a supervisor user (holds customer.read + reports.read,
// but NOT margin.view) for the tenant and returns its email.
func seedSupervisor(t *testing.T, ctx context.Context, pool *database.Pool, tenantID uuid.UUID) string {
	t.Helper()
	hasher := password.New(password.DefaultParams, "")
	hash, err := hasher.Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	email := "sup-" + uuid.NewString()[:8] + "@it.local"
	var supID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		VALUES ($1, $2, 'IT Supervisor', 'active', $3, now()) RETURNING id`,
		tenantID, email, hash).Scan(&supID); err != nil {
		t.Fatalf("seed supervisor user: %v", err)
	}
	grantRole(t, ctx, pool, tenantID, supID, "supervisor")
	return email
}
