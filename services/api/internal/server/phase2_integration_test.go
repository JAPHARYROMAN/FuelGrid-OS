package server_test

// DB-backed integration tests for the Phase 2 surface. They boot the real
// API (on a free localhost port) against a Postgres + Redis, seed a unique
// tenant, log in over the real auth path as both a tenant-wide admin and a
// station-restricted operator, and assert authorization, the nozzle DB
// invariant, calibration upload/lookup/supersede, audit+outbox atomicity,
// and the soft-delete + lifecycle guards.
//
// Gated on TEST_DATABASE_URL (a migrated database) and TEST_REDIS_URL; the
// suite skips when either is unset, so `go test ./...` stays green without
// infra. Locally:
//
//	TEST_DATABASE_URL=postgres://fuelgrid:fuelgrid@localhost:5433/fuelgrid?sslmode=disable \
//	TEST_REDIS_URL=redis://localhost:6379/0 \
//	go test ./services/api/internal/server -run Phase2 -v

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"

	"github.com/japharyroman/fuelgrid-os/internal/cache"
	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
	"github.com/japharyroman/fuelgrid-os/internal/identity/ratelimit"
	"github.com/japharyroman/fuelgrid-os/internal/identity/repo"
	"github.com/japharyroman/fuelgrid-os/internal/identity/session"
	"github.com/japharyroman/fuelgrid-os/internal/observability"
	"github.com/japharyroman/fuelgrid-os/services/api/internal/config"
	"github.com/japharyroman/fuelgrid-os/services/api/internal/logging"
	"github.com/japharyroman/fuelgrid-os/services/api/internal/server"
)

const testPassword = "integration-test-password-123"

type seedIDs struct {
	tenantID   uuid.UUID
	station1   uuid.UUID // MIK-01 — operator is scoped here
	station2   uuid.UUID // MSA-01 — out of operator scope
	pmsProduct uuid.UUID
	agoProduct uuid.UUID
	tankPMS    uuid.UUID // station1, PMS
	tankAGO    uuid.UUID // station1, AGO
	tankMSA    uuid.UUID // station2, PMS
	pump1      uuid.UUID // station1
	adminEmail string
	opEmail    string
	opID       uuid.UUID // the seeded operator (station_manager) — used as a non-admin shift closer
}

type harness struct {
	baseURL string
	pool    *database.Pool
	ids     seedIDs
}

func setupHarness(t *testing.T) (*harness, func()) {
	return setupHarnessOpts(t, harnessOpts{})
}

// setupHarnessRLS is setupHarness with an option to connect request-scoped
// queries as the non-owner fuelgrid_app role, so Postgres RLS is enforced
// end-to-end through the real HTTP middleware (DATABASE_APP_URL behaviour).
func setupHarnessRLS(t *testing.T, enableRLS bool) (*harness, func()) {
	return setupHarnessOpts(t, harnessOpts{enableRLS: enableRLS})
}

// harnessOpts toggles the optional behaviours of the integration harness.
type harnessOpts struct {
	// enableRLS connects request-scoped queries as the non-owner fuelgrid_app
	// role so Postgres RLS is enforced end-to-end (DATABASE_APP_URL behaviour).
	enableRLS bool
	// enforceMFA turns on AUTH_ENFORCE_MFA_FOR_PRIVILEGED_ROLES (SR-M1). Off by
	// default so the many multi-privileged-user maker-checker tests, which seed
	// second approvers without MFA, keep working unchanged. The dedicated SR-M1
	// test opts it on.
	enforceMFA bool
	// pwResetRateMax sets the per-IP password-reset rate limit (SR-L3). Zero (the
	// default) disables the guard so existing tests are unaffected; the dedicated
	// SR-L3 test sets a small value to prove the limiter trips with 429.
	pwResetRateMax int64
	pwResetRateWin time.Duration
}

