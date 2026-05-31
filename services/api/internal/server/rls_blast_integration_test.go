package server_test

// Cross-tenant "blast" proof that Postgres row-level security isolates tenants
// across a BROAD set of tenant-owned tables — not just one — when queried over
// the real production mechanism: a fuelgrid_app connection scoped with
// app.current_tenant via database.Pool.AcquireTenant (the exact path the
// request middleware uses). Migration 0074 RLS-enabled every tenant_id table;
// the prior rls_integration_test.go only exercised `companies`. This widens the
// proof to the whole RLS surface, data-driven over a table list (Wave-9 MT-6).
//
// For each table we seed a real row for BOTH tenant A and tenant B (via the
// owner pool, which bypasses RLS), then under tenant A's scoped fuelgrid_app
// connection assert:
//   (a) READ isolation — counting rows for tenant B returns ZERO (the USING
//       policy hides them), even though the rows demonstrably exist; and
//   (b) WRITE isolation — inserting a row carrying tenant B's tenant_id is
//       rejected by the WITH CHECK policy. The cross-tenant INSERT is otherwise
//       valid (it references tenant B's own child rows), so the rejection is
//       the policy firing, not an incidental FK/constraint failure.
//
// Gated on TEST_DATABASE_URL like the other integration tests; skips without a
// migrated database. CI runs it against the enforced (fuelgrid_app) path.

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// blastTenant holds the seeded row ids for one tenant across every table the
// blast exercises. The cross-tenant INSERT templates reference these so the
// only thing "wrong" about a cross write is the tenant_id under another
// tenant's GUC — isolating the WITH CHECK policy as the rejecting cause.
type blastTenant struct {
	tenantID uuid.UUID
	company  uuid.UUID
	station  uuid.UUID
	product  uuid.UUID
	tank     uuid.UUID
	supplier uuid.UUID
	customer uuid.UUID
	user     uuid.UUID
	account  uuid.UUID
	period   uuid.UUID
	journal  uuid.UUID
	movement uuid.UUID
	payable  uuid.UUID
}

// blastTable describes one table in the blast: its name, the id of the seeded
// row that proves it exists, and a parameterized cross-tenant INSERT that is
// valid except for being run under the wrong tenant's GUC. The INSERT carries
// $1 = the (foreign) tenant_id; remaining args are supplied by argsFor.
type blastTable struct {
	name      string
	rowIDOf   func(bt blastTenant) uuid.UUID
	insertSQL string
	argsFor   func(bt blastTenant) []any
}

