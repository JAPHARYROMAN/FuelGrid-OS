-- 0074_rls_coverage: extend tenant row-level security to EVERY tenant-owned
-- table (INFRA-01 / AUTH-25 / MT-4).
--
-- 0005 enabled RLS on the Phase-1 tables, and later phase migrations added it
-- inline for many of their own tables — but coverage drifted, leaving some
-- tenant-owned tables (e.g. tank_calibration_entries) with NO policy. Once the
-- API runs as the non-owner fuelgrid_app role (now the production default —
-- see config.validate), an un-policied tenant table would be fully visible
-- across tenants. This migration closes the gap comprehensively: every public
-- table that has a NOT NULL tenant_id column and does not already have RLS
-- enabled gets the standard tenant_isolation policy.
--
-- RLS is ENABLED, not FORCED: the table owner (migrations, the seed, and the
-- owner-pool background jobs / outbox publisher that legitimately span tenants)
-- still bypasses it, while the fuelgrid_app request pool — which sets
-- app.current_tenant per request — is confined to one tenant. FORCE (subjecting
-- the owner too) is deliberately deferred: it would require the seed and every
-- background job to set a per-tenant GUC first.
--
-- Tables already carrying RLS (relrowsecurity = true) are skipped, so their
-- existing policies — including roles' special tenant-or-system policy — are
-- left untouched and no duplicate policy is created. Re-running after the
-- no-op down is therefore a clean no-op.

DO $$
DECLARE r record;
BEGIN
    FOR r IN
        SELECT c.relname AS tbl
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
                AND col.is_nullable = 'NO'
          )
    LOOP
        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', r.tbl);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON public.%I '
            'USING (tenant_id::text = current_setting(''app.current_tenant'', true)) '
            'WITH CHECK (tenant_id::text = current_setting(''app.current_tenant'', true))',
            r.tbl
        );
        RAISE NOTICE 'rls_coverage: enabled tenant_isolation on %', r.tbl;
    END LOOP;
END$$;
