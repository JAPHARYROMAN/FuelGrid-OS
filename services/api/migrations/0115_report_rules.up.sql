-- 0115_report_rules: the config-driven, tunable, auditable REPORT INSIGHT rules
-- engine (Reports Center Phase 15 — blueprint §9 "Automated Report Intelligence
-- Without AI", §9.3 Insight Rule Engine, §21.3, §23).
--
-- This brings the report INSIGHT layer under one configurable surface — exactly
-- like risk_rules (migration 0084) but for report insights instead of risk
-- alerts. The `condition` column names a deterministic, code-backed Go evaluator
-- registered in internal/reportrules (NOT a free-form expression, NO AI); each
-- evaluator reads the report's already-computed figures and either fires (with a
-- {token}-substituted message) or stays silent. Operators configure threshold,
-- comparison window, severity, message_template, recommended_action, placement,
-- and enabled — and disabling a rule actually removes its insight.
--
-- ADDITIVE / NO-REGRESSION DESIGN. The hardcoded deterministic composers in
-- internal/reporting (DailyClose, SalesSummary, Executive, RiskLoss, ...) REMAIN
-- the byte-identical source of truth for every insight they emit today. This
-- engine AUGMENTS that output: a tenant can tune a seeded rule, disable it, or
-- add a new rule, and those changes fold into the same insights[] /
-- recommended_actions[] the composer already produces. The SYSTEM rules seeded
-- below carry is_system = true and DEFAULT to "augment-off" (mode='shadow') so a
-- fresh tenant's default report output is IDENTICAL to pre-Phase-15 — the seed
-- documents and mirrors the composer thresholds for tuning, while the composer
-- stays authoritative. A rule the tenant flips to mode='augment' (or any custom
-- rule they add) then contributes an additional insight line. This is the safe
-- design the golden no-regression tests lock.

-- ---------------------------------------------------------------------------
-- reports.rules.manage — the tenant-wide MANAGE gate for tuning report rules.
-- Mirrors risk_rule.manage. Granted to the same management roles that already
-- hold reports.schedule / reports.export and a reason to tune insight thresholds.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('reports.rules.manage', 'View and tune report insight rules', 'reports', false)
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'reports.rules.manage'
  AND r.code IN (
      'system_admin', 'finance_officer', 'regional_manager', 'executive', 'station_manager'
  )
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- report_rules — the per-tenant report-insight rule definitions. Mirrors the
-- risk_rules shape (code/condition/threshold/comparison_period_days/severity/
-- message_template/recommended_action/enabled/is_system/status) and adds the
-- report-insight-specific report_key (nullable = applies broadly) +
-- report_placement (where the fired insight surfaces) + threshold_config jsonb
-- (the structured, multi-field threshold surface the simple `threshold` decimal
-- cannot hold).
-- ---------------------------------------------------------------------------
CREATE TABLE report_rules (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id              uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,

    -- Identity: code is unique per tenant (the stable handle the API + seed use).
    code                   text  NOT NULL,
    name                   text  NOT NULL,
    description            text,

    -- WHICH report this rule annotates. NULL = applies broadly to every report
    -- whose facts satisfy the evaluator (a cross-report rule, e.g. period locking).
    report_key             text,
    -- A coarse grouping for the management UI (sales | cash | inventory | credit |
    -- procurement | risk | executive | general).
    category               text  NOT NULL DEFAULT 'general',

    -- The registered, deterministic evaluator key (internal/reportrules). An
    -- unknown condition is simply skipped (never an error) — the engine degrades
    -- safely, exactly like the risk registry.
    condition              text  NOT NULL,

    -- Operator-configurable thresholds. `threshold` is the single decimal scalar
    -- (kept as text so it stays off the float path, NULL when unset);
    -- threshold_config is the structured multi-field surface (e.g.
    -- {"pct":25,"min_base":0}) an evaluator reads when one scalar is not enough.
    threshold              numeric,
    threshold_config       jsonb NOT NULL DEFAULT '{}'::jsonb,
    comparison_period_days integer,

    severity               text  NOT NULL DEFAULT 'info',
    -- The {token} message template rendered with the evaluator's vars (SAFE — plain
    -- {token} string substitution in Go, no eval, no injection surface).
    message_template       text  NOT NULL,
    recommended_action     text,

    -- WHERE a fired insight surfaces on the report: insight (the insights[] list,
    -- the default) | data_quality (the data-quality band) | summary (a KPI note).
    report_placement       text  NOT NULL DEFAULT 'insight',

    -- HOW a system rule participates by default:
    --   shadow  — evaluated for audit/preview but NOT folded into the envelope, so
    --             the composer stays the byte-identical source of truth (the seed
    --             default for is_system rules — guarantees no regression).
    --   augment — the fired insight IS folded into the envelope (tenant-tuned rules
    --             and custom rules; a tenant may flip a system rule to this).
    mode                   text  NOT NULL DEFAULT 'augment',

    -- Whether a fired insight at this severity emits an in-app notification
    -- (opt-in, off by default — the engine never spams).
    notify_on_fire         boolean NOT NULL DEFAULT false,

    -- is_system marks a seeded rule (mirrors a composer threshold); is_system rows
    -- cannot be deleted via the API (only disabled / re-moded), so the seed set is
    -- always restorable.
    is_system              boolean NOT NULL DEFAULT false,

    enabled                boolean NOT NULL DEFAULT true,
    status                 text  NOT NULL DEFAULT 'active',

    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_report_rules_tenant_code UNIQUE (tenant_id, code),
    CONSTRAINT chk_report_rules_severity
        CHECK (severity IN ('info', 'warning', 'critical')),
    CONSTRAINT chk_report_rules_placement
        CHECK (report_placement IN ('insight', 'data_quality', 'summary')),
    CONSTRAINT chk_report_rules_mode
        CHECK (mode IN ('shadow', 'augment')),
    CONSTRAINT chk_report_rules_status
        CHECK (status IN ('draft', 'active', 'paused', 'retired'))
);

