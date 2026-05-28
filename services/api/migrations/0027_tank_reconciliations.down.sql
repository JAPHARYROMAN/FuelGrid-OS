-- Reverse of 0027_tank_reconciliations.

DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions WHERE code IN ('reconciliation.read', 'reconciliation.manage')
);
DELETE FROM permissions WHERE code IN ('reconciliation.read', 'reconciliation.manage');

DROP TABLE IF EXISTS tank_reconciliations;
