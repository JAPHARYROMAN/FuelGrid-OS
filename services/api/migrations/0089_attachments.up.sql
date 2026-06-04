-- 0089_attachments: the generic per-entity file Attachments framework (C.3).
--
-- A single tenant-scoped table holds files attached to any business entity
-- (expense receipts to start; deliveries, invoices, etc. later) keyed by an
-- opaque (entity_type, entity_id) pair rather than a hard FK, so a new consumer
-- needs no schema change. The bytes are stored inline as bytea — exactly like
-- the tenant logo (0085_tenant_branding) — because this deployment has no
-- object store; the upload handler caps size (<= 5 MiB) and restricts the
-- content type (PDF/PNG/JPEG) so the column never grows unbounded.
--
-- Rows are APPEND-ONLY plus a soft delete: an attachment is never mutated, and
-- "removing" one sets deleted_at so the audit trail and any posted/locked
-- parent record keep their evidence. List/stream always filter deleted_at IS
-- NULL. station_id is optional context (some entities are station-scoped, some
-- tenant-wide) and is carried for reporting, not for the RLS check.

CREATE TABLE attachments (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    station_id    uuid REFERENCES stations(id) ON DELETE SET NULL,
    entity_type   text NOT NULL,   -- e.g. 'expense'
    entity_id     uuid NOT NULL,   -- the parent row's id (opaque, no FK)
    filename      text NOT NULL,
    content_type  text NOT NULL,
    size_bytes    bigint NOT NULL,
    data          bytea NOT NULL,  -- inline bytes (size-capped in the handler)
    checksum      text NOT NULL,   -- hex sha-256 of data, for integrity / dedupe
    uploaded_by   uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz,     -- soft delete; NULL means live

    CONSTRAINT chk_attachments_content_type
        CHECK (content_type IN ('application/pdf', 'image/png', 'image/jpeg')),
    CONSTRAINT chk_attachments_size
        CHECK (size_bytes >= 0 AND size_bytes <= 5242880)  -- 5 MiB
);

-- The hot path is "live attachments for one entity". A partial index on the
-- non-deleted rows keys that lookup directly.
CREATE INDEX idx_attachments_entity
    ON attachments (tenant_id, entity_type, entity_id)
    WHERE deleted_at IS NULL;

ALTER TABLE attachments ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON attachments
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- Permissions: attachment.read (view/download) and attachment.manage (upload /
-- remove). Both are tenant-wide (station_scoped=false) — an attachment's reach
-- follows its parent entity's own permission, and the optional station_id is
-- context only. Mirrors 0004_rbac's seed + grant pattern.
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('attachment.read',   'View and download entity attachments', 'documents', false),
    ('attachment.manage', 'Upload and remove entity attachments',  'documents', false);

-- Grant to the system roles that already manage the records attachments hang
-- off. Read is broad (any role that can see finance/ops records); manage is the
-- finance/management roles. system_admin gets everything via the catch-all.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system
  AND p.code IN ('attachment.read', 'attachment.manage')
  AND (
    (p.code = 'attachment.read' AND r.code IN (
        'supervisor', 'station_manager', 'regional_manager',
        'finance_officer', 'procurement_officer', 'auditor', 'executive'
    ))
    OR (p.code = 'attachment.manage' AND r.code IN (
        'station_manager', 'regional_manager', 'finance_officer', 'procurement_officer'
    ))
    -- system_admin holds everything (scoped to the two new codes by the
    -- p.code filter above, so this never re-grants prior permissions).
    OR (r.code = 'system_admin')
);
