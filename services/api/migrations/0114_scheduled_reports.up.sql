-- 0114_scheduled_reports: per-tenant Scheduled Reports (Reports Center Phase 12 —
-- blueprint §8, §22.4).
--
-- A SCHEDULED REPORT is a tenant-owned definition that re-runs one catalog report
-- on a recurrence and DELIVERS the rendered file to a set of recipients over a
-- channel (in-app notification / email / webhook). It SUPERSEDES the two GLOBAL,
-- env-configured digests (report_daily_close_digest / report_monthly_pnl) that
-- aggregated EVERY tenant's financials into one body emailed to a flat env
-- recipient list — a tenant-isolation hazard. Those global jobs are REMOVED from
-- the scheduler in this phase; this per-tenant, RLS-isolated, permission-checked
-- mechanism replaces them. Every row belongs to exactly one tenant and is
-- tenant-isolated by RLS, so one tenant's schedule can never read or deliver
-- another tenant's data.
--
-- RECURRENCE MODEL (the `schedule` jsonb). A small, fully representable and
-- testable shape — NOT a free-form cron string by default:
--   { "frequency": "daily",   "hour": 22, "minute": 30 }
--   { "frequency": "weekly",  "hour": 8,  "minute": 0, "day_of_week": 1 }   -- 0=Sun..6=Sat
--   { "frequency": "monthly", "hour": 6,  "minute": 0, "day_of_month": 1 }  -- clamped to month length
--   { "frequency": "cron",    "cron": "30 22 * * *" }                        -- 5-field escape hatch
-- The Go layer (scheduled_reports.go nextRunAt) computes next_run_at
-- DETERMINISTICALLY from this shape; the column is the authoritative due-time so
-- the worker query stays a trivial `next_run_at <= now()`.
--
-- PERMISSION ANCHOR (blueprint §8.5 — "a user should not receive report data they
-- are no longer permitted to view"). Two recipient kinds live in `recipients`:
--   - a USER-ID recipient is its own permission identity: at every run the
--     worker re-checks THAT user's permission for report_key (+ station scope) and
--     SKIPS them if revoked — no notification row, no bytes.
--   - an EMAIL / webhook recipient has no platform identity, so the OWNER
--     (created_by) is the permission anchor: if the owner can no longer run the
--     report the whole run is skipped (recorded skipped_forbidden) and NOTHING is
--     generated or delivered. This is the documented rule for non-identity sinks.
--
-- IDEMPOTENCY. The worker CLAIMS a due row by atomically advancing next_run_at in
-- the same UPDATE that selects it (so a duplicated/concurrent tick can claim it at
-- most once), and records each generation in scheduled_report_runs keyed UNIQUE on
-- (scheduled_report_id, period_key) — a missed/retried tick for the same logical
-- period collapses to ONE delivery, mirroring the old digests' job_runs guard.

-- ---------------------------------------------------------------------------
-- reports.schedule — the tenant-wide MANAGE gate for creating/editing schedules.
-- Creating a schedule ALSO requires the underlying report's own read permission
-- (re-checked in-handler from report_key), so a holder of reports.schedule can
-- only schedule reports they can actually run. Granted to the same management
-- roles that already hold reports.export + a reporting reason to automate.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('reports.schedule', 'Create and manage scheduled report deliveries', 'reports', false)
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'reports.schedule'
  AND r.code IN (
      'system_admin', 'finance_officer', 'regional_manager', 'executive', 'station_manager'
  )
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- scheduled_reports — the per-tenant schedule definitions.
-- ---------------------------------------------------------------------------
CREATE TABLE scheduled_reports (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,

    -- WHAT runs.
    report_key       text  NOT NULL,
    name             text  NOT NULL,
    -- station / period (and any report-specific) inputs, the SAME map the live
    -- report endpoint + Export Center take.
    filters          jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- The recurrence shape (see header). Validated + interpreted in Go.
    schedule         jsonb NOT NULL,

    -- WHO receives it: an array of { "type": "user"|"email", "value": "<uuid|addr>" }.
    -- A user recipient is re-permission-checked individually; an email recipient is
    -- anchored on created_by.
    recipients       jsonb NOT NULL DEFAULT '[]'::jsonb,

    delivery_channel text  NOT NULL,
    format           text  NOT NULL,
    -- Required for delivery_channel = 'webhook'; NULL otherwise. SSRF-guarded in Go
    -- at delivery (https only, private/loopback/link-local hosts rejected).
    webhook_url      text,

    -- The permission anchor for email/webhook (non-identity) recipients, and the
    -- audit owner of the schedule.
    created_by       uuid  NOT NULL,

    enabled          boolean NOT NULL DEFAULT true,
    -- Last successful (or attempted) generation, and the next due instant the
    -- worker selects on. next_run_at is computed in Go from `schedule`.
    last_run_at      timestamptz,
    next_run_at      timestamptz NOT NULL,
    -- A short lifecycle/health label surfaced in the UI: active | paused | error.
    status           text  NOT NULL DEFAULT 'active',

    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_scheduled_reports_channel
        CHECK (delivery_channel IN ('in_app', 'email', 'webhook')),
    CONSTRAINT chk_scheduled_reports_format
        CHECK (format IN ('csv', 'pdf', 'xlsx')),
    CONSTRAINT chk_scheduled_reports_status
        CHECK (status IN ('active', 'paused', 'error')),
    -- A webhook schedule must carry a webhook_url; the other channels must not.
    CONSTRAINT chk_scheduled_reports_webhook
        CHECK (
            (delivery_channel = 'webhook' AND webhook_url IS NOT NULL)
            OR (delivery_channel <> 'webhook' AND webhook_url IS NULL)
        ),
    CONSTRAINT scheduled_reports_created_by_fk
        FOREIGN KEY (tenant_id, created_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

-- The worker scans enabled, due schedules across all tenants oldest-due first; a
-- partial index on the enabled set keeps that scan cheap as paused/disabled rows
-- accumulate.
CREATE INDEX idx_scheduled_reports_due
    ON scheduled_reports (next_run_at)
    WHERE enabled = true;

-- The tenant CRUD list (newest first).
CREATE INDEX idx_scheduled_reports_tenant
    ON scheduled_reports (tenant_id, created_at DESC);

CREATE TRIGGER scheduled_reports_set_updated_at
    BEFORE UPDATE ON scheduled_reports
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE scheduled_reports ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON scheduled_reports
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- scheduled_report_runs — the per-generation history ledger.
--
-- One row per logical PERIOD the worker generates for. period_key is the stable
-- identity of the run's period (e.g. "2026-06-15" for a daily run) so a duplicated
-- or retried tick for the same period collides on the UNIQUE index instead of
-- double-sending. The row records the produced export_job (the rendered file), the
-- notification ids delivered in-app, and any error / skip reason.
-- ---------------------------------------------------------------------------
CREATE TABLE scheduled_report_runs (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    scheduled_report_id uuid NOT NULL REFERENCES scheduled_reports(id) ON DELETE CASCADE,

    -- Stable identity of the logical period this run covers — the idempotency key.
    period_key          text NOT NULL,
    run_at              timestamptz NOT NULL DEFAULT now(),

    status              text NOT NULL DEFAULT 'success',
    -- The rendered file (NULL when the run was skipped/forbidden before render).
    export_job_id       uuid REFERENCES export_jobs(id) ON DELETE SET NULL,
    -- Notification ids delivered in-app (jsonb array of uuids), empty for other channels.
    notification_ids    jsonb NOT NULL DEFAULT '[]'::jsonb,
    -- How many recipients actually received the delivery (in-app rows / email sends).
    delivered_count     integer NOT NULL DEFAULT 0,
    -- How many recipients were SKIPPED because their permission was revoked.
    skipped_count       integer NOT NULL DEFAULT 0,
    error               text,

    created_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_scheduled_report_runs_status
        CHECK (status IN ('success', 'partial', 'failed', 'skipped_forbidden'))
);

-- IDEMPOTENCY: one run per (schedule, period). A duplicated/retried tick for the
-- same logical period fails this UNIQUE instead of producing a second delivery.
CREATE UNIQUE INDEX uq_scheduled_report_runs_period
    ON scheduled_report_runs (scheduled_report_id, period_key);

-- The schedule's recent-runs panel (newest first).
CREATE INDEX idx_scheduled_report_runs_schedule
    ON scheduled_report_runs (scheduled_report_id, run_at DESC);

ALTER TABLE scheduled_report_runs ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON scheduled_report_runs
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Catalog: the 'scheduled' category is now LIVE (real CRUD page + backing API).
-- Flip the seeded system row from 'placeholder' to 'live' and point it at the
-- real route. Gate it on reports.schedule so only schedule managers see the card.
-- ---------------------------------------------------------------------------
UPDATE report_categories
SET availability        = 'live',
    required_permission = 'reports.schedule'
WHERE tenant_id IS NULL AND key = 'scheduled';
