-- 0084_rules_engine (down): remove the configurable-engine columns and the
-- four seeded default rules. Symmetric with the up migration; up->down->up is
-- clean because the seed uses ON CONFLICT DO NOTHING and the down DELETE keys
-- on the same four codes.

-- Drop the four seeded default rules (across all tenants).
DELETE FROM risk_rules WHERE code IN (
    'fuel_variance_over_tolerance',
    'repeated_cash_shortage',
    'stockout_coverage',
    'supplier_delivery_shortage'
);

-- risk_alerts additions.
DROP INDEX IF EXISTS idx_risk_alerts_rule_id;
ALTER TABLE risk_alerts
    DROP CONSTRAINT IF EXISTS risk_alerts_rule_fk,
    DROP COLUMN IF EXISTS recommended_action,
    DROP COLUMN IF EXISTS rule_id;

-- risk_rules additions.
ALTER TABLE risk_rules DROP CONSTRAINT IF EXISTS uq_risk_rules_tenant_id;
ALTER TABLE risk_rules
    DROP COLUMN IF EXISTS enabled,
    DROP COLUMN IF EXISTS recommended_action,
    DROP COLUMN IF EXISTS message_template,
    DROP COLUMN IF EXISTS comparison_period_days,
    DROP COLUMN IF EXISTS condition,
    DROP COLUMN IF EXISTS category;
