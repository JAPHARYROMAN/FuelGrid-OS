-- 0001_init: tenants, companies, regions, stations, and shared helpers.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Reusable trigger function: bumps updated_at on any UPDATE. Created here
-- so every later migration can hang triggers off it without redefining.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------------------
-- tenants — top of the hierarchy. A tenant is one customer of FuelGrid OS.
-- Tenants are never hard-deleted; the status column expresses lifecycle.
-- ---------------------------------------------------------------------------
CREATE TABLE tenants (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    slug        text NOT NULL,
    status      text NOT NULL DEFAULT 'active',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_tenants_status CHECK (status IN ('active', 'suspended', 'deleted')),
    CONSTRAINT chk_tenants_slug   CHECK (slug ~ '^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$')
);

CREATE UNIQUE INDEX idx_tenants_slug ON tenants(slug) WHERE status <> 'deleted';

CREATE TRIGGER tenants_set_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- companies — legal entities under a tenant. Most tenants have one company;
-- enterprise tenants may operate several under one umbrella.
-- ---------------------------------------------------------------------------
CREATE TABLE companies (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    name            text NOT NULL,
    legal_name      text,
    registration_no text,
    tax_id          text,
    currency        text NOT NULL DEFAULT 'USD',
    timezone        text NOT NULL DEFAULT 'UTC',
    status          text NOT NULL DEFAULT 'active',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_companies_status   CHECK (status IN ('active', 'suspended', 'deleted')),
    CONSTRAINT chk_companies_currency CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE INDEX idx_companies_tenant_id ON companies(tenant_id);
CREATE UNIQUE INDEX idx_companies_tenant_name
    ON companies(tenant_id, lower(name)) WHERE status <> 'deleted';

CREATE TRIGGER companies_set_updated_at
    BEFORE UPDATE ON companies
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- regions — geographic / operational groupings of stations under a company.
-- Optional in the hierarchy; small tenants may have stations without regions.
-- ---------------------------------------------------------------------------
CREATE TABLE regions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    company_id  uuid NOT NULL REFERENCES companies(id) ON DELETE RESTRICT,
    name        text NOT NULL,
    code        text,
    status      text NOT NULL DEFAULT 'active',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_regions_status CHECK (status IN ('active', 'suspended', 'deleted'))
);

CREATE INDEX idx_regions_tenant_id  ON regions(tenant_id);
CREATE INDEX idx_regions_company_id ON regions(company_id);
CREATE UNIQUE INDEX idx_regions_company_name
    ON regions(company_id, lower(name)) WHERE status <> 'deleted';

CREATE TRIGGER regions_set_updated_at
    BEFORE UPDATE ON regions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- stations — physical fueling locations. Tank/pump/shift entities all FK here.
-- `code` is the human-facing identifier ("MIK-01"); `id` is for joins.
-- ---------------------------------------------------------------------------
CREATE TABLE stations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    company_id      uuid NOT NULL REFERENCES companies(id) ON DELETE RESTRICT,
    region_id       uuid REFERENCES regions(id) ON DELETE SET NULL,
    name            text NOT NULL,
    code            text NOT NULL,
    address_line1   text,
    address_line2   text,
    city            text,
    state           text,
    country         text,
    latitude        numeric(10, 7),
    longitude       numeric(10, 7),
    timezone        text NOT NULL DEFAULT 'UTC',
    status          text NOT NULL DEFAULT 'active',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_stations_status    CHECK (status IN ('active', 'suspended', 'closed', 'deleted')),
    CONSTRAINT chk_stations_latitude  CHECK (latitude  IS NULL OR (latitude  BETWEEN -90  AND 90)),
    CONSTRAINT chk_stations_longitude CHECK (longitude IS NULL OR (longitude BETWEEN -180 AND 180))
);

CREATE INDEX idx_stations_tenant_id  ON stations(tenant_id);
CREATE INDEX idx_stations_company_id ON stations(company_id);
CREATE INDEX idx_stations_region_id  ON stations(region_id);
CREATE UNIQUE INDEX idx_stations_tenant_code
    ON stations(tenant_id, code) WHERE status <> 'deleted';

CREATE TRIGGER stations_set_updated_at
    BEFORE UPDATE ON stations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