func blastTables() []blastTable {
	return []blastTable{
		{
			name:    "companies",
			rowIDOf: func(bt blastTenant) uuid.UUID { return bt.company },
			insertSQL: `INSERT INTO companies (tenant_id, name, legal_name, currency, timezone)
			            VALUES ($1, 'Blast', 'Blast', 'USD', 'UTC')`,
			argsFor: func(bt blastTenant) []any { return nil },
		},
		{
			name:    "stations",
			rowIDOf: func(bt blastTenant) uuid.UUID { return bt.station },
			insertSQL: `INSERT INTO stations (tenant_id, company_id, name, code)
			            VALUES ($1, $2, 'Blast Station', 'BLST-X')`,
			argsFor: func(bt blastTenant) []any { return []any{bt.company} },
		},
		{
			name:    "products",
			rowIDOf: func(bt blastTenant) uuid.UUID { return bt.product },
			insertSQL: `INSERT INTO products (tenant_id, code, name, default_price, color)
			            VALUES ($1, 'BLX', 'Blast', 1000.00, '#000000')`,
			argsFor: func(bt blastTenant) []any { return nil },
		},
		{
			name:    "tanks",
			rowIDOf: func(bt blastTenant) uuid.UUID { return bt.tank },
			insertSQL: `INSERT INTO tanks (tenant_id, station_id, product_id, name, code, capacity_litres, safe_max_litres)
			            VALUES ($1, $2, $3, 'Blast T', 'BTX', 10000, 9500)`,
			// station + product must belong to the foreign tenant for the row to
			// be valid; the WITH CHECK on tenant_id is what rejects it.
			argsFor: func(bt blastTenant) []any { return []any{bt.station, bt.product} },
		},
		{
			name:    "suppliers",
			rowIDOf: func(bt blastTenant) uuid.UUID { return bt.supplier },
			insertSQL: `INSERT INTO suppliers (tenant_id, code, name)
			            VALUES ($1, 'BSUP-X', 'Blast Supplier')`,
			argsFor: func(bt blastTenant) []any { return nil },
		},
		{
			name:    "customers",
			rowIDOf: func(bt blastTenant) uuid.UUID { return bt.customer },
			insertSQL: `INSERT INTO customers (tenant_id, code, name)
			            VALUES ($1, 'BCUS-X', 'Blast Customer')`,
			argsFor: func(bt blastTenant) []any { return nil },
		},
		{
			name:    "accounts",
			rowIDOf: func(bt blastTenant) uuid.UUID { return bt.account },
			insertSQL: `INSERT INTO accounts (tenant_id, code, name, type, normal_balance)
			            VALUES ($1, 'BACC-X', 'Blast Account', 'asset', 'debit')`,
			argsFor: func(bt blastTenant) []any { return nil },
		},
		{
			name:    "accounting_periods",
			rowIDOf: func(bt blastTenant) uuid.UUID { return bt.period },
			// A non-overlapping date range (the seeded period uses 2099-02).
			insertSQL: `INSERT INTO accounting_periods (tenant_id, start_date, end_date)
			            VALUES ($1, '2099-03-01', '2099-03-31')`,
			argsFor: func(bt blastTenant) []any { return nil },
		},
		{
			name:    "journal_entries",
			rowIDOf: func(bt blastTenant) uuid.UUID { return bt.journal },
			// period + posted_by reference the foreign tenant's own rows.
			insertSQL: `INSERT INTO journal_entries (tenant_id, period_id, entry_date, source_type, posted_by)
			            VALUES ($1, $2, '2099-02-15', 'adjustment', $3)`,
			argsFor: func(bt blastTenant) []any { return []any{bt.period, bt.user} },
		},
		{
			name:    "stock_movements",
			rowIDOf: func(bt blastTenant) uuid.UUID { return bt.movement },
			insertSQL: `INSERT INTO stock_movements (tenant_id, tank_id, movement_type, litres, balance_after, recorded_by)
			            VALUES ($1, $2, 'opening', 100, 100, $3)`,
			argsFor: func(bt blastTenant) []any { return []any{bt.tank, bt.user} },
		},
		{
			name:    "payables",
			rowIDOf: func(bt blastTenant) uuid.UUID { return bt.payable },
			insertSQL: `INSERT INTO payables (tenant_id, supplier_id, source_invoice_id, amount, outstanding_amount)
			            VALUES ($1, $2, $3, 500, 500)`,
			argsFor: func(bt blastTenant) []any { return []any{bt.supplier, uuid.New()} },
		},
	}
}

// seedBlast inserts a real row for the given tenant across every blast table,
// via the owner pool (bypasses RLS). The FK chain is built bottom-up. Returns
// the captured row ids.
func seedBlast(t *testing.T, ctx context.Context, owner *database.Pool, tenant uuid.UUID, tag string) blastTenant {
	t.Helper()
	bt := blastTenant{tenantID: tenant}
	q := func(dest *uuid.UUID, sql string, args ...any) {
		if err := owner.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("seed %s [%s]: %v", tag, sql, err)
		}
	}

	q(&bt.company, `INSERT INTO companies (tenant_id, name, legal_name, currency, timezone)
	                VALUES ($1, $2, $2, 'USD', 'UTC') RETURNING id`, tenant, "Blast Co "+tag)
	q(&bt.station, `INSERT INTO stations (tenant_id, company_id, name, code)
	                VALUES ($1, $2, 'Blast Station', $3) RETURNING id`, tenant, bt.company, "BST-"+tag)
	q(&bt.product, `INSERT INTO products (tenant_id, code, name, default_price, color)
	                VALUES ($1, $2, 'Blast Product', 1000.00, '#123456') RETURNING id`, tenant, "BP-"+tag)
	q(&bt.tank, `INSERT INTO tanks (tenant_id, station_id, product_id, name, code, capacity_litres, safe_max_litres)
	             VALUES ($1, $2, $3, 'Blast Tank', $4, 20000, 19000) RETURNING id`,
		tenant, bt.station, bt.product, "BT-"+tag)
	q(&bt.supplier, `INSERT INTO suppliers (tenant_id, code, name)
	                 VALUES ($1, $2, 'Blast Supplier') RETURNING id`, tenant, "BS-"+tag)
	q(&bt.customer, `INSERT INTO customers (tenant_id, code, name)
	                 VALUES ($1, $2, 'Blast Customer') RETURNING id`, tenant, "BC-"+tag)
	q(&bt.user, `INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
	             VALUES ($1, $2, 'Blast User', 'active', 'x', now()) RETURNING id`,
		tenant, "blast-"+tag+"-"+tenant.String()[:8]+"@blast.local")
	q(&bt.account, `INSERT INTO accounts (tenant_id, code, name, type, normal_balance)
	                VALUES ($1, $2, 'Blast Account', 'asset', 'debit') RETURNING id`, tenant, "BA-"+tag)
	q(&bt.period, `INSERT INTO accounting_periods (tenant_id, start_date, end_date)
	               VALUES ($1, '2099-02-01', '2099-02-28') RETURNING id`, tenant)
	q(&bt.journal, `INSERT INTO journal_entries (tenant_id, period_id, entry_date, source_type, posted_by)
	                VALUES ($1, $2, '2099-02-10', 'adjustment', $3) RETURNING id`, tenant, bt.period, bt.user)
	// One journal line keeps the entry realistic (and exercises journal_lines RLS
	// indirectly via the seed); not asserted directly.
	if _, err := owner.Exec(ctx, `INSERT INTO journal_lines (tenant_id, journal_entry_id, account_id, debit, credit)
	                              VALUES ($1, $2, $3, 100, 0)`, tenant, bt.journal, bt.account); err != nil {
		t.Fatalf("seed %s journal_line: %v", tag, err)
	}
	q(&bt.movement, `INSERT INTO stock_movements (tenant_id, tank_id, movement_type, litres, balance_after, recorded_by)
	                 VALUES ($1, $2, 'opening', 500, 500, $3) RETURNING id`, tenant, bt.tank, bt.user)
	q(&bt.payable, `INSERT INTO payables (tenant_id, supplier_id, source_invoice_id, amount, outstanding_amount)
	                VALUES ($1, $2, $3, 1000, 1000) RETURNING id`, tenant, bt.supplier, uuid.New())
	return bt
}