// setupHarnessOpts is the shared harness builder behind setupHarness /
// setupHarnessRLS and the SR-M1 MFA-enforcement harness.
func setupHarnessOpts(t *testing.T, opts harnessOpts) (*harness, func()) {
	t.Helper()
	enableRLS := opts.enableRLS
	dbURL := os.Getenv("TEST_DATABASE_URL")
	redisURL := os.Getenv("TEST_REDIS_URL")
	if dbURL == "" || redisURL == "" {
		t.Skip("set TEST_DATABASE_URL and TEST_REDIS_URL to run Phase 2 integration tests")
	}

	ctx := context.Background()
	pool, err := database.Connect(ctx, database.Config{URL: dbURL})
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	redis, err := cache.Connect(ctx, redisURL)
	if err != nil {
		pool.Close()
		t.Fatalf("connect redis: %v", err)
	}

	ids := seedTenant(t, ctx, pool)

	logger := logging.New("error", "json")
	hasher := password.New(password.DefaultParams, "")
	prefix := fmt.Sprintf("ittest:%d:", time.Now().UnixNano())
	store := session.NewRedisStore(redis, prefix+"session:")
	limiter := ratelimit.New(redis, prefix+"ratelimit:")

	identitySvc := identity.NewService(
		identity.ServiceConfig{
			SessionTTL:       time.Hour,
			LoginLockAfter:   1000,
			LoginLockFor:     time.Minute,
			LoginRateMax:     1000,
			LoginRateWindow:  time.Minute,
			PasswordResetTTL: time.Hour,
		},
		pool, hasher, repo.NewUserRepo(pool), repo.NewSessionRepo(pool),
		store, limiter, redis, logger, "",
	)

	port := freePort(t)
	cfg := config.Config{
		Env: "development", Host: "127.0.0.1", Port: port,
		CORSOrigins: []string{"http://localhost:3000"}, ShutdownTimeout: 5 * time.Second,
		// SR-M1: off by default (harness), opt-in for the enforcement test.
		AuthEnforceMfaForPrivilegedRoles: opts.enforceMFA,
		// SR-L3: per-IP password-reset limit; 0 disables it (harness default),
		// the dedicated SR-L3 test opts in with a small max.
		AuthPasswordResetRateMax:    opts.pwResetRateMax,
		AuthPasswordResetRateWindow: opts.pwResetRateWin,
	}
	var appDB *database.Pool
	appClose := func() {}
	if enableRLS {
		if _, err := pool.Exec(ctx, `ALTER ROLE fuelgrid_app WITH LOGIN PASSWORD 'fuelgrid_app'`); err != nil {
			pool.Close()
			_ = redis.Close()
			t.Fatalf("ensure fuelgrid_app password: %v", err)
		}
		appURL, aerr := appRoleURL(dbURL)
		if aerr != nil {
			pool.Close()
			_ = redis.Close()
			t.Fatalf("app url: %v", aerr)
		}
		ap, aerr := database.Connect(ctx, database.Config{URL: appURL})
		if aerr != nil {
			pool.Close()
			_ = redis.Close()
			t.Fatalf("app pool (fuelgrid_app): %v", aerr)
		}
		appDB = ap
		appClose = ap.Close
	}

	srv := server.New(cfg, logger, server.Deps{
		DB: pool, AppDB: appDB, Redis: redis, Identity: identitySvc,
		Policy: policy.NewService(policy.NewDBLoader(pool)), Metrics: observability.NewMetrics(),
	})
	go func() { _ = srv.Start() }()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitReady(t, base)

	cleanup := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		cleanupTenant(ctx, pool, ids.tenantID)
		_ = redis.Close()
		appClose()
		pool.Close()
	}
	return &harness{baseURL: base, pool: pool, ids: ids}, cleanup
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitReady(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/healthz") //nolint:noctx // test
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server did not become ready")
}

