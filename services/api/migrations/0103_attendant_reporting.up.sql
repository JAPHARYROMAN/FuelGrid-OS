-- 0103_attendant_reporting: Mobile Attendant App Phase 7 — per-attendant
-- notification dedupe, attendant issue reporting (incidents.report), and the
-- offline-replay dedupe key on incidents.
--
-- Three concerns, all additive:
--
--   1. notifications.source_event_id — the outbox event that produced a feed
--      row. The notification subscriber is at-least-once (a failed handler
--      leaves the outbox event unpublished and EVERY handler re-runs on the
--      next tick), so per-attendant notifications need a database-enforced
--      dedupe or a redelivery would spam the attendant. Same partial-unique
--      pattern as payments.idempotency_key (0096): nullable column, existing
--      rows unaffected, INSERT ... ON CONFLICT DO NOTHING in the repo. The
--      target user is part of the key (COALESCEd for tenant-wide rows) because
--      one event legitimately fans out to several attendants (shift approved).
--
--   2. incidents.report — a station-scoped permission for attendant
--      self-service issue reporting (PRD §6.12). Holders create incidents ONLY
--      at the station of their own current shift, derived server-side; the
--      supervisor-tier incidents.manage write path is untouched.
--
--   3. incidents.dedupe_key — the mobile offline queue replays creations,
--      which are non-idempotent. A client-supplied key (partial unique per
--      tenant, exactly the 0096 payments pattern) lets a replayed create
--      return the existing incident instead of duplicating it.
--
-- The incident type CHECK gains the PRD §6.12 issue types (pump, nozzle,
-- meter, payment) alongside the existing vocabulary — additive, nothing
-- existing is remapped.

-- ---------------------------------------------------------------------------
-- 1. Notification redelivery dedupe.
-- ---------------------------------------------------------------------------
ALTER TABLE notifications
    ADD COLUMN source_event_id uuid;

-- One feed row per (tenant, outbox event, target user). user_id is NULL for
-- tenant-wide rows, and Postgres unique indexes treat NULLs as distinct, so
-- the target leg is COALESCEd to the zero uuid — a redelivered tenant-wide
-- mapping dedupes too. Partial predicate keeps all pre-0103 rows (and any
-- writer that supplies no event id) out of the index entirely.
CREATE UNIQUE INDEX uq_notifications_event_target
    ON notifications (tenant_id, source_event_id,
                      COALESCE(user_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE source_event_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 2. Incident type vocabulary: add the PRD attendant issue types.
-- ---------------------------------------------------------------------------
ALTER TABLE incidents
    DROP CONSTRAINT chk_incidents_type;
ALTER TABLE incidents
    ADD CONSTRAINT chk_incidents_type CHECK (
        type IN ('equipment', 'leak', 'variance', 'safety', 'calibration', 'other',
                 'pump', 'nozzle', 'meter', 'payment')
    );

-- ---------------------------------------------------------------------------
-- 3. Offline-replay dedupe key on incidents (0096 payments pattern).
-- ---------------------------------------------------------------------------
ALTER TABLE incidents
    ADD COLUMN dedupe_key text;

-- Bound the key length so a client cannot store unbounded blobs; 255 is ample
-- for a UUID/ULID/opaque token. NULL (no key supplied) is always allowed.
ALTER TABLE incidents
    ADD CONSTRAINT chk_incidents_dedupe_key_len
        CHECK (dedupe_key IS NULL OR char_length(dedupe_key) BETWEEN 1 AND 255);

-- Partial unique: one incident per (tenant, dedupe_key). Safe on the
-- populated table — every existing row is NULL and the predicate excludes
-- NULLs from the index.
CREATE UNIQUE INDEX uq_incidents_tenant_dedupe_key
    ON incidents (tenant_id, dedupe_key)
    WHERE dedupe_key IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 4. incidents.report permission, seeded to the workflow roles.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('incidents.report', 'Report an operational issue at the station of own current shift', 'station', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system
  AND p.code = 'incidents.report'
  AND r.code IN ('attendant', 'supervisor', 'station_manager', 'regional_manager')
ON CONFLICT (role_id, permission_id) DO NOTHING;