// cleanupBlast tears down everything seedBlast created for a tenant, in
// child-before-parent order. The ledger immutability triggers (0065/0069)
// require the escape-hatch GUC, so all deletes run on one connection that sets
// it for the session — mirroring cleanupTenant in the phase2 harness.
func cleanupBlast(ctx context.Context, owner *database.Pool, tenants ...uuid.UUID) {
	stmts := []string{
		`DELETE FROM payables WHERE tenant_id = $1`,
		`DELETE FROM stock_movements WHERE tenant_id = $1`,
		`DELETE FROM journal_lines WHERE tenant_id = $1`,
		`DELETE FROM journal_entries WHERE tenant_id = $1`,
		`DELETE FROM accounting_periods WHERE tenant_id = $1`,
		`DELETE FROM accounts WHERE tenant_id = $1`,
		`DELETE FROM customers WHERE tenant_id = $1`,
		`DELETE FROM suppliers WHERE tenant_id = $1`,
		`DELETE FROM tanks WHERE tenant_id = $1`,
		`DELETE FROM products WHERE tenant_id = $1`,
		`DELETE FROM stations WHERE tenant_id = $1`,
		`DELETE FROM users WHERE tenant_id = $1`,
		`DELETE FROM companies WHERE tenant_id = $1`,
		`DELETE FROM tenants WHERE id = $1`,
	}
	conn, err := owner.Acquire(ctx)
	if err != nil {
		for _, tn := range tenants {
			for _, s := range stmts {
				_, _ = owner.Exec(ctx, s, tn)
			}
		}
		return
	}
	defer conn.Release()
	_, _ = conn.Exec(ctx, `SET app.allow_ledger_delete = 'on'`)
	for _, tn := range tenants {
		for _, s := range stmts {
			_, _ = conn.Exec(ctx, s, tn)
		}
	}
}