func seedTenant(t *testing.T, ctx context.Context, pool *database.Pool) seedIDs {
	t.Helper()
	hasher := password.New(password.DefaultParams, "")
	hash, err := hasher.Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	suffix := time.Now().UnixNano()
	var ids seedIDs
	ids.adminEmail = fmt.Sprintf("admin-%d@it.local", suffix)
	ids.opEmail = fmt.Sprintf("op-%d@it.local", suffix)

	q := func(dest *uuid.UUID, sql string, args ...any) {
		if err := pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("seed %q: %v", sql, err)
		}
	}

	q(&ids.tenantID, `INSERT INTO tenants (name, slug) VALUES ('IT Co', $1) RETURNING id`,
		fmt.Sprintf("ittest-%d", suffix))
	var companyID uuid.UUID
	q(&companyID, `INSERT INTO companies (tenant_id, name) VALUES ($1, 'IT Co') RETURNING id`, ids.tenantID)
	q(&ids.station1, `INSERT INTO stations (tenant_id, company_id, name, code) VALUES ($1, $2, 'Mikocheni', 'MIK-01') RETURNING id`, ids.tenantID, companyID)
	q(&ids.station2, `INSERT INTO stations (tenant_id, company_id, name, code) VALUES ($1, $2, 'Mlimani', 'MSA-01') RETURNING id`, ids.tenantID, companyID)
	q(&ids.pmsProduct, `INSERT INTO products (tenant_id, code, name, default_price, color) VALUES ($1, 'PMS', 'Premium', 2950.00, '#f97316') RETURNING id`, ids.tenantID)
	q(&ids.agoProduct, `INSERT INTO products (tenant_id, code, name, default_price, color) VALUES ($1, 'AGO', 'Diesel', 2820.00, '#2563eb') RETURNING id`, ids.tenantID)
	q(&ids.tankPMS, `INSERT INTO tanks (tenant_id, station_id, product_id, name, code, capacity_litres, safe_max_litres) VALUES ($1, $2, $3, 'PMS T1', 'T1', 30000, 28500) RETURNING id`, ids.tenantID, ids.station1, ids.pmsProduct)
	q(&ids.tankAGO, `INSERT INTO tanks (tenant_id, station_id, product_id, name, code, capacity_litres, safe_max_litres) VALUES ($1, $2, $3, 'AGO T2', 'T2', 30000, 28500) RETURNING id`, ids.tenantID, ids.station1, ids.agoProduct)
	q(&ids.tankMSA, `INSERT INTO tanks (tenant_id, station_id, product_id, name, code, capacity_litres, safe_max_litres) VALUES ($1, $2, $3, 'PMS T1', 'T1', 25000, 23750) RETURNING id`, ids.tenantID, ids.station2, ids.pmsProduct)
	q(&ids.pump1, `INSERT INTO pumps (tenant_id, station_id, number, name) VALUES ($1, $2, 1, 'Pump 1') RETURNING id`, ids.tenantID, ids.station1)
	if _, err := pool.Exec(ctx, `INSERT INTO nozzles (tenant_id, station_id, pump_id, tank_id, product_id, number, default_price) VALUES ($1, $2, $3, $4, $5, 1, 2950.00)`,
		ids.tenantID, ids.station1, ids.pump1, ids.tankPMS, ids.pmsProduct); err != nil {
		t.Fatalf("seed nozzle: %v", err)
	}

	// Admin (system_admin, tenant-wide) and operator (station_manager scoped to station1).
	var adminID, opID uuid.UUID
	q(&adminID, `INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at) VALUES ($1, $2, 'IT Admin', 'active', $3, now()) RETURNING id`, ids.tenantID, ids.adminEmail, hash)
	q(&opID, `INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at) VALUES ($1, $2, 'IT Operator', 'active', $3, now()) RETURNING id`, ids.tenantID, ids.opEmail, hash)
	ids.opID = opID
	grantRole(t, ctx, pool, ids.tenantID, adminID, "system_admin")
	grantRole(t, ctx, pool, ids.tenantID, opID, "station_manager")
	if _, err := pool.Exec(ctx, `INSERT INTO user_station_access (user_id, station_id, tenant_id) VALUES ($1, $2, $3)`, opID, ids.station1, ids.tenantID); err != nil {
		t.Fatalf("seed station access: %v", err)
	}
	return ids
}

// seedOpeningStock posts a genuine 'opening' stock movement (idempotently) for
// every non-deleted tank at the station, mirroring the predicate that
// setup.Counts uses to recognise opening stock (movement_type='opening',
// status='posted', source_ref_type IS NULL). This satisfies the per-station
// open-shift operational prerequisite so shift-opening test fixtures pass the
// readiness guard. recorded_by is the tenant's admin user.
func seedOpeningStock(t *testing.T, ctx context.Context, pool *database.Pool, tenantID, stationID uuid.UUID) {
	t.Helper()
	var adminID uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT u.id FROM users u
		JOIN user_roles ur ON ur.user_id = u.id AND ur.tenant_id = u.tenant_id
		JOIN roles r ON r.id = ur.role_id
		WHERE u.tenant_id = $1 AND r.code = 'system_admin'
		ORDER BY u.created_at LIMIT 1`, tenantID).Scan(&adminID); err != nil {
		t.Fatalf("seed opening stock: lookup admin: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO stock_movements (tenant_id, tank_id, movement_type, litres, balance_after, recorded_by, status)
		SELECT $1, t.id, 'opening', 10000.000, 10000.000, $2, 'posted'
		FROM tanks t
		WHERE t.tenant_id = $1 AND t.station_id = $3 AND t.status <> 'deleted'
		ON CONFLICT DO NOTHING`,
		tenantID, adminID, stationID); err != nil {
		t.Fatalf("seed opening stock: %v", err)
	}
}

func grantRole(t *testing.T, ctx context.Context, pool *database.Pool, tenantID, userID uuid.UUID, code string) {
	t.Helper()
	var roleID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM roles WHERE code = $1 AND is_system`, code).Scan(&roleID); err != nil {
		t.Fatalf("role %s: %v", code, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_roles (user_id, role_id, tenant_id) VALUES ($1, $2, $3)`, userID, roleID, tenantID); err != nil {
		t.Fatalf("grant role: %v", err)
	}
}

