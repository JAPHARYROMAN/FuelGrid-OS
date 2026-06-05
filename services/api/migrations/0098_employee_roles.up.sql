CREATE TABLE employee_roles (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    code        text NOT NULL,
    name        text NOT NULL,
    is_default  boolean NOT NULL DEFAULT false,
    status      text NOT NULL DEFAULT 'active',
    sort_order  integer NOT NULL DEFAULT 1000,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_employee_roles_code
        CHECK (code ~ '^[a-z0-9][a-z0-9_]{0,63}$'),
    CONSTRAINT chk_employee_roles_name
        CHECK (btrim(name) <> ''),
    CONSTRAINT chk_employee_roles_status
        CHECK (status IN ('active', 'inactive')),
    CONSTRAINT uq_employee_roles_code UNIQUE (tenant_id, code),
    CONSTRAINT uq_employee_roles_tenant_id UNIQUE (tenant_id, id)
);

CREATE INDEX idx_employee_roles_tenant_id ON employee_roles(tenant_id);

CREATE TRIGGER employee_roles_set_updated_at
    BEFORE UPDATE ON employee_roles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE employee_roles ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON employee_roles
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE OR REPLACE FUNCTION seed_default_employee_roles()
RETURNS trigger AS $$
BEGIN
    INSERT INTO employee_roles (tenant_id, code, name, is_default, sort_order)
    VALUES
        (NEW.id, 'pump_attendant', 'Pump attendant', true, 10),
        (NEW.id, 'cashier', 'Cashier', true, 20),
        (NEW.id, 'supervisor', 'Supervisor', true, 30),
        (NEW.id, 'manager', 'Manager', true, 40),
        (NEW.id, 'security', 'Security', true, 50),
        (NEW.id, 'other', 'Other', true, 60)
    ON CONFLICT (tenant_id, code) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER tenants_seed_employee_roles
    AFTER INSERT ON tenants
    FOR EACH ROW EXECUTE FUNCTION seed_default_employee_roles();

INSERT INTO employee_roles (tenant_id, code, name, is_default, sort_order)
SELECT t.id, v.code, v.name, true, v.sort_order
FROM tenants t
CROSS JOIN (
    VALUES
        ('pump_attendant', 'Pump attendant', 10),
        ('cashier', 'Cashier', 20),
        ('supervisor', 'Supervisor', 30),
        ('manager', 'Manager', 40),
        ('security', 'Security', 50),
        ('other', 'Other', 60)
) AS v(code, name, sort_order)
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO employee_roles (tenant_id, code, name, is_default, sort_order)
SELECT DISTINCT e.tenant_id, e.role, initcap(replace(e.role, '_', ' ')), false, 1000
FROM employees e
LEFT JOIN employee_roles er
    ON er.tenant_id = e.tenant_id AND er.code = e.role
WHERE er.id IS NULL;

ALTER TABLE employees DROP CONSTRAINT IF EXISTS chk_employees_role;

ALTER TABLE employees
    ADD CONSTRAINT employees_role_fk
        FOREIGN KEY (tenant_id, role) REFERENCES employee_roles(tenant_id, code)
        ON UPDATE RESTRICT
        ON DELETE RESTRICT;
