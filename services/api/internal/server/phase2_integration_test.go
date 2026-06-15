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
	"sort"
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

	// Report Insight Rules Engine (Reports Center Phase 15): seed the system rules
	// for this freshly-created tenant, mirroring the 0115 migration + cmd/seed (the
	// migration's CROSS JOIN tenants only covers tenants that existed when it ran, so
	// a harness tenant created live needs the same per-tenant provisioning). Seeded
	// as shadow so report output stays byte-identical to the composer by default.
	if _, err := pool.Exec(ctx, `
		INSERT INTO report_rules
		    (tenant_id, code, name, report_key, category, condition, threshold,
		     threshold_config, comparison_period_days, severity, message_template,
		     recommended_action, report_placement, mode, is_system, enabled, status)
		VALUES
		    ($1,'gross_swing','Gross revenue swing','sales','sales','period_over_period',25,'{"metric":"Gross revenue","warn_pct":25}',NULL,'warning','{metric} moved {direction} {pct}% vs the prior period.','Confirm the day''s transactions.','insight','shadow',true,true,'active'),
		    ($1,'gross_variance','Gross vs recent average','sales','sales','variance_vs_average',20,'{"metric":"Gross revenue","warn_pct":20}',30,'warning','{metric} is {pct}% vs its recent average.','Confirm the underlying transactions.','insight','shadow',true,true,'active'),
		    ($1,'cash_variance','Cash variance over tolerance','cash-reconciliation','cash','cash_variance_over_tolerance',NULL,'{"critical_multiple":2}',NULL,'warning','Cash drawer is off by {variance} — beyond tolerance.','Reconcile the drawer.','insight','shadow',true,true,'active'),
		    ($1,'tank_over_tolerance','Tank variance over tolerance','inventory-reconciliation','inventory','tank_over_tolerance',NULL,'{}',NULL,'warning','{count} tank(s) exceeded their variance tolerance.','Investigate possible loss.','insight','shadow',true,true,'active'),
		    ($1,'margin_health','Margin health','sales','sales','margin_health',15,'{"contract_pct":15}',NULL,'critical','Latest margin is negative — sales are running below cost.','Review pump pricing and COGS.','insight','shadow',true,true,'active'),
		    ($1,'overdue_receivables','Overdue receivables share','customer-credit','credit','overdue_share',50,'{"critical_pct":50}',NULL,'warning','{overdue} of receivables is overdue ({pct}% of outstanding).','Chase the overdue balances.','insight','shadow',true,true,'active'),
		    ($1,'delivery_shortfall','Delivery shortfall','delivery','procurement','delivery_shortfall',5,'{"warn_pct":5}',NULL,'warning','Received {shortfall} L less than ordered this period ({pct}% of the ordered volume).','Reconcile short deliveries.','insight','shadow',true,true,'active'),
		    ($1,'period_unlocked','Period not locked',NULL,'general','period_unlocked',NULL,'{}',NULL,'info','This period is not locked yet, so its totals are provisional.',NULL,'data_quality','shadow',true,true,'active')
		ON CONFLICT (tenant_id, code) DO NOTHING
	`, ids.tenantID); err != nil {
		t.Fatalf("seed report rules: %v", err)
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

// cleanupTenant purges every row a test seeded for tenantID, child-before-parent,
// so the next run starts from a clean slate and a dirty local dev DB does not
// accumulate orphan tenants/tanks/movements (the bug class behind the
// opening-stock phantom-500 investigation).
//
// The delete order is DERIVED FROM THE LIVE SCHEMA at runtime rather than
// hand-maintained: we enumerate every tenant-scoped table (one with a tenant_id
// column) from information_schema, read the foreign-key edges among them from
// pg_constraint, topologically sort, and delete leaves (the referencing child)
// before the tables they reference. A future migration that adds a tenant-scoped
// table is therefore torn down automatically — the teardown can never rot. The
// guard test TestCleanupTenant_LeavesNoResidual asserts this stays true.
//
// Every delete is scoped `WHERE tenant_id = $1` so shared/system rows (e.g.
// system roles, which carry tenant_id IS NULL) are never touched. tenants itself
// has no tenant_id column and is removed last, by id.
//
// journal_entries / journal_lines are append-only at the database (the 0065
// immutability trigger). A test owns its tenant and may purge it, so we acquire
// one connection, flip the app.allow_ledger_delete escape-hatch GUC on it for the
// session, and run every delete on that connection. Non-ledger tables ignore the
// GUC. Errors are swallowed per-statement (best effort): the guard test is the
// backstop that fails the moment a real leak survives.
func cleanupTenant(ctx context.Context, pool *database.Pool, tenantID uuid.UUID) {
	stmts := tenantDeleteStatements(ctx, pool)
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

// tenantScopedTables returns every base table in the public schema that carries
// a tenant_id column — i.e. the set of tables a single tenant owns rows in.
func tenantScopedTables(ctx context.Context, pool *database.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.table_name
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = 'public'
		  AND c.column_name = 'tenant_id'
		  AND t.table_type = 'BASE TABLE'
		ORDER BY c.table_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// tenantFKEdges returns the foreign-key edges among tenant-scoped tables as
// child -> parent pairs (the child holds the FK, the parent is referenced).
// Self-references are excluded: a single `DELETE FROM t WHERE tenant_id = $1`
// removes all of a table's rows at once, so intra-table ordering is irrelevant.
func tenantFKEdges(ctx context.Context, pool *database.Pool) (map[string][]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT con.conrelid::regclass::text AS child,
		                con.confrelid::regclass::text AS parent
		FROM pg_constraint con
		JOIN pg_namespace n ON n.oid = con.connamespace
		WHERE con.contype = 'f'
		  AND n.nspname = 'public'
		  AND con.conrelid <> con.confrelid
		  AND con.conrelid::regclass::text IN (
		      SELECT table_name FROM information_schema.columns
		      WHERE table_schema = 'public' AND column_name = 'tenant_id')
		  AND con.confrelid::regclass::text IN (
		      SELECT table_name FROM information_schema.columns
		      WHERE table_schema = 'public' AND column_name = 'tenant_id')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	edges := map[string][]string{}
	for rows.Next() {
		var child, parent string
		if err := rows.Scan(&child, &parent); err != nil {
			return nil, err
		}
		edges[child] = append(edges[child], parent)
	}
	return edges, rows.Err()
}

// tenantDeleteStatements builds the ordered list of DELETE statements that purge
// a tenant, children before parents, from the live schema. The order is a
// topological sort of the tenant-scoped FK graph: a table is emitted only after
// every table that references it has been emitted (leaves first). The final
// statement removes the tenant row itself by id. If the schema cannot be read
// (e.g. the pool is gone mid-teardown) it falls back to deleting tenants alone.
func tenantDeleteStatements(ctx context.Context, pool *database.Pool) []string {
	tables, err := tenantScopedTables(ctx, pool)
	if err != nil || len(tables) == 0 {
		return []string{`DELETE FROM tenants WHERE id = $1`}
	}
	edges, err := tenantFKEdges(ctx, pool)
	if err != nil {
		return []string{`DELETE FROM tenants WHERE id = $1`}
	}
	ordered := topoDeleteOrder(tables, edges)

	stmts := make([]string, 0, len(ordered)+1)
	for _, tbl := range ordered {
		stmts = append(stmts, fmt.Sprintf(`DELETE FROM %s WHERE tenant_id = $1`, tbl))
	}
	stmts = append(stmts, `DELETE FROM tenants WHERE id = $1`)
	return stmts
}

// topoDeleteOrder returns tables ordered so each table appears before any table
// it references via a foreign key — i.e. referencing children are deleted before
// the parents they point at. edges maps child -> parents. The graph among
// distinct tenant-scoped tables is a DAG (verified: no 2-cycles), so a stable
// Kahn-style sort always terminates; any table caught in an unexpected cycle is
// appended at the end so it is still attempted.
func topoDeleteOrder(tables []string, edges map[string][]string) []string {
	// childrenOf[parent] = tables that reference parent. We want every child
	// emitted before its parent, so we emit parents only once all their children
	// are done — process the reverse-dependency graph leaves-first.
	childrenOf := map[string]map[string]bool{}
	remainingChildren := map[string]int{} // unemitted children referencing this table
	present := map[string]bool{}
	for _, t := range tables {
		present[t] = true
	}
	for child, parents := range edges {
		if !present[child] {
			continue
		}
		for _, parent := range parents {
			if !present[parent] || parent == child {
				continue
			}
			if childrenOf[parent] == nil {
				childrenOf[parent] = map[string]bool{}
			}
			if !childrenOf[parent][child] {
				childrenOf[parent][child] = true
				remainingChildren[parent]++
			}
		}
	}

	// Deterministic order: process tables in name order, emitting a table only
	// once it has no remaining unemitted children referencing it.
	sorted := append([]string(nil), tables...)
	sort.Strings(sorted)

	emitted := map[string]bool{}
	out := make([]string, 0, len(tables))
	for progress := true; progress; {
		progress = false
		for _, t := range sorted {
			if emitted[t] || remainingChildren[t] > 0 {
				continue
			}
			emitted[t] = true
			out = append(out, t)
			progress = true
			for parent := range childrenOf {
				if childrenOf[parent][t] {
					delete(childrenOf[parent], t)
					remainingChildren[parent]--
				}
			}
		}
	}
	// Any table left (a cycle we did not anticipate) is appended so it is still
	// attempted; the guard test would catch a resulting leak.
	for _, t := range sorted {
		if !emitted[t] {
			out = append(out, t)
		}
	}
	return out
}

// residualTenantRows returns, for the given tenant, the per-table count of rows
// that survive cleanupTenant. A clean teardown yields an empty map. It iterates
// the SAME runtime-enumerated tenant-scoped table set the teardown uses, so it
// can never silently miss a newly added table. Counts are scoped
// `WHERE tenant_id = $1`, so shared/system rows (tenant_id IS NULL) are ignored.
func residualTenantRows(ctx context.Context, pool *database.Pool, tenantID uuid.UUID) (map[string]int, error) {
	tables, err := tenantScopedTables(ctx, pool)
	if err != nil {
		return nil, err
	}
	residual := map[string]int{}
	for _, tbl := range tables {
		var n int
		if err := pool.QueryRow(ctx,
			fmt.Sprintf(`SELECT count(*) FROM %s WHERE tenant_id = $1`, tbl), tenantID).Scan(&n); err != nil {
			return nil, fmt.Errorf("count %s: %w", tbl, err)
		}
		if n > 0 {
			residual[tbl] = n
		}
	}
	return residual, nil
}

// cleanupTenantNoResidual purges tenantID via the generic cleanupTenant and then
// asserts — with the pool still open — that ZERO rows survive in any
// tenant-scoped table AND that the tenant row itself is gone. It is the same
// guard TestCleanupTenant_LeavesNoResidual applies, packaged so the OTHER
// teardown paths (the enterprise foreign tenant, the rls_blast tenants) can be
// routed through cleanupTenant and provably leave nothing behind. Register it as
// a defer AFTER the harness `defer cleanup()` so it runs (LIFO) while the pool is
// still open — never via t.Cleanup, which fires after cleanup() has closed the
// pool and would make both the purge and this check silent no-ops.
func cleanupTenantNoResidual(t *testing.T, ctx context.Context, pool *database.Pool, tenantID uuid.UUID) {
	t.Helper()
	cleanupTenant(ctx, pool, tenantID)
	residual, err := residualTenantRows(ctx, pool, tenantID)
	if err != nil {
		t.Errorf("residual scan for tenant %s: %v", tenantID, err)
		return
	}
	if len(residual) > 0 {
		total := 0
		for _, n := range residual {
			total += n
		}
		t.Errorf("cleanupTenant left %d residual rows across %d tables for tenant %s: %v "+
			"(a tenant-scoped table is not being torn down)", total, len(residual), tenantID, residual)
	}
	var tenantRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tenants WHERE id = $1`, tenantID).Scan(&tenantRows); err != nil {
		t.Errorf("count tenant row %s: %v", tenantID, err)
		return
	}
	if tenantRows != 0 {
		t.Errorf("tenant row %s survived cleanupTenant: %d rows", tenantID, tenantRows)
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

func TestPhase2_NozzleInitialMeterSeedAndAdjust(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	createBody := fmt.Sprintf(
		`{"pump_id":%q,"tank_id":%q,"number":2,"meter_decimal_places":2,"initial_meter_reading":"1000.25","initial_meter_note":"physical install"}`,
		h.ids.pump1.String(), h.ids.tankAGO.String(),
	)
	code, created := h.postJSON(t, "/api/v1/nozzles", admin, createBody)
	if code != http.StatusCreated {
		t.Fatalf("create nozzle with initial meter: code=%d body=%v", code, created)
	}
	if got := created["initial_meter_reading"]; got != "1000.25" {
		t.Fatalf("created initial_meter_reading=%v (want 1000.25)", got)
	}
	if created["initial_meter_recorded_at"] == nil || created["initial_meter_recorded_by"] == nil {
		t.Fatalf("created nozzle missing initial-meter metadata: %v", created)
	}

	var nozzleID uuid.UUID
	if err := h.pool.QueryRow(context.Background(), `
		SELECT id FROM nozzles
		WHERE tenant_id = $1 AND pump_id = $2 AND number = 1 AND status <> 'deleted'
	`, h.ids.tenantID, h.ids.pump1).Scan(&nozzleID); err != nil {
		t.Fatalf("lookup seeded nozzle: %v", err)
	}

	path := "/api/v1/nozzles/" + nozzleID.String() + "/initial-meter"
	if code, body := h.postJSON(t, path, admin, `{"reading":"12345.67","note":"meter serviced"}`); code != http.StatusOK || body["initial_meter_reading"] != "12345.67" {
		t.Fatalf("seed existing nozzle initial meter: code=%d body=%v", code, body)
	}
	if code, _ := h.postJSON(t, path, admin, `{"reading":"12345.678","note":"too many decimals"}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("precision reject: code=%d (want 422)", code)
	}
	if code, body := h.postJSON(t, path, admin, `{"reading":"12346.00","note":"final calibration"}`); code != http.StatusOK || body["initial_meter_reading"] != "12346" {
		t.Fatalf("adjust existing nozzle initial meter: code=%d body=%v", code, body)
	}

	var seeded, corrected int
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE tenant_id=$1 AND action='nozzle.initial_meter_seeded'`, h.ids.tenantID).Scan(&seeded)
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE tenant_id=$1 AND action='nozzle.initial_meter_corrected'`, h.ids.tenantID).Scan(&corrected)
	if seeded != 1 || corrected != 1 {
		t.Fatalf("initial meter audit counts seeded=%d corrected=%d (want 1/1)", seeded, corrected)
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