func cleanupTenant(ctx context.Context, pool *database.Pool, tenantID uuid.UUID) {
	// Delete children before parents to satisfy RESTRICT FKs.
	stmts := []string{
		`DELETE FROM outbox_events WHERE tenant_id = $1`,
		`DELETE FROM audit_logs WHERE tenant_id = $1`,
		`DELETE FROM investigation_case_actions WHERE tenant_id = $1`,
		`DELETE FROM investigation_case_comments WHERE tenant_id = $1`,
		`DELETE FROM investigation_case_alerts WHERE tenant_id = $1`,
		`DELETE FROM investigation_cases WHERE tenant_id = $1`,
		`DELETE FROM risk_feedback WHERE tenant_id = $1`,
		`DELETE FROM risk_suppressions WHERE tenant_id = $1`,
		`DELETE FROM risk_scores WHERE tenant_id = $1`,
		`DELETE FROM risk_alerts WHERE tenant_id = $1`,
		`DELETE FROM risk_rules WHERE tenant_id = $1`,
		`DELETE FROM risk_signals WHERE tenant_id = $1`,
		`DELETE FROM stock_transfer_orders WHERE tenant_id = $1`,
		`DELETE FROM central_procurement_plan_lines WHERE tenant_id = $1`,
		`DELETE FROM central_procurement_plans WHERE tenant_id = $1`,
		`DELETE FROM central_price_rollouts WHERE tenant_id = $1`,
		`DELETE FROM station_daily_kpis WHERE tenant_id = $1`,
		`DELETE FROM enterprise_projection_state WHERE tenant_id = $1`,
		`DELETE FROM approval_decisions WHERE tenant_id = $1`,
		`DELETE FROM approval_requests WHERE tenant_id = $1`,
		`DELETE FROM approval_policies WHERE tenant_id = $1`,
		`DELETE FROM enterprise_scope_grants WHERE tenant_id = $1`,
		`DELETE FROM station_group_memberships WHERE tenant_id = $1`,
		`DELETE FROM station_groups WHERE tenant_id = $1`,
		`DELETE FROM accounting_exports WHERE tenant_id = $1`,
		`DELETE FROM petty_cash_reconciliations WHERE tenant_id = $1`,
		`DELETE FROM petty_cash_transactions WHERE tenant_id = $1`,
		`DELETE FROM petty_cash_floats WHERE tenant_id = $1`,
		`DELETE FROM expenses WHERE tenant_id = $1`,
		`DELETE FROM expense_categories WHERE tenant_id = $1`,
		`DELETE FROM customer_payment_allocations WHERE tenant_id = $1`,
		`DELETE FROM customer_payments WHERE tenant_id = $1`,
		`DELETE FROM customer_invoice_lines WHERE tenant_id = $1`,
		`DELETE FROM customer_invoices WHERE tenant_id = $1`,
		`DELETE FROM customer_credit_alerts WHERE tenant_id = $1`,
		`DELETE FROM customer_statements WHERE tenant_id = $1`,
		`DELETE FROM vehicle_odometer_readings WHERE tenant_id = $1`,
		`DELETE FROM fuel_authorization_denials WHERE tenant_id = $1`,
		`DELETE FROM fuel_authorizations WHERE tenant_id = $1`,
		`DELETE FROM fuel_limits WHERE tenant_id = $1`,
		`DELETE FROM fuel_credentials WHERE tenant_id = $1`,
		`DELETE FROM customer_drivers WHERE tenant_id = $1`,
		`DELETE FROM customer_vehicles WHERE tenant_id = $1`,
		`DELETE FROM customer_price_agreements WHERE tenant_id = $1`,
		`DELETE FROM customer_credit_profiles WHERE tenant_id = $1`,
		`DELETE FROM customer_contacts WHERE tenant_id = $1`,
		`DELETE FROM bank_statement_lines WHERE tenant_id = $1`,
		`DELETE FROM bank_statement_imports WHERE tenant_id = $1`,
		`DELETE FROM bank_deposit_lines WHERE tenant_id = $1`,
		`DELETE FROM bank_deposits WHERE tenant_id = $1`,
		`DELETE FROM bank_accounts WHERE tenant_id = $1`,
		`DELETE FROM cash_reconciliation_lines WHERE tenant_id = $1`,
		`DELETE FROM cash_reconciliations WHERE tenant_id = $1`,
		`DELETE FROM supplier_payment_allocations WHERE tenant_id = $1`,
		`DELETE FROM supplier_payments WHERE tenant_id = $1`,
		`DELETE FROM payables WHERE tenant_id = $1`,
		`DELETE FROM journal_lines WHERE tenant_id = $1`,
		`DELETE FROM journal_entries WHERE tenant_id = $1`,
		`DELETE FROM accounting_periods WHERE tenant_id = $1`,
		`DELETE FROM accounts WHERE tenant_id = $1`,
		`DELETE FROM revenue_days WHERE tenant_id = $1`,
		`DELETE FROM sale_voids WHERE tenant_id = $1`,
		`DELETE FROM sales WHERE tenant_id = $1`,
		`DELETE FROM payments WHERE tenant_id = $1`,
		`DELETE FROM ar_entries WHERE tenant_id = $1`,
		`DELETE FROM customers WHERE tenant_id = $1`,
		`DELETE FROM price_changes WHERE tenant_id = $1`,
		`DELETE FROM procurement_discrepancies WHERE tenant_id = $1`,
		`DELETE FROM supplier_invoice_lines WHERE tenant_id = $1`,
		`DELETE FROM supplier_invoices WHERE tenant_id = $1`,
		`DELETE FROM tank_reconciliations WHERE tenant_id = $1`,
		`DELETE FROM stock_movements WHERE tenant_id = $1`,
		`DELETE FROM deliveries WHERE tenant_id = $1`,
		`DELETE FROM purchase_order_lines WHERE tenant_id = $1`,
		`DELETE FROM purchase_orders WHERE tenant_id = $1`,
		`DELETE FROM supplier_products WHERE tenant_id = $1`,
		`DELETE FROM suppliers WHERE tenant_id = $1`,
		`DELETE FROM tank_dip_readings WHERE tenant_id = $1`,
		`DELETE FROM shift_close_lines WHERE tenant_id = $1`,
		`DELETE FROM shifts WHERE tenant_id = $1`,
		`DELETE FROM shift_team_members WHERE tenant_id = $1`,
		`DELETE FROM shift_teams WHERE tenant_id = $1`,
		`DELETE FROM employees WHERE tenant_id = $1`,
		`DELETE FROM operating_days WHERE tenant_id = $1`,
		`DELETE FROM nozzles WHERE tenant_id = $1`,
		`DELETE FROM pump_calibrations WHERE tenant_id = $1`,
		`DELETE FROM pumps WHERE tenant_id = $1`,
		`DELETE FROM tank_calibration_charts WHERE tenant_id = $1`,
		`DELETE FROM incidents WHERE tenant_id = $1`,
		`DELETE FROM tanks WHERE tenant_id = $1`,
		`DELETE FROM products WHERE tenant_id = $1`,
		`DELETE FROM user_station_access WHERE tenant_id = $1`,
		`DELETE FROM user_roles WHERE tenant_id = $1`,
		`DELETE FROM sessions WHERE tenant_id = $1`,
		`DELETE FROM users WHERE tenant_id = $1`,
		`DELETE FROM stations WHERE tenant_id = $1`,
		`DELETE FROM regions WHERE tenant_id = $1`,
		`DELETE FROM companies WHERE tenant_id = $1`,
		`DELETE FROM tenants WHERE id = $1`,
	}
	// journal_entries / journal_lines are append-only at the database (0065
	// immutability trigger). A test owns its tenant and may purge it, so acquire
	// one connection, flip the escape-hatch GUC on it for the session, and run
	// every delete on that connection. Non-journal tables ignore the GUC.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		for _, s := range stmts {
			_, _ = pool.Exec(ctx, s, tenantID)
		}
		return
	}
	defer conn.Release()
	_, _ = conn.Exec(ctx, `SET app.allow_ledger_delete = 'on'`)
	for _, s := range stmts {
		_, _ = conn.Exec(ctx, s, tenantID)
	}
}

