ALTER TABLE employees DROP CONSTRAINT IF EXISTS employees_role_fk;

DROP TRIGGER IF EXISTS tenants_seed_employee_roles ON tenants;
DROP FUNCTION IF EXISTS seed_default_employee_roles();

UPDATE employees
SET role = 'other'
WHERE role NOT IN ('pump_attendant', 'cashier', 'supervisor', 'manager', 'other');

ALTER TABLE employees DROP CONSTRAINT IF EXISTS chk_employees_role;

ALTER TABLE employees
    ADD CONSTRAINT chk_employees_role
        CHECK (role IN ('pump_attendant', 'cashier', 'supervisor', 'manager', 'other'));

DROP TABLE IF EXISTS employee_roles;