// TestRLS_BlastCrossTenantIsolation is the data-driven blast: across a broad
// set of tenant-owned tables, a fuelgrid_app connection scoped to tenant A (via
// the production AcquireTenant) can neither READ tenant B's rows nor WRITE a row
// for tenant B. A failure on any single table means RLS is not isolating that
// part of the surface.
func TestRLS_BlastCrossTenantIsolation(t *testing.T) {
	ownerURL := os.Getenv("TEST_DATABASE_URL")
	if ownerURL == "" {
		t.Skip("set TEST_DATABASE_URL to run the RLS blast isolation test")
	}
	ctx := context.Background()

	owner, err := database.Connect(ctx, database.Config{URL: ownerURL})
	if err != nil {
		t.Fatalf("owner pool: %v", err)
	}
	defer owner.Close()

	// Guarantee the fuelgrid_app login password matches what we connect with
	// (the role exists from migration 0005; idempotent, owner-only).
	if _, err := owner.Exec(ctx, `ALTER ROLE fuelgrid_app WITH LOGIN PASSWORD 'fuelgrid_app'`); err != nil {
		t.Fatalf("ensure fuelgrid_app password: %v", err)
	}

	appURL, err := appRoleURL(ownerURL)
	if err != nil {
		t.Fatalf("app url: %v", err)
	}
	app, err := database.Connect(ctx, database.Config{URL: appURL})
	if err != nil {
		t.Fatalf("app pool (fuelgrid_app): %v", err)
	}
	defer app.Close()

	// Prove the app role is NOT an RLS-bypassing superuser — otherwise the
	// whole blast would trivially "pass" by seeing everything.
	var isSuper bool
	if err := app.QueryRow(ctx, `SELECT rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&isSuper); err != nil {
		t.Fatalf("role check: %v", err)
	}
	if isSuper {
		t.Fatal("fuelgrid_app must not be a superuser, or RLS would be bypassed")
	}

	tenantA, tenantB := uuid.New(), uuid.New()
	if _, err := owner.Exec(ctx, `
		INSERT INTO tenants (id, name, slug) VALUES ($1, 'Blast A', $2), ($3, 'Blast B', $4)
	`, tenantA, "blast-a-"+tenantA.String()[:8], tenantB, "blast-b-"+tenantB.String()[:8]); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}
	t.Cleanup(func() { cleanupBlast(ctx, owner, tenantA, tenantB) })

	btA := seedBlast(t, ctx, owner, tenantA, "A")
	btB := seedBlast(t, ctx, owner, tenantB, "B")

	tables := blastTables()

	// Scope a single fuelgrid_app connection to tenant A for the whole blast,
	// exactly as the request middleware does for one request.
	scopedCtx, release, err := app.AcquireTenant(ctx, tenantA)
	if err != nil {
		t.Fatalf("AcquireTenant(A): %v", err)
	}
	defer release()

	for _, tbl := range tables {
		t.Run(tbl.name, func(t *testing.T) {
			// Sanity: the OWNER (RLS-bypassing) can see tenant B's row, so the
			// row genuinely exists — a zero count under A's scope is RLS hiding
			// it, not a missing seed.
			var ownerSees int
			if err := owner.QueryRow(ctx,
				`SELECT count(*) FROM `+tbl.name+` WHERE tenant_id = $1 AND id = $2`,
				btB.tenantID, tbl.rowIDOf(btB)).Scan(&ownerSees); err != nil {
				t.Fatalf("owner count: %v", err)
			}
			if ownerSees != 1 {
				t.Fatalf("precondition: owner sees %d of tenant B's %s row, want 1 (seed problem)", ownerSees, tbl.name)
			}

			// (a) READ isolation: under tenant A's GUC, tenant B's rows are zero.
			var seenB int
			if err := app.QueryRow(scopedCtx,
				`SELECT count(*) FROM `+tbl.name+` WHERE tenant_id = $1`, btB.tenantID).Scan(&seenB); err != nil {
				t.Fatalf("scoped count of tenant B %s: %v", tbl.name, err)
			}
			if seenB != 0 {
				t.Fatalf("tenant A sees %d of tenant B's %s rows under RLS — NOT isolated", seenB, tbl.name)
			}
			// And it sees its OWN row (the scope is live, not just empty).
			var seenA int
			if err := app.QueryRow(scopedCtx,
				`SELECT count(*) FROM `+tbl.name+` WHERE tenant_id = $1`, btA.tenantID).Scan(&seenA); err != nil {
				t.Fatalf("scoped count of tenant A %s: %v", tbl.name, err)
			}
			if seenA < 1 {
				t.Fatalf("tenant A sees %d of its OWN %s rows under RLS — scope is broken, not just isolating", seenA, tbl.name)
			}

			// (b) WRITE isolation: inserting a row for tenant B under A's GUC is
			// rejected by WITH CHECK. The insert references tenant B's own child
			// rows, so the row is otherwise valid — the rejection is the policy,
			// not an incidental FK/constraint failure. AcquireTenant hands out a
			// non-transactional session connection, so each Exec auto-commits
			// independently and a rejected INSERT does not poison the next table.
			args := append([]any{btB.tenantID}, tbl.argsFor(btB)...)
			if _, ierr := app.Exec(scopedCtx, tbl.insertSQL, args...); ierr == nil {
				// Should never persist — undo via the owner if WITH CHECK let it
				// through, so the failure does not leak into other assertions.
				_, _ = owner.Exec(ctx, `DELETE FROM `+tbl.name+` WHERE tenant_id = $1 AND id NOT IN ($2)`, btB.tenantID, tbl.rowIDOf(btB))
				t.Fatalf("WITH CHECK did not reject inserting a %s row for another tenant", tbl.name)
			}
		})
	}
}