// --- HTTP helpers ---

func (h *harness) login(t *testing.T, slug, email string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"tenant_slug": slug, "email": email, "password": testPassword})
	resp, err := http.Post(h.baseURL+"/api/v1/auth/login", "application/json", bytes.NewReader(body)) //nolint:noctx // test
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("login %s: status %d: %s", email, resp.StatusCode, raw)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if out.Token == "" {
		t.Fatalf("login %s: empty token", email)
	}
	return out.Token
}

func (h *harness) do(t *testing.T, method, path, token string, body io.Reader, contentType string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, h.baseURL+path, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func (h *harness) getJSON(t *testing.T, path, token string) (int, map[string]any) {
	t.Helper()
	code, raw := h.do(t, http.MethodGet, path, token, nil, "")
	var m map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	return code, m
}

func countOf(m map[string]any) int {
	if v, ok := m["count"].(float64); ok {
		return int(v)
	}
	return -1
}

func slug(h *harness) string {
	var s string
	_ = h.pool.QueryRow(context.Background(), `SELECT slug FROM tenants WHERE id = $1`, h.ids.tenantID).Scan(&s)
	return s
}

// --- Tests ---

func TestPhase2_ReadAuthorization(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	op := h.login(t, tenantSlug, h.ids.opEmail)

	// Admin (tenant-wide) sees all three tanks.
	if code, m := h.getJSON(t, "/api/v1/tanks", admin); code != 200 || countOf(m) != 3 {
		t.Fatalf("admin /tanks: code=%d count=%d (want 200/3)", code, countOf(m))
	}
	// Operator (scoped to station1) sees only its two tanks.
	if code, m := h.getJSON(t, "/api/v1/tanks", op); code != 200 || countOf(m) != 2 {
		t.Fatalf("operator /tanks: code=%d count=%d (want 200/2)", code, countOf(m))
	}
	// Operator can't fetch an out-of-scope tank by id.
	if code, _ := h.do(t, http.MethodGet, "/api/v1/tanks/"+h.ids.tankMSA.String(), op, nil, ""); code != http.StatusForbidden {
		t.Fatalf("operator GET MSA tank: code=%d (want 403)", code)
	}
	// Operator can't filter to an out-of-scope station.
	if code, _ := h.do(t, http.MethodGet, "/api/v1/tanks?station_id="+h.ids.station2.String(), op, nil, ""); code != http.StatusForbidden {
		t.Fatalf("operator /tanks?station=MSA: code=%d (want 403)", code)
	}
	// Products are tenant-wide; the operator still sees them.
	if code, m := h.getJSON(t, "/api/v1/products", op); code != 200 || countOf(m) != 2 {
		t.Fatalf("operator /products: code=%d count=%d (want 200/2)", code, countOf(m))
	}

	// ORG-01: the stations list is station-scoped too. Admin (tenant-wide)
	// sees both stations; the operator (scoped to station1) sees only one and
	// cannot filter to an out-of-scope station.
	if code, m := h.getJSON(t, "/api/v1/stations", admin); code != 200 || countOf(m) != 2 {
		t.Fatalf("admin /stations: code=%d count=%d (want 200/2)", code, countOf(m))
	}
	if code, m := h.getJSON(t, "/api/v1/stations", op); code != 200 || countOf(m) != 1 {
		t.Fatalf("operator /stations: code=%d count=%d (want 200/1)", code, countOf(m))
	}
	if code, _ := h.do(t, http.MethodGet, "/api/v1/stations?station_id="+h.ids.station2.String(), op, nil, ""); code != http.StatusForbidden {
		t.Fatalf("operator /stations?station=station2: code=%d (want 403)", code)
	}
}

// TestPhase2_MutatingRoutesGated covers AUTH-21: mutating routes carry an
// explicit permission gate, so a principal lacking the permission is refused
// at the route (not merely by in-handler logic). An attendant holds neither
// tanks.manage, pumps.manage, incidents.manage, nor purchase_order.manage,
// even with a station grant, so every such write is 403.
func TestPhase2_MutatingRoutesGated(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)

	// Attendant (minimal role) WITH a station grant on station1 — proves the
	// 403 is the missing manage-permission, not missing station scope.
	hash, err := password.New(password.DefaultParams, "").Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	email := fmt.Sprintf("att-gate-%d@it.local", time.Now().UnixNano())
	var uid uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'Gate Attendant', 'active', $3, now()) RETURNING id`,
		h.ids.tenantID, email, hash).Scan(&uid); err != nil {
		t.Fatalf("seed attendant: %v", err)
	}
	grantRole(t, ctx, h.pool, h.ids.tenantID, uid, "attendant")
	if _, err := h.pool.Exec(ctx,
		`INSERT INTO user_station_access (user_id, station_id, tenant_id) VALUES ($1, $2, $3)`,
		uid, h.ids.station1, h.ids.tenantID); err != nil {
		t.Fatalf("station access: %v", err)
	}
	tok := h.login(t, tenantSlug, email)

	st := h.ids.station1.String()
	cases := []struct{ path, body string }{
		{"/api/v1/tanks", `{"station_id":"` + st + `","product_id":"` + h.ids.pmsProduct.String() + `","name":"X","code":"X9","capacity_litres":1000}`},
		{"/api/v1/pumps", `{"station_id":"` + st + `","number":9,"name":"P9"}`},
		{"/api/v1/incidents", `{"station_id":"` + st + `","category":"other","summary":"x"}`},
		{"/api/v1/purchase-orders", `{"station_id":"` + st + `"}`},
	}
	for _, c := range cases {
		if code, _ := h.postJSON(t, c.path, tok, c.body); code != http.StatusForbidden {
			t.Fatalf("attendant POST %s: code=%d (want 403)", c.path, code)
		}
	}
}

// TestPhase2_TenantWideIsExplicit covers AUTH-20: tenant-wide reach is a role
// property, not the absence of station grants. A user holding a station-scoped
// role with no user_station_access rows must get no station-scoped access
// (default-deny), not silent tenant-wide reach.
func TestPhase2_TenantWideIsExplicit(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)

	// A station_manager (not a tenant-wide role) with zero station grants.
	hash, err := password.New(password.DefaultParams, "").Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	email := fmt.Sprintf("noscope-%d@it.local", time.Now().UnixNano())
	var uid uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'No Scope', 'active', $3, now()) RETURNING id`,
		h.ids.tenantID, email, hash).Scan(&uid); err != nil {
		t.Fatalf("seed no-scope user: %v", err)
	}
	grantRole(t, ctx, h.pool, h.ids.tenantID, uid, "station_manager")
	tok := h.login(t, tenantSlug, email)

	// No station grants + non-tenant-wide role ⇒ scoped list is forbidden,
	// not a silent tenant-wide listing.
	if code, _ := h.do(t, http.MethodGet, "/api/v1/stations", tok, nil, ""); code != http.StatusForbidden {
		t.Fatalf("no-scope /stations: code=%d (want 403)", code)
	}
	if code, _ := h.do(t, http.MethodGet, "/api/v1/tanks", tok, nil, ""); code != http.StatusForbidden {
		t.Fatalf("no-scope /tanks: code=%d (want 403)", code)
	}
}

