-- 0084_rules_engine: the configurable Rules & Insights Engine (Workstream D).
--
-- This turns risk detection from three HARDCODED packs (RISK-001/RISK-002 —
-- detection ignored rule config, so pausing a rule was a no-op) into a
-- registry of named, code-backed evaluators driven entirely by per-tenant
-- rule rows. The `condition` column names a deterministic evaluator (NOT a
-- free-form expression); operators configure threshold, comparison window,
-- severity, message_template, recommended_action, and enabled. Each evaluator
-- runs deterministic SQL and is fully auditable/explainable — no AI.
--
-- The added columns are additive: lookback_days and the old rule_code path on
-- risk_alerts are kept for back-compat. comparison_period_days supersedes
-- lookback_days going forward (backfilled from it).

-- ---------------------------------------------------------------------------
-- risk_rules: configurable evaluator-driven schema.
-- ---------------------------------------------------------------------------
ALTER TABLE risk_rules
    ADD COLUMN category              text    NOT NULL DEFAULT 'general',
    ADD COLUMN condition             text,            -- named evaluator key
    ADD COLUMN comparison_period_days integer,        -- nullable window in days
    ADD COLUMN message_template      text,            -- with {placeholder} tokens
    ADD COLUMN recommended_action    text,
    ADD COLUMN enabled               boolean NOT NULL DEFAULT true;

-- Backfill condition from the legacy hardcoded rule codes so existing tenant
-- rules keep firing under the new registry.
UPDATE risk_rules SET condition = 'fuel_variance_over_tolerance'
    WHERE condition IS NULL AND code = 'fuel_loss';
UPDATE risk_rules SET condition = 'repeated_cash_shortage'
    WHERE condition IS NULL AND code = 'cash_shortage';
UPDATE risk_rules SET condition = 'supplier_delivery_shortage'
    WHERE condition IS NULL AND code = 'delivery_discrepancy';

-- comparison_period_days supersedes lookback_days; backfill the window.
UPDATE risk_rules SET comparison_period_days = lookback_days
    WHERE comparison_period_days IS NULL;

-- enabled tracks whether a rule should run: draft/active rules are enabled,
-- paused/retired are not (RunDetection also re-checks status at run time).
UPDATE risk_rules SET enabled = (status IN ('active', 'draft'));

-- Composite tenant key on risk_rules so risk_alerts can carry a tenant-bound
-- FK to the firing rule (matches the (tenant_id, id) FK convention elsewhere).
ALTER TABLE risk_rules ADD CONSTRAINT uq_risk_rules_tenant_id UNIQUE (tenant_id, id);

-- ---------------------------------------------------------------------------
-- risk_alerts: link to the firing rule and carry the recommended action.
-- detail stays the rendered human message.
-- ---------------------------------------------------------------------------
ALTER TABLE risk_alerts
    ADD COLUMN rule_id            uuid,
    ADD COLUMN recommended_action text,
    ADD CONSTRAINT risk_alerts_rule_fk
        FOREIGN KEY (tenant_id, rule_id) REFERENCES risk_rules(tenant_id, id) ON DELETE SET NULL;

CREATE INDEX idx_risk_alerts_rule_id ON risk_alerts(rule_id) WHERE rule_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Seed the FOUR default rules for every existing tenant. The web/API and
-- cmd/seed insert the same set; ON CONFLICT keeps this idempotent.
-- ---------------------------------------------------------------------------
INSERT INTO risk_rules
    (tenant_id, code, name, rule_type, status, category, condition,
     threshold, lookback_days, comparison_period_days, severity,
     message_template, recommended_action, enabled, description)
SELECT t.id, v.code, v.name, 'threshold', 'active', v.category, v.condition,
       v.threshold, v.comparison_period_days, v.comparison_period_days, v.severity,
       v.message_template, v.recommended_action, true, v.description
FROM tenants t
CROSS JOIN (VALUES
    ('fuel_variance_over_tolerance', 'Fuel variance over tolerance', 'inventory',
     'fuel_variance_over_tolerance', NULL::numeric, 30,
     'high', '{product} variance exceeded tolerance by {variance_litres} L.',
     'Review tank dip, pump readings, and delivery records.',
     'Fires when a tank reconciliation variance exceeds the product loss tolerance (or the rule threshold in litres when set).'),
    ('repeated_cash_shortage', 'Repeated cash shortage', 'cash',
     'repeated_cash_shortage', 3::numeric, 7,
     'high', 'Attendant {attendant} has repeated shortages across {count} shifts in {days} days.',
     'Review cash submissions and supervisor approvals.',
     'Fires when an attendant has at least the threshold number of cash shortages within the comparison window.'),
    ('stockout_coverage', 'Stockout coverage', 'inventory',
     'stockout_coverage', 2::numeric, 14,
     'medium', '{product} may reach minimum level within ~{hours} hours.',
     'Create a purchase order or schedule a delivery.',
     'Fires when projected days of cover (on-hand litres / average daily sales) fall below the threshold.'),
    ('supplier_delivery_shortage', 'Supplier delivery shortage', 'procurement',
     'supplier_delivery_shortage', NULL::numeric, 30,
     'high', 'Supplier {supplier} delivery shortage of {shortage_litres} L detected.',
     'Flag delivery for dispute before supplier invoice approval.',
     'Fires when received litres fall short of ordered litres by more than the tolerance fraction (rule threshold).')
) AS v(code, name, category, condition, threshold, comparison_period_days,
       severity, message_template, recommended_action, description)
ON CONFLICT (tenant_id, code) DO NOTHING;
