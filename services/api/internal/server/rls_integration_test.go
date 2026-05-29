package server_test

// DB-backed proof that Postgres row-level security actually isolates tenants
// when the API connects as the non-owner `fuelgrid_app` role (audit INFRA-01 /
// AUTH-25 / TEST-04: RLS policies exist on 51 tables but were never exercised
// from Go — the prior harness connects as the owner, which bypasses RLS).
//
// This test connects a SECOND pool as fuelgrid_app (RLS enforced) and verifies:
//   - a tenant-scoped connection sees only its own tenant's rows,
//   - an UNSCOPED connection (no app.current_tenant) sees nothing (fail-closed),
//   - WITH CHECK rejects inserting a row for a different tenant.
// Gated on TEST_DATABASE_URL like the other integration tests.

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// appRoleURL rewrites the owner DSN to connect as the non-owner fuelgrid_app
// role (the role + a default password are created by migration 0005).
func appRoleURL(ownerURL string) (string, error) {
	u, err := url.Parse(ownerURL)
	if err != nil {
		return "", err
	}
	u.User = url.UserPassword("fuelgrid_app", "fuelgrid_app")
	return u.String(), nil
}

// scopedCompanyCount counts how many of the given company ids are visible over
// a fuelgrid_app connection scoped (or not, when tenant=="") to a tenant.
func scopedCompanyCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant string, ids []uuid.UUID) int {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if tenant != "" {
		// SET LOCAL takes no bind params; the value is a fixed-format UUID.
		if _, err := tx.Exec(ctx, "SET LOCAL app.current_tenant = '"+tenant+"'"); err != nil {
			t.Fatalf("set tenant: %v", err)
		}
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM companies WHERE id = ANY($1)`, ids).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestRLS_TenantIsolation(t *testing.T) {
	ownerURL := os.Getenv("TEST_DATABASE_URL")
	if ownerURL == "" {
		t.Skip("set TEST_DATABASE_URL to run the RLS isolation test")
	}
	ctx := context.Background()

	owner, err := pgxpool.New(ctx, ownerURL)
	if err != nil {
		t.Fatalf("owner pool: %v", err)
	}
	defer owner.Close()

	// Guarantee the fuelgrid_app login password matches what we connect with
	// (the role exists from migration 0005; this is idempotent and owner-only).
	if _, err := owner.Exec(ctx, `ALTER ROLE fuelgrid_app WITH LOGIN PASSWORD 'fuelgrid_app'`); err != nil {
		t.Fatalf("ensure fuelgrid_app password: %v", err)
	}

	appURL, err := appRoleURL(ownerURL)
	if err != nil {
		t.Fatalf("app url: %v", err)
	}
	app, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatalf("app pool (fuelgrid_app): %v", err)
	}
	defer app.Close()

	// Prove the app role is NOT an RLS-bypassing superuser.
	var isSuper bool
	if err := app.QueryRow(ctx, `SELECT rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&isSuper); err != nil {
		t.Fatalf("role check: %v", err)
	}
	if isSuper {
		t.Fatal("fuelgrid_app must not be a superuser, or RLS would be bypassed")
	}

	// Seed two tenants + one company each, via the owner (bypasses RLS).
	tenantA, tenantB := uuid.New(), uuid.New()
	if _, err := owner.Exec(ctx, `
		INSERT INTO tenants (id, name, slug) VALUES ($1, 'RLS A', $2), ($3, 'RLS B', $4)
	`, tenantA, "rls-a-"+tenantA.String()[:8], tenantB, "rls-b-"+tenantB.String()[:8]); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}
	t.Cleanup(func() {
		_, _ = owner.Exec(ctx, `DELETE FROM companies WHERE tenant_id IN ($1, $2)`, tenantA, tenantB)
		_, _ = owner.Exec(ctx, `DELETE FROM tenants WHERE id IN ($1, $2)`, tenantA, tenantB)
	})

	newCompany := func(tenant uuid.UUID, name string) uuid.UUID {
		var id uuid.UUID
		if err := owner.QueryRow(ctx, `
			INSERT INTO companies (tenant_id, name, legal_name, currency, timezone)
			VALUES ($1, $2, $2, 'USD', 'UTC') RETURNING id
		`, tenant, name).Scan(&id); err != nil {
			t.Fatalf("seed company: %v", err)
		}
		return id
	}
	compA := newCompany(tenantA, "Co A")
	compB := newCompany(tenantB, "Co B")
	both := []uuid.UUID{compA, compB}

	// Scoped to tenant A: sees exactly A's company, never B's.
	if got := scopedCompanyCount(t, ctx, app, tenantA.String(), both); got != 1 {
		t.Fatalf("tenant A scope sees %d of the 2 companies, want 1 (only its own)", got)
	}
	if got := scopedCompanyCount(t, ctx, app, tenantA.String(), []uuid.UUID{compB}); got != 0 {
		t.Fatal("tenant A can see tenant B's company — RLS is NOT isolating")
	}
	// Scoped to tenant B: the mirror.
	if got := scopedCompanyCount(t, ctx, app, tenantB.String(), both); got != 1 {
		t.Fatalf("tenant B scope sees %d, want 1", got)
	}
	// Unscoped (no app.current_tenant): fail-closed, sees nothing.
	if got := scopedCompanyCount(t, ctx, app, "", both); got != 0 {
		t.Fatalf("unscoped connection sees %d companies, want 0 (RLS must fail closed)", got)
	}

	// WITH CHECK: scoped to tenant A, inserting a row for tenant B is rejected.
	tx, err := app.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL app.current_tenant = '"+tenantA.String()+"'"); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO companies (tenant_id, name, legal_name, currency, timezone)
		VALUES ($1, 'Cross', 'Cross', 'USD', 'UTC')
	`, tenantB); err == nil {
		t.Fatal("WITH CHECK did not reject inserting a company for another tenant")
	}
}

// TestRLS_PoolWrapperScoping exercises the PRODUCTION mechanism the request
// middleware relies on: database.Connect (the *database.Pool wrapper) +
// AcquireTenant (sets app.current_tenant on a checked-out connection) + the
// wrapper's Query/Begin preferring that connection. It proves a scoped wrapper
// is tenant-isolated and an UNSCOPED wrapper call (as fuelgrid_app, no GUC)
// fails closed — the exact behaviour requireAuth produces when RLS is enabled.
func TestRLS_PoolWrapperScoping(t *testing.T) {
	ownerURL := os.Getenv("TEST_DATABASE_URL")
	if ownerURL == "" {
		t.Skip("set TEST_DATABASE_URL to run the RLS pool-wrapper test")
	}
	ctx := context.Background()

	owner, err := database.Connect(ctx, database.Config{URL: ownerURL})
	if err != nil {
		t.Fatalf("owner pool: %v", err)
	}
	defer owner.Close()
	if _, err := owner.Exec(ctx, `ALTER ROLE fuelgrid_app WITH LOGIN PASSWORD 'fuelgrid_app'`); err != nil {
		t.Fatalf("ensure fuelgrid_app password: %v", err)
	}

	appURL, err := appRoleURL(ownerURL)
	if err != nil {
		t.Fatalf("app url: %v", err)
	}
	app, err := database.Connect(ctx, database.Config{URL: appURL})
	if err != nil {
		t.Fatalf("app pool: %v", err)
	}
	defer app.Close()

	tenantA, tenantB := uuid.New(), uuid.New()
	if _, err := owner.Exec(ctx, `
		INSERT INTO tenants (id, name, slug) VALUES ($1, 'RLS WA', $2), ($3, 'RLS WB', $4)
	`, tenantA, "rls-wa-"+tenantA.String()[:8], tenantB, "rls-wb-"+tenantB.String()[:8]); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}
	t.Cleanup(func() {
		_, _ = owner.Exec(ctx, `DELETE FROM companies WHERE tenant_id IN ($1, $2)`, tenantA, tenantB)
		_, _ = owner.Exec(ctx, `DELETE FROM tenants WHERE id IN ($1, $2)`, tenantA, tenantB)
	})
	var compA, compB uuid.UUID
	if err := owner.QueryRow(ctx, `INSERT INTO companies (tenant_id, name, legal_name, currency, timezone) VALUES ($1,'WA','WA','USD','UTC') RETURNING id`, tenantA).Scan(&compA); err != nil {
		t.Fatalf("seed company A: %v", err)
	}
	if err := owner.QueryRow(ctx, `INSERT INTO companies (tenant_id, name, legal_name, currency, timezone) VALUES ($1,'WB','WB','USD','UTC') RETURNING id`, tenantB).Scan(&compB); err != nil {
		t.Fatalf("seed company B: %v", err)
	}
	both := []uuid.UUID{compA, compB}

	// Scoped to tenant A via the production AcquireTenant + wrapper Query.
	scopedCtx, release, err := app.AcquireTenant(ctx, tenantA)
	if err != nil {
		t.Fatalf("AcquireTenant: %v", err)
	}
	var n int
	if err := app.QueryRow(scopedCtx, `SELECT count(*) FROM companies WHERE id = ANY($1)`, both).Scan(&n); err != nil {
		release()
		t.Fatalf("scoped query: %v", err)
	}
	if n != 1 {
		release()
		t.Fatalf("scoped wrapper sees %d of 2 companies, want 1 (only tenant A)", n)
	}
	// A transaction begun via the wrapper on the scoped ctx inherits the GUC.
	tx, err := app.Begin(scopedCtx)
	if err != nil {
		release()
		t.Fatalf("scoped begin: %v", err)
	}
	var nTx int
	_ = tx.QueryRow(scopedCtx, `SELECT count(*) FROM companies WHERE id = ANY($1)`, both).Scan(&nTx)
	_ = tx.Rollback(scopedCtx)
	if nTx != 1 {
		release()
		t.Fatalf("scoped tx sees %d, want 1", nTx)
	}
	release()

	// Unscoped wrapper call as fuelgrid_app (no app.current_tenant) -> fail-closed.
	var u int
	if err := app.QueryRow(ctx, `SELECT count(*) FROM companies WHERE id = ANY($1)`, both).Scan(&u); err != nil {
		t.Fatalf("unscoped query: %v", err)
	}
	if u != 0 {
		t.Fatalf("unscoped wrapper sees %d companies, want 0 (RLS must fail closed)", u)
	}
}

