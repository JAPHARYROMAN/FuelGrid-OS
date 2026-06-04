-- 0091_export_jobs: a lightweight job model for report exports (Feature 10.7).
--
-- Today's report exports are synchronous: a request streams the CSV/PDF/XLSX
-- file straight back. This table records WHAT was requested (report_key, format,
-- the filters that produced it) and the resulting file's metadata, so the
-- reporting hub can surface an export history. The file is still produced via the
-- existing synchronous export path; an export_jobs row is the durable receipt,
-- mirroring the 'report.exported' audit event the exports already emit.
--
-- status is 'completed' on a successful synchronous produce, or 'failed' if the
-- file could not be generated. The model is forward-compatible with an async
-- worker (a 'queued' -> 'running' -> 'completed' lifecycle) without a schema
-- change, but no worker is introduced here.

CREATE TABLE export_jobs (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    report_key   text NOT NULL,
    format       text NOT NULL,
    filters      jsonb NOT NULL DEFAULT '{}'::jsonb,
    status       text NOT NULL DEFAULT 'completed',
    file_url     text,
    file_name    text,
    file_size    bigint,
    error        text,
    requested_by uuid NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_export_jobs_status
        CHECK (status IN ('queued', 'running', 'completed', 'failed')),
    CONSTRAINT chk_export_jobs_format
        CHECK (format IN ('csv', 'pdf', 'xlsx')),
    CONSTRAINT export_jobs_requested_by_fk
        FOREIGN KEY (tenant_id, requested_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_export_jobs_tenant ON export_jobs(tenant_id, created_at DESC);

ALTER TABLE export_jobs ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON export_jobs
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- Permissions reports.export, revenue.read and finance.read already exist
-- (0004 / 0033) and gate this surface; no new permission is required.