func TestPhase2_NozzleProductInvariantDBEnforced(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	// Raw insert that lies: a nozzle on the PMS tank but claiming the AGO
	// product. The composite FK to tanks(tenant,id,station,product) must
	// reject it regardless of the app layer.
	_, err := h.pool.Exec(ctx, `
		INSERT INTO nozzles (tenant_id, station_id, pump_id, tank_id, product_id, number, default_price)
		VALUES ($1, $2, $3, $4, $5, 9, 0)`,
		h.ids.tenantID, h.ids.station1, h.ids.pump1, h.ids.tankPMS, h.ids.agoProduct)
	if err == nil {
		t.Fatal("expected FK violation inserting product-mismatched nozzle, got nil")
	}
}

func TestPhase2_CalibrationUploadLookupSupersede(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	tankPath := "/api/v1/tanks/" + h.ids.tankPMS.String()

	csv := "dip_mm,volume_litres\n0,0\n1200,12000\n1260,12600\n3000,30000\n"

	// Dry run must not persist.
	code, _ := h.uploadCSV(t, tankPath+"/calibration-charts?dry_run=true", admin, "preview", csv)
	if code != 200 {
		t.Fatalf("dry-run upload: code=%d (want 200)", code)
	}
	if code, m := h.getJSON(t, tankPath+"/calibration-charts", admin); countOf(m) != 0 {
		t.Fatalf("after dry-run charts count=%d code=%d (want 0)", countOf(m), code)
	}

	// Real upload activates a chart.
	if code, _ := h.uploadCSV(t, tankPath+"/calibration-charts", admin, "Initial", csv); code != 201 {
		t.Fatalf("upload: code=%d (want 201)", code)
	}

	// Interpolation: 1240 sits 2/3 between 12000 and 12600 -> 12400.
	code, m := h.getJSON(t, tankPath+"/calibrated-volume?dip_mm=1240", admin)
	if code != 200 {
		t.Fatalf("calibrated-volume: code=%d (want 200)", code)
	}
	if v, _ := m["volume_litres"].(float64); v != 12400 {
		t.Fatalf("calibrated-volume dip 1240 = %v (want 12400)", m["volume_litres"])
	}
	// Out of range refuses to extrapolate.
	if code, _ := h.do(t, http.MethodGet, tankPath+"/calibrated-volume?dip_mm=4000", admin, nil, ""); code != http.StatusUnprocessableEntity {
		t.Fatalf("calibrated-volume out of range: code=%d (want 422)", code)
	}
	// Invalid CSV rejected (no partial commit).
	if code, _ := h.uploadCSV(t, tankPath+"/calibration-charts", admin, "bad", "dip_mm,volume_litres\n0,0\n50,500\n40,400\n"); code != 400 {
		t.Fatalf("invalid CSV upload: code=%d (want 400)", code)
	}

	// Replacing supersedes the prior active chart and keeps it as history.
	if code, _ := h.uploadCSV(t, tankPath+"/calibration-charts", admin, "Re-strap", csv); code != 201 {
		t.Fatalf("replace upload: code=%d (want 201)", code)
	}
	ctx := context.Background()
	var active, superseded int
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM tank_calibration_charts WHERE tank_id=$1 AND status='active'`, h.ids.tankPMS).Scan(&active)
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM tank_calibration_charts WHERE tank_id=$1 AND status='superseded'`, h.ids.tankPMS).Scan(&superseded)
	if active != 1 || superseded != 1 {
		t.Fatalf("after replace: active=%d superseded=%d (want 1/1)", active, superseded)
	}
}

