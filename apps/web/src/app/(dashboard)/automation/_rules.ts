import type { RiskSeverity } from '@fuelgrid/ui';

/**
 * The four code-backed evaluators the deterministic Automation Engine ships.
 * `condition` names an evaluator in the Go engine (NOT a free-typed expression),
 * so the New-rule dialog offers these as a closed set rather than a text field.
 */
export const CONDITION_KEYS = [
  'fuel_variance_over_tolerance',
  'repeated_cash_shortage',
  'stockout_coverage',
  'supplier_delivery_shortage',
] as const;

export type ConditionKey = (typeof CONDITION_KEYS)[number];

interface ConditionMeta {
  /** Human-readable label for the evaluator. */
  label: string;
  /** Plain-English description of what the rule watches for. */
  description: string;
  /** The category bucket the condition belongs to. */
  category: RuleCategory;
}

export type RuleCategory = 'inventory' | 'cash' | 'procurement' | 'general';

/** Display metadata for each known condition, keyed by the evaluator name. */
export const CONDITION_META: Record<string, ConditionMeta> = {
  fuel_variance_over_tolerance: {
    label: 'Fuel variance over tolerance',
    description: 'A tank reconciliation variance exceeds its loss tolerance.',
    category: 'inventory',
  },
  repeated_cash_shortage: {
    label: 'Repeated cash shortage',
    description: 'A station logs cash variances over the lookback window.',
    category: 'cash',
  },
  stockout_coverage: {
    label: 'Low stock coverage',
    description: "A tank's days-of-stock fall below the threshold.",
    category: 'inventory',
  },
  supplier_delivery_shortage: {
    label: 'Supplier delivery shortage',
    description: 'Delivered litres fall short of the ordered quantity.',
    category: 'procurement',
  },
};

/** A friendly label for a condition, falling back to the raw key. */
export function conditionLabel(condition?: string): string {
  if (!condition) return 'Custom condition';
  return CONDITION_META[condition]?.label ?? condition;
}

/** Ordered category buckets, so groups always render in a stable order. */
export const CATEGORY_ORDER: RuleCategory[] = ['inventory', 'cash', 'procurement', 'general'];

export const CATEGORY_LABEL: Record<RuleCategory, string> = {
  inventory: 'Inventory',
  cash: 'Cash',
  procurement: 'Procurement',
  general: 'General',
};

/** Coerce an arbitrary category string into one of the known buckets. */
export function normalizeCategory(category?: string): RuleCategory {
  if (category === 'inventory' || category === 'cash' || category === 'procurement') {
    return category;
  }
  return 'general';
}

/** The severities a rule can be set to, in escalating order. */
export const SEVERITIES: RiskSeverity[] = ['info', 'low', 'medium', 'high', 'critical'];

/** Map a (possibly unknown) severity string to a valid RiskSeverity. */
export function toRiskSeverity(sev?: string): RiskSeverity {
  return (SEVERITIES as string[]).includes(sev ?? '') ? (sev as RiskSeverity) : 'info';
}
