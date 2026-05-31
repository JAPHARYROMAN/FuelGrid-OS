-- Revert 0074_rls_coverage: intentional no-op.
--
-- This migration only ADDED row-level-security policies (defense in depth); it
-- never dropped or weakened anything. Reverting is not security-meaningful, and
-- a dynamic "disable RLS everywhere" down could strip protection from tables
-- that should keep it. On a full rollback-to-zero the tenant tables are dropped
-- wholesale by the base-schema downs, which removes their RLS with them; a
-- subsequent re-apply re-runs the up block and skips tables already enabled.
SELECT 1;