func TestPhase2_AuditOutboxAtomic(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	body := `{"code":"LPG","name":"Liquefied Petroleum Gas","category":"gas","color":"#14b8a6"}`
	code, raw := h.do(t, http.MethodPost, "/api/v1/products", admin, bytes.NewReader([]byte(body)), "application/json")
	if code != 201 {
		t.Fatalf("create product: code=%d body=%s (want 201)", code, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &created)

	ctx := context.Background()
	var auditN, outboxN int
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE tenant_id=$1 AND action='product.created' AND entity_id=$2`, h.ids.tenantID, created.ID).Scan(&auditN)
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE tenant_id=$1 AND event_type='ProductCreated' AND aggregate_id=$2`, h.ids.tenantID, created.ID).Scan(&outboxN)
	if auditN != 1 || outboxN != 1 {
		t.Fatalf("create product side effects: audit=%d outbox=%d (want 1/1)", auditN, outboxN)
	}
}

func TestPhase2_SoftDeleteGuards(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	// PMS product is bound to tanks -> 409.
	if code, _ := h.do(t, http.MethodDelete, "/api/v1/products/"+h.ids.pmsProduct.String(), admin, nil, ""); code != http.StatusConflict {
		t.Fatalf("delete in-use product: code=%d (want 409)", code)
	}
	// PMS tank feeds a nozzle -> 409.
	if code, _ := h.do(t, http.MethodDelete, "/api/v1/tanks/"+h.ids.tankPMS.String(), admin, nil, ""); code != http.StatusConflict {
		t.Fatalf("delete tank with nozzle: code=%d (want 409)", code)
	}
	// AGO tank has no nozzles -> 204.
	if code, _ := h.do(t, http.MethodDelete, "/api/v1/tanks/"+h.ids.tankAGO.String(), admin, nil, ""); code != http.StatusNoContent {
		t.Fatalf("delete unused tank: code=%d (want 204)", code)
	}
}