-- The management list (by report, then code) and the per-report evaluation lookup
-- (the engine selects enabled rules for a report_key OR the broad NULL set).
CREATE INDEX idx_report_rules_tenant_report
    ON report_rules (tenant_id, report_key, code);
CREATE INDEX idx_report_rules_eval
    ON report_rules (tenant_id, report_key)
    WHERE enabled = true AND status = 'active';

CREATE TRIGGER report_rules_set_updated_at
    BEFORE UPDATE ON report_rules
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE report_rules ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON report_rules
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Seed the SYSTEM rules for every existing tenant. Each row mirrors a threshold
-- a composer already enforces today (the `description` names the composer rule it
-- reproduces). They seed with mode='shadow' so DEFAULT report output is unchanged
-- (the composer remains authoritative); a tenant flips a row to mode='augment' to
-- have the engine contribute the line, or tunes the threshold and the engine
-- folds the tuned line in. cmd/seed inserts the same set for new tenants;
-- ON CONFLICT keeps this idempotent.
-- ---------------------------------------------------------------------------
INSERT INTO report_rules
    (tenant_id, code, name, description, report_key, category, condition,
     threshold, threshold_config, comparison_period_days, severity,
     message_template, recommended_action, report_placement, mode, is_system,
     enabled, status)
SELECT t.id, v.code, v.name, v.description, v.report_key, v.category, v.condition,
       v.threshold, v.threshold_config::jsonb, v.comparison_period_days, v.severity,
       v.message_template, v.recommended_action, v.report_placement, 'shadow', true,
       true, 'active'
