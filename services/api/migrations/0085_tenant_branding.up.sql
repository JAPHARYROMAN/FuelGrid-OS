-- 0085_tenant_branding: the tenant-level company letterhead / branding row.
--
-- Letterhead is TENANT-LEVEL (one row per tenant): customer/supplier/product
-- lists are tenant-shared, so the documents printed from them carry one
-- consistent letterhead rather than per-company variants. Every downloadable
-- PDF (the existing report exports, plus the per-entity document PDFs a later
-- wave adds) renders its header from this row via the shared letterhead helper.
--
-- The logo is stored inline as bytea (size-capped in the upload handler to
-- <= 1 MiB, content-type restricted to PNG/JPEG) so a brand-new tenant needs no
-- object store to print a branded document. Text fields are all optional; the
-- helper degrades gracefully when they are empty.

CREATE TABLE tenant_branding (
    tenant_id         uuid PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    display_name      text,            -- brand/trading name shown large on the letterhead
    legal_name        text,
    tax_id            text,
    registration_no   text,
    address_line1     text,
    address_line2     text,
    city              text,
    country           text,
    phone             text,
    email             text,
    website           text,
    footer_note       text,            -- optional footer line (e.g. "Confidential")
    logo              bytea,           -- optional PNG/JPEG bytes, size-capped in the handler
    logo_content_type text,            -- 'image/png' | 'image/jpeg'
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_tenant_branding_logo_content_type
        CHECK (logo_content_type IS NULL OR logo_content_type IN ('image/png', 'image/jpeg'))
);

CREATE TRIGGER tenant_branding_set_updated_at
    BEFORE UPDATE ON tenant_branding
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE tenant_branding ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant_branding
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- Seed one branding row per existing tenant, defaulting the identity fields
-- from that tenant's first (oldest) non-deleted company where available. The
-- LATERAL picks a single deterministic company; tenants with no company still
-- get an empty row. ON CONFLICT keeps this idempotent.
INSERT INTO tenant_branding
    (tenant_id, display_name, legal_name, tax_id, registration_no)
SELECT t.id, c.name, c.legal_name, c.tax_id, c.registration_no
FROM tenants t
LEFT JOIN LATERAL (
    SELECT name, legal_name, tax_id, registration_no
    FROM companies
    WHERE companies.tenant_id = t.id AND companies.status <> 'deleted'
    ORDER BY created_at ASC, id ASC
    LIMIT 1
) c ON true
ON CONFLICT (tenant_id) DO NOTHING;
