package server_test

// DB-backed RLS coverage drift-detector (MT-4 / INFRA-01 / AUTH-25).
//
// Migration 0074 dynamically ENABLEd the standard tenant_isolation policy on
// every public table carrying a NOT NULL tenant_id that still lacked RLS. That
// closed the historical gap — but a *future* migration could add a new
// tenant-owned table and forget the policy, silently re-opening cross-tenant
// exposure once the API runs as the non-owner fuelgrid_app role. RLS bugs are
// invisible in owner-pool tests (the owner bypasses RLS), so a missing policy
// would not surface in the ordinary suite.
//
// This test is the guard: it asks the catalog for every public table that has
// a tenant_id column but does NOT have row-level security enabled
// (relrowsecurity = false) and fails, listing the offenders. Any new
// tenant-owned table merged without an RLS policy turns this test red in CI.
//
// On tank_calibration_entries (the table the audit called out): it has NO
// tenant_id column — it is keyed only by chart_id and inherits isolation from
// its tenant-scoped parent tank_calibration_charts (FK + ON DELETE CASCADE),
// exactly like role_permissions inherits from roles (see migration 0012). A
// table with no tenant_id cannot have a tenant_id-based policy, so it is
// correctly OUT of this check's scope and needs no policy of its own. The
// check deliberately keys on the presence of a tenant_id column so such
// parent-scoped child tables are exempt by construction, while every table
// that DOES own a tenant_id must carry the policy.
//
// Gated on TEST_DATABASE_URL like the other integration tests; CI runs it
// against a migrated database. It cannot run locally without a database.

import (
	"context"
	"testing"
)

// TestRLS_NoTenantTableMissingPolicy fails if any public table that owns a
// tenant_id column lacks row-level security. This is a drift detector for
// migration 0074's coverage guarantee (MT-4).
func TestRLS_NoTenantTableMissingPolicy(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()

	// Every ordinary (relkind = 'r') public table that has a tenant_id column
	// but whose row-level security is OFF. Such a table, read over the
	// fuelgrid_app request pool, would be fully visible across tenants.
	const q = `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind = 'r'
		  AND NOT c.relrowsecurity
		  AND EXISTS (
		      SELECT 1 FROM information_schema.columns col
		      WHERE col.table_schema = 'public'
		        AND col.table_name = c.relname
		        AND col.column_name = 'tenant_id'
		  )
		ORDER BY c.relname`

	rows, err := h.pool.Query(ctx, q)
	if err != nil {
		t.Fatalf("query RLS coverage: %v", err)
	}
	defer rows.Close()

	var offenders []string
	for rows.Next() {
		var tbl string
		if err := rows.Scan(&tbl); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		offenders = append(offenders, tbl)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate RLS coverage rows: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf("%d tenant-owned table(s) have a tenant_id column but NO row-level "+
			"security — they are cross-tenant readable over the fuelgrid_app pool. "+
			"Add the tenant_isolation policy (see migration 0074): %v",
			len(offenders), offenders)
	}

	// Note: this check intentionally flags tables with NOT NULL *and* nullable
	// tenant_id columns. 0074 only auto-enabled RLS where the column is NOT
	// NULL; a tenant_id that is nullable AND unpolicied is itself a finding
	// (rows with NULL tenant_id can't be tenant-attributed), so surfacing it
	// here is the conservative, fail-closed behaviour.
}