FROM tenants t
CROSS JOIN (VALUES
    -- Period-over-period gross swing (sales / daily-close). Composer:
    -- PeriodOverPeriod + severityForDeltaPct (>=25% warns).
    ('gross_swing', 'Gross revenue swing', 'sales', 'sales',
     'period_over_period', 25::numeric, '{"metric":"Gross revenue","warn_pct":25}', NULL::integer,
     'warning', '{metric} moved {direction} {pct}% vs the prior period.',
     'Confirm the day''s transactions before relying on the swing.', 'insight',
     'Mirrors the PeriodOverPeriod composer: a period-over-period gross swing past the warn threshold.'),
    -- Variance-vs-recent-average on gross (sales / daily-close). Composer:
    -- VarianceVs30dAverage(threshold 20).
    ('gross_variance', 'Gross vs recent average', 'sales', 'sales',
     'variance_vs_average', 20::numeric, '{"metric":"Gross revenue","warn_pct":20}', 30,
     'warning', '{metric} is {pct}% vs its recent average — an unusual reading.',
     'Confirm the underlying transactions before relying on this figure.', 'insight',
     'Mirrors the VarianceVs30dAverage composer: the latest gross deviates from its window average past the threshold.'),
    -- Cash variance over tolerance (daily-close / cash-recon). Composer:
    -- cashVarianceInsight (any non-zero over tolerance warns; >2x tolerance critical).
    ('cash_variance', 'Cash variance over tolerance', 'cash', 'cash-reconciliation',
     'cash_variance_over_tolerance', NULL::numeric, '{"critical_multiple":2}', NULL,
     'warning', 'Cash drawer is off by {variance} — beyond tolerance.',
     'Reconcile the drawer and confirm the tender breakdown before locking the day.', 'insight',
     'Mirrors the cashVarianceInsight composer: a cash variance beyond the configured tolerance.'),
    -- Tank reconciliation over tolerance (stock-recon). Composer:
    -- tankOverTolerance (|variance%| > tolerance%).
    ('tank_over_tolerance', 'Tank variance over tolerance', 'inventory', 'inventory-reconciliation',
     'tank_over_tolerance', NULL::numeric, '{}', NULL,
     'warning', '{count} tank(s) exceeded their variance tolerance.',
     'Investigate possible loss, theft, or a miscalibrated dip.', 'insight',
     'Mirrors the StockReconciliation composer: one or more tanks over their variance tolerance.'),
    -- Negative / shrinking margin (sales / profitability). Composer:
    -- marginInsight (negative critical; <=-15% contraction warns).
    ('margin_health', 'Margin health', 'sales', 'sales',
     'margin_health', 15::numeric, '{"contract_pct":15}', NULL,
     'critical', 'Latest margin is negative — sales are running below cost.',
     'Review pump pricing and COGS for the period.', 'insight',
     'Mirrors the marginInsight composer: a negative latest margin or a steep contraction vs the prior period.'),
    -- Overdue / concentrated receivables (customer-credit / aging). Composer:
    -- CustomerCredit overdue share (>=50% of outstanding -> critical) + concentration.
    ('overdue_receivables', 'Overdue receivables share', 'credit', 'customer-credit',
     'overdue_share', 50::numeric, '{"critical_pct":50}', NULL,
     'warning', '{overdue} of receivables is overdue ({pct}% of outstanding).',
     'Chase the overdue balances and review the affected customers'' credit standing.', 'insight',
     'Mirrors the CustomerCredit composer: the overdue share of outstanding receivables (critical past the threshold).'),
    -- Supplier delivery shortfall (delivery). Composer: Delivery ordered-vs-received
    -- (>=5% short warns).
    ('delivery_shortfall', 'Delivery shortfall', 'procurement', 'delivery',
     'delivery_shortfall', 5::numeric, '{"warn_pct":5}', NULL,
     'warning', 'Received {shortfall} L less than ordered this period ({pct}% of the ordered volume).',
     'Reconcile short deliveries with the supplier and confirm the goods-receipt dips.', 'insight',
     'Mirrors the Delivery composer: net received litres short of ordered past the threshold.'),
    -- Period not yet locked → provisional (any report). Composer: every report''s
    -- "period is not locked" data-quality note.
    ('period_unlocked', 'Period not locked', 'general', NULL,
     'period_unlocked', NULL::numeric, '{}', NULL,
     'info', 'This period is not locked yet, so its totals are provisional.',
     NULL, 'data_quality',
     'Mirrors the shared period-lock data-quality note: the period is still open and figures are provisional.')
) AS v(code, name, category, report_key, condition, threshold, threshold_config,
       comparison_period_days, severity, message_template, recommended_action,
       report_placement, description)
ON CONFLICT (tenant_id, code) DO NOTHING;

-- NOTE: the rules-tuning surface is an ADMIN/settings page (gated by
-- reports.rules.manage), reachable directly at /reports/rules; it is deliberately
-- NOT registered as one of the 16 blueprint report_categories cards, so the
-- catalog contract (exactly the 16 categories) is unchanged.
