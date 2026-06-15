-- Reverse 0113_report_snapshots: drop the snapshot table, its immutability
-- trigger/function and indexes cleanly.
DROP TRIGGER IF EXISTS report_snapshots_immutable ON report_snapshots;
DROP FUNCTION IF EXISTS assert_report_snapshot_immutable();
DROP INDEX IF EXISTS idx_report_snapshots_supersedes;
DROP INDEX IF EXISTS idx_report_snapshots_signed_off;
DROP INDEX IF EXISTS idx_report_snapshots_report;
DROP TABLE IF EXISTS report_snapshots;