func TestPhase2_StatusTransitions(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	statusPath := "/api/v1/pumps/" + h.ids.pump1.String() + "/status"

	patch := func(b string) int {
		code, _ := h.do(t, http.MethodPatch, statusPath, admin, bytes.NewReader([]byte(b)), "application/json")
		return code
	}
	if c := patch(`{"status":"maintenance"}`); c != 400 {
		t.Fatalf("maintenance without reason: %d (want 400)", c)
	}
	if c := patch(`{"status":"maintenance","reason":"service"}`); c != 200 {
		t.Fatalf("maintenance with reason: %d (want 200)", c)
	}
	if c := patch(`{"status":"decommissioned","reason":"retired"}`); c != 200 {
		t.Fatalf("decommission: %d (want 200)", c)
	}
	if c := patch(`{"status":"active","reason":"oops"}`); c != 409 {
		t.Fatalf("revive decommissioned: %d (want 409)", c)
	}
}

func (h *harness) uploadCSV(t *testing.T, path, token, name, csv string) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("name", name)
	fw, err := mw.CreateFormFile("file", "chart.csv")
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	_, _ = fw.Write([]byte(csv))
	_ = mw.Close()
	return h.do(t, http.MethodPost, path, token, &buf, mw.FormDataContentType())
}

// TestPhase2_MfaSecretEncryptedAtRest proves AUTH-13: the TOTP/MFA seed is
// encrypted at rest. Enrolling stores versioned ciphertext (never the plaintext
// base32 seed), so a database-only compromise cannot recover the seed and mint
// valid codes — and the encrypted seed still verifies end-to-end, proving the
// decrypt path is wired into both enrollment activation and login.
func TestPhase2_MfaSecretEncryptedAtRest(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	// Enroll: the response carries the plaintext base32 seed for the QR code.
	code, raw := h.do(t, http.MethodPost, "/api/v1/auth/mfa/enroll", admin, nil, "")
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

	// The stored column must be versioned ciphertext, not the plaintext seed.
	var stored string
	if err := h.pool.QueryRow(ctx, `SELECT mfa_secret FROM users WHERE id = $1`, adminID).Scan(&stored); err != nil {
		t.Fatalf("read mfa_secret: %v", err)
	}
	if stored == enr.Secret {
		t.Fatal("mfa_secret is stored in plaintext (AUTH-13 regression)")
	}
	if !strings.HasPrefix(stored, "v1:") {
		t.Fatalf("mfa_secret is not versioned ciphertext: %q", stored)
	}

	// The encrypted seed still verifies: a code generated from the plaintext
	// seed activates MFA, which exercises the decrypt-then-verify path.
	otp, err := totp.GenerateCode(enr.Secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	body := bytes.NewReader([]byte(fmt.Sprintf(`{"code":%q}`, otp)))
	if code, raw := h.do(t, http.MethodPost, "/api/v1/auth/mfa/verify", admin, body, "application/json"); code != http.StatusNoContent {
		t.Fatalf("verify: %d %s", code, raw)
	}
	var enabled bool
	if err := h.pool.QueryRow(ctx, `SELECT mfa_enabled FROM users WHERE id = $1`, adminID).Scan(&enabled); err != nil || !enabled {
		t.Fatalf("mfa_enabled = %v (err %v)", enabled, err)
	}
}
