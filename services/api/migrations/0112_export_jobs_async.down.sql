-- Reverse 0112_export_jobs_async: drop the async worker columns + index, leaving
-- the original synchronous-receipt shape from 0091 intact.
DROP INDEX IF EXISTS idx_export_jobs_queued;

ALTER TABLE export_jobs
    DROP COLUMN IF EXISTS result_bytes,
    DROP COLUMN IF EXISTS result_content_type,
    DROP COLUMN IF EXISTS result_filename,
    DROP COLUMN IF EXISTS result_size,
    DROP COLUMN IF EXISTS result_checksum,
    DROP COLUMN IF EXISTS started_at,
    DROP COLUMN IF EXISTS completed_at,
    DROP COLUMN IF EXISTS attempts;
