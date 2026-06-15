-- 0113_report_snapshots: immutable captured report snapshots + sign-off / reopen
-- revision chain (Reports Center Phase 14 — Report Locking & Snapshots,
-- blueprint §15).
--
-- A report SNAPSHOT is a point-in-time capture of a rendered report: the exact
-- ReportEnvelope (report_envelope.go) the live structured endpoint produced at
-- capture time, stored VERBATIM (every money/litre figure is already the exact
-- decimal STRING the repos returned — nothing is recomputed here), plus a
-- content_hash = sha256 of a CANONICAL serialization of that envelope so the
-- capture is provably tamper-evident.
--
-- IMMUTABILITY IS THE CORE PROPERTY (blueprint §15.4). The captured payload
-- (envelope, content_hash) and the capture provenance (captured_by/at,
-- report_key, filters_used, revision, supersedes_id) are FROZEN once written —
-- a trigger BLOCKS any UPDATE that would mutate them, mirroring the audit_logs
-- (0070) / journal-ledger (0065) / stock-ledger (0069) append-only discipline.
-- Only the sign-off lifecycle columns (status, signed_off_by/at,
-- correction_note) may transition. A CORRECTION never overwrites: it captures a
-- NEW row (revision+1) whose supersedes_id points at the prior revision, so the
-- original snapshot stays readable forever.
--
-- DELETE is blocked too, except under the same app.allow_ledger_delete escape
-- hatch the other ledgers use (integration-test cleanup / deliberate
-- tenant-offboarding purge). current_setting(..., true) fails closed when unset.
--
-- RLS + tenant_isolation exactly like the sibling tables (export_jobs / reports);
-- the per-snapshot read/capture authorization (the SAME permission as running the
-- report live) is enforced in the handler layer.

CREATE TABLE report_snapshots (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,

    -- WHAT was captured.
    report_key      text  NOT NULL,
    filters_used    jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- The rendered ReportEnvelope, captured verbatim at snapshot time. IMMUTABLE.
    envelope        jsonb NOT NULL,
    -- sha256 (hex) of the canonical serialization of `envelope`. IMMUTABLE.
    content_hash    text  NOT NULL,

    -- WHO captured it + WHEN. IMMUTABLE provenance.
    captured_by     uuid NOT NULL,
    captured_at     timestamptz NOT NULL DEFAULT now(),

    -- Sign-off lifecycle (the only mutable columns).
    status          text NOT NULL DEFAULT 'draft',
    -- Revision chain: a correction captures a NEW row (revision+1) whose
    -- supersedes_id points at the prior revision; the original is never touched.
    revision        integer NOT NULL DEFAULT 1,
    supersedes_id   uuid REFERENCES report_snapshots(id) ON DELETE RESTRICT,

    signed_off_by   uuid,
    signed_off_at   timestamptz,
    correction_note text,

    created_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_report_snapshots_status
        CHECK (status IN ('draft', 'signed_off', 'reopened')),
    CONSTRAINT chk_report_snapshots_revision
        CHECK (revision >= 1),
    -- A signed-off snapshot must carry its signer + time; a non-signed one must
    -- not (so the sign-off provenance can never be half-written).
    CONSTRAINT chk_report_snapshots_signoff
        CHECK (
            (status = 'signed_off' AND signed_off_by IS NOT NULL AND signed_off_at IS NOT NULL)
            OR (status <> 'signed_off' AND signed_off_by IS NULL AND signed_off_at IS NULL)
        ),
    CONSTRAINT report_snapshots_captured_by_fk
        FOREIGN KEY (tenant_id, captured_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT report_snapshots_signed_off_by_fk
        FOREIGN KEY (tenant_id, signed_off_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

-- List a report's snapshots newest-first (the report page panel + revision chain).
CREATE INDEX idx_report_snapshots_report
    ON report_snapshots (tenant_id, report_key, captured_at DESC);

-- The hub "Locked" rail lists recent SIGNED-OFF snapshots across reports.
CREATE INDEX idx_report_snapshots_signed_off
    ON report_snapshots (tenant_id, signed_off_at DESC)
    WHERE status = 'signed_off';

-- Walk a revision chain by its supersedes link.
CREATE INDEX idx_report_snapshots_supersedes
    ON report_snapshots (supersedes_id)
    WHERE supersedes_id IS NOT NULL;

ALTER TABLE report_snapshots ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON report_snapshots
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- Append-only / immutability enforcement at the DB layer (blueprint §15.4).
--
-- The captured payload (envelope, content_hash) and the capture provenance
-- (report_key, filters_used, captured_by, captured_at, revision, supersedes_id,
-- tenant_id, id, created_at) are FROZEN once written. Any UPDATE that changes
-- one of them is rejected; only the sign-off lifecycle columns (status,
-- signed_off_by, signed_off_at, correction_note) may change. A correction is a
-- NEW INSERT, never an UPDATE, so the original revision is preserved.
--
-- DELETE follows the ledger escape-hatch convention: blocked unless
-- current_setting('app.allow_ledger_delete', true) = 'on' (whole-tenant teardown
-- / test cleanup only). An unset GUC reads NULL and fails closed.
CREATE OR REPLACE FUNCTION assert_report_snapshot_immutable() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF current_setting('app.allow_ledger_delete', true) = 'on' THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'report_snapshots are append-only: snapshot % cannot be deleted', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    -- UPDATE: the captured payload + provenance are immutable. Reject any change
    -- to them; a correction must create a NEW revision row, never rewrite this one.
    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
        OR NEW.report_key IS DISTINCT FROM OLD.report_key
        OR NEW.filters_used IS DISTINCT FROM OLD.filters_used
        OR NEW.envelope IS DISTINCT FROM OLD.envelope
        OR NEW.content_hash IS DISTINCT FROM OLD.content_hash
        OR NEW.captured_by IS DISTINCT FROM OLD.captured_by
        OR NEW.captured_at IS DISTINCT FROM OLD.captured_at
        OR NEW.revision IS DISTINCT FROM OLD.revision
        OR NEW.supersedes_id IS DISTINCT FROM OLD.supersedes_id
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
    THEN
        RAISE EXCEPTION 'report_snapshots are immutable: snapshot % captured payload/provenance cannot be modified (corrections create a new revision)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER report_snapshots_immutable
    BEFORE UPDATE OR DELETE ON report_snapshots
    FOR EACH ROW EXECUTE FUNCTION assert_report_snapshot_immutable();
