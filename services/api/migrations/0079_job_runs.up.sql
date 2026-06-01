-- 0079_job_runs: a visibility ledger for the background scheduler
-- (internal/scheduler). Every time the runner fires a job it appends one row
-- here — start time, finish time, terminal status, and a short detail string —
-- so operators can answer "did the nightly revenue compute run, when, and did
-- it succeed?" without scraping logs.
--
-- This is a SYSTEM table: the scheduler runs cross-tenant on the owner pool and
-- a single run can touch many tenants, so the ledger deliberately has NO
-- tenant_id and therefore NO row-level-security policy (there is nothing to
-- scope it to). It is owner-only by construction — only the table owner (the
-- migration/owner role) and the scheduler reach it; the request-scoped
-- fuelgrid_app role never queries it. Keep it that way: do not add an RLS
-- policy here.
--
-- Rows are written by the runner and are not meant to be mutated after the
-- terminal UPDATE that stamps finished_at/status/detail, but we intentionally
-- do NOT freeze the table with an immutability trigger (unlike the journal /
-- stock / outbox ledgers): this is operational telemetry, not a
-- financial/audit record, and a retention sweep is expected to prune old rows.

CREATE TABLE job_runs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_name    text NOT NULL,
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    status      text NOT NULL DEFAULT 'running',
    detail      text,

    CONSTRAINT chk_job_runs_status CHECK (status IN ('running', 'success', 'failure', 'skipped'))
);

-- "most recent runs of job X" is the dominant query (status endpoints, ops
-- dashboards, the retention sweep), so index job_name with the newest first.
CREATE INDEX idx_job_runs_job_started ON job_runs(job_name, started_at DESC);
