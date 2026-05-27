-- 0004_rbac: permissions, roles, role grants, user roles, station access.
--
-- Scoping model (referenced by the policy evaluator):
--   • Permissions are the action vocabulary ("shift.close", "station.read").
--   • Roles bundle permissions. is_system=true rows with tenant_id IS NULL
--     are platform-level system roles, available to every tenant.
--   • user_roles grants a role to a user.
--   • user_station_access grants explicit station-level scope. A user with
--     NO user_station_access rows has tenant-wide reach for whatever
--     permissions their roles confer. A user with one or more rows is
--     restricted to those stations for station-scoped permissions.
--
-- The seed at the bottom of this file is platform-level (not tenant data),
-- which is why it lives in the schema migration: every database needs the
-- same permission codes and the same system role definitions.

-- ---------------------------------------------------------------------------
-- permissions — the platform's action vocabulary.
-- ---------------------------------------------------------------------------
CREATE TABLE permissions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code        text NOT NULL UNIQUE,
    description text NOT NULL,
    category    text NOT NULL,
    -- station_scoped = true means a station_id must accompany the check.
    -- Permissions like 'audit.read' or 'reports.export' are tenant-wide.
    station_scoped boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_permissions_category ON permissions(category);

-- ---------------------------------------------------------------------------
-- roles — named bundles of permissions. System roles (is_system=true,
-- tenant_id IS NULL) are platform-defined and shared by all tenants.
-- Custom tenant roles can be added later by setting tenant_id.
-- ---------------------------------------------------------------------------
CREATE TABLE roles (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid REFERENCES tenants(id) ON DELETE RESTRICT,
    code        text NOT NULL,
    name        text NOT NULL,
    description text,
    is_system   boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_roles_tenant_code UNIQUE (tenant_id, code),
    CONSTRAINT chk_roles_system_no_tenant CHECK (
        (is_system = true  AND tenant_id IS NULL) OR
        (is_system = false AND tenant_id IS NOT NULL)
    )
);

-- System role lookups are by code; tenant role lookups need tenant scoping.
CREATE INDEX idx_roles_tenant_id ON roles(tenant_id);
CREATE UNIQUE INDEX idx_roles_system_code ON roles(code) WHERE is_system;

CREATE TRIGGER roles_set_updated_at
    BEFORE UPDATE ON roles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- role_permissions — many-to-many between roles and permissions.
-- ---------------------------------------------------------------------------
CREATE TABLE role_permissions (
    role_id       uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id uuid NOT NULL REFERENCES permissions(id) ON DELETE RESTRICT,
    PRIMARY KEY (role_id, permission_id)
);

CREATE INDEX idx_role_permissions_permission_id ON role_permissions(permission_id);

-- ---------------------------------------------------------------------------
-- user_roles — which roles a user holds.
--   • System roles (tenant_id IS NULL on roles): user_roles.tenant_id is the
--     tenant the user actually operates in.
--   • Custom tenant roles: user_roles.tenant_id must equal roles.tenant_id;
--     enforced in app code.
-- ---------------------------------------------------------------------------
CREATE TABLE user_roles (
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id    uuid NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    tenant_id  uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    granted_by uuid REFERENCES users(id),
    granted_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX idx_user_roles_tenant_id ON user_roles(tenant_id);
CREATE INDEX idx_user_roles_role_id   ON user_roles(role_id);

-- ---------------------------------------------------------------------------
-- user_station_access — explicit station-level scope.
--   • A user with zero rows has TENANT-WIDE scope.
--   • A user with one or more rows is RESTRICTED to those stations for
--     any station_scoped permission.
-- ---------------------------------------------------------------------------
CREATE TABLE user_station_access (
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    station_id uuid NOT NULL REFERENCES stations(id) ON DELETE CASCADE,
    tenant_id  uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    granted_by uuid REFERENCES users(id),
    granted_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, station_id)
);

CREATE INDEX idx_user_station_access_tenant_id  ON user_station_access(tenant_id);
CREATE INDEX idx_user_station_access_station_id ON user_station_access(station_id);

-- ===========================================================================
-- Platform seeds — system permissions and roles.
-- ===========================================================================

INSERT INTO permissions (code, description, category, station_scoped) VALUES
    -- Station scope
    ('station.read',             'View station dashboards and details',         'station',      true),
    ('station.manage',           'Create, edit, suspend stations',              'station',      false),
    -- Shift / operations
    ('shift.open',               'Open shifts',                                 'shift',        true),
    ('shift.close',              'Close shifts',                                'shift',        true),
    ('shift.approve',            'Approve shifts',                              'shift',        true),
    ('reading.edit',             'Edit meter / tank readings',                  'shift',        true),
    -- Inventory
    ('stock.adjust',             'Create manual stock adjustments',             'inventory',    true),
    ('stock.approve_adjustment', 'Approve stock adjustments',                   'inventory',    true),
    -- Pricing
    ('price.change',             'Change fuel prices',                          'pricing',      true),
    -- Finance
    ('margin.view',              'View profit margin and financial data',       'finance',      true),
    ('credit.manage',            'Manage credit customers',                     'finance',      false),
    ('credit.override_limit',    'Override a credit customer''s limit',         'finance',      false),
    ('period.lock',              'Lock reporting periods',                      'finance',      false),
    -- Reports / audit
    ('reports.export',           'Export reports to PDF / Excel / CSV',         'reports',      false),
    ('audit.read',               'View audit logs',                             'audit',        false),
    -- Platform admin
    ('integrations.manage',      'Manage integration credentials',              'integrations', false),
    ('users.manage',             'Create / edit / deactivate users',            'admin',        false),
    ('users.assign_roles',       'Grant or revoke roles on users',              'admin',        false);

INSERT INTO roles (tenant_id, code, name, description, is_system) VALUES
    (NULL, 'attendant',           'Attendant',            'Operate assigned pump, submit readings and cash',     true),
    (NULL, 'supervisor',          'Supervisor',           'Assign attendants, open/close shifts, approve cash',  true),
    (NULL, 'station_manager',     'Station Manager',      'Run a station: deliveries, day close, expenses',      true),
    (NULL, 'regional_manager',    'Regional Manager',     'Oversee a region of stations',                        true),
    (NULL, 'finance_officer',     'Finance Officer',      'Reconcile cash, manage invoices and supplier bills',  true),
    (NULL, 'procurement_officer', 'Procurement Officer',  'Manage suppliers and purchase orders',                true),
    (NULL, 'auditor',             'Auditor',              'Read-only access to audit and risk surfaces',         true),
    (NULL, 'executive',           'Executive',            'Network-wide visibility and strategic decisions',     true),
    (NULL, 'system_admin',        'System Administrator', 'Configure tenants, users, integrations, security',    true);

-- Helper: insert role_permissions by code lookup.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND (
    -- attendant: intentionally minimal at this stage; expanded in later
    -- phases when pump/shift workflows arrive.
    (r.code = 'attendant' AND p.code IN ('shift.open'))

    OR (r.code = 'supervisor' AND p.code IN (
        'station.read', 'shift.open', 'shift.close', 'shift.approve', 'reading.edit'
    ))

    OR (r.code = 'station_manager' AND p.code IN (
        'station.read', 'shift.open', 'shift.close', 'shift.approve', 'reading.edit',
        'stock.adjust', 'stock.approve_adjustment',
        'margin.view', 'reports.export'
    ))

    OR (r.code = 'regional_manager' AND p.code IN (
        'station.read', 'station.manage',
        'shift.approve', 'reading.edit',
        'stock.adjust', 'stock.approve_adjustment',
        'price.change',
        'margin.view', 'reports.export'
    ))

    OR (r.code = 'finance_officer' AND p.code IN (
        'station.read',
        'margin.view',
        'credit.manage', 'credit.override_limit',
        'reports.export', 'period.lock'
    ))

    OR (r.code = 'procurement_officer' AND p.code IN (
        'station.read', 'reports.export'
    ))

    OR (r.code = 'auditor' AND p.code IN (
        'station.read', 'audit.read', 'margin.view', 'reports.export'
    ))

    OR (r.code = 'executive' AND p.code IN (
        'station.read', 'station.manage',
        'margin.view', 'reports.export', 'audit.read'
    ))

    -- system_admin: everything.
    OR (r.code = 'system_admin')
);
