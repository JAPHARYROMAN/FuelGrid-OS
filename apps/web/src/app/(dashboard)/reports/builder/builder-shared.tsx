'use client';

import * as React from 'react';

import type {
  BuilderAggFunc,
  BuilderDataset,
  BuilderOperator,
  BuilderSpec,
  BuilderSpecFilter,
  ReportEnvelope,
} from '@fuelgrid/sdk';
import {
  AreaChart,
  BarChart,
  CategoricalBarChart,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  DonutChart,
  EmptyState,
  chartColors,
  type DonutSlice,
} from '@fuelgrid/ui';

import { formatLitres, formatMoney } from '@/lib/money';

/**
 * Shared presentation + safety helpers for the Custom Report Builder (Reports
 * Center Phase 11). The builder never composes SQL in the browser — it only ever
 * lets the user pick from the registry the backend returned (getBuilderDatasets),
 * and the backend re-validates every identifier against that allowlist before any
 * query runs. These helpers render the returned ReportEnvelope's `chart_data`
 * (the structured columns + rows + viz) through the same shared charts the
 * signature reports use, and format money/litre decimal STRINGS for display.
 */

/** The builder envelope's chart_data shape (reports_builder_handlers.go). */
export interface BuilderChartColumn {
  key: string;
  label: string;
  dimension: boolean;
  decimal: boolean;
  unit?: string;
}
export interface BuilderChartData {
  viz: 'table' | 'bar' | 'line' | 'pie';
  columns: BuilderChartColumn[];
  rows: string[][];
}

/** The visualizations the builder offers; each maps onto a shared chart. */
export const VIZ_OPTIONS: { value: NonNullable<BuilderSpec['viz']>; label: string }[] = [
  { value: 'table', label: 'Table' },
  { value: 'bar', label: 'Bar chart' },
  { value: 'line', label: 'Line / area' },
  { value: 'pie', label: 'Donut' },
];

/** Human label for an aggregate function. */
export const AGG_LABELS: Record<BuilderAggFunc, string> = {
  sum: 'Sum',
  avg: 'Average',
  count: 'Count',
  min: 'Minimum',
  max: 'Maximum',
};

/** Human label + arity for a filter operator. `in` and `between` take lists. */
export const OPERATOR_LABELS: Record<BuilderOperator, string> = {
  eq: 'equals',
  ne: 'not equals',
  gt: 'greater than',
  gte: 'at least',
  lt: 'less than',
  lte: 'at most',
  in: 'is one of',
  like: 'contains',
  between: 'between',
};

/** True when an operator carries a list value (`values`) instead of a scalar. */
export function operatorIsList(op: BuilderOperator): boolean {
  return op === 'in';
}
/** True when an operator carries a [lo, hi] pair (`values`). */
export function operatorIsRange(op: BuilderOperator): boolean {
  return op === 'between';
}

/** Coerce a decimal string to a finite number for chart GEOMETRY only. */
function num(v: string | undefined): number {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

/** Format a decimal cell by its declared unit (money / litres / verbatim). */
export function formatByUnit(value: string, unit?: string): string {
  const u = (unit ?? '').toUpperCase();
  if (u === 'TZS') return formatMoney(value);
  if (u === 'L') return formatLitres(value);
  return value;
}

/**
 * Render the builder result's chosen visualization. `table` is handled by the
 * shared EnvelopeTable elsewhere; here we render the bar / line / donut from the
 * structured chart_data. We chart the FIRST dimension column as the category and
 * the FIRST measure column as the value (the honest, predictable default for a
 * generic builder), and fall back to an honest empty state when the shape can't
 * be charted (no dimension, no numeric measure, or all-zero values).
 */
export function BuilderChart({ chart }: { chart: BuilderChartData }) {
  const dim = chart.columns.find((c) => c.dimension);
  const measureCols = chart.columns.filter((c) => !c.dimension);
  const measure = measureCols[0];

  if (!dim || !measure) {
    return (
      <EmptyState
        title="Not chartable"
        description="A chart needs at least one dimension (group-by) and one measure. Add one of each, or use the table view."
      />
    );
  }

  const dimIdx = chart.columns.indexOf(dim);
  const measureIdx = chart.columns.indexOf(measure);

  // Shape the rows into {category, value} records keyed by the column keys.
  const data = chart.rows.map((row) => ({
    category: row[dimIdx] ?? '',
    value: row[measureIdx] ?? '0',
  }));
  const total = data.reduce((s, d) => s + num(d.value), 0);

  if (data.length === 0 || total <= 0) {
    return (
      <EmptyState
        title="Nothing to chart"
        description="The query returned no rows with a non-zero value for the chosen measure."
      />
    );
  }

  const fmt = (v: unknown) => formatByUnit(String(v ?? ''), measure.unit);

  if (chart.viz === 'pie') {
    const slices: DonutSlice[] = data
      .slice(0, 12)
      .map((d, i) => ({ key: `${d.category}-${i}`, label: d.category || '—', value: d.value }));
    return <DonutChart slices={slices} valueFormatter={fmt} height={280} />;
  }

  if (chart.viz === 'line') {
    return (
      <AreaChart
        data={data}
        xKey="category"
        valueFormatter={fmt}
        series={[{ key: 'value', label: measure.label, color: chartColors.accent }]}
        height={280}
      />
    );
  }

  // Default bar: many categories read better as a horizontal (vertical-layout) bar.
  if (data.length > 6) {
    return (
      <CategoricalBarChart
        data={data}
        xKey="category"
        valueKey="value"
        label={measure.label}
        valueFormatter={fmt}
        layout="vertical"
        height={Math.min(80 + data.length * 28, 520)}
      />
    );
  }
  return (
    <BarChart
      data={data}
      xKey="category"
      valueFormatter={fmt}
      series={[{ key: 'value', label: measure.label, color: chartColors.accent }]}
      height={280}
    />
  );
}

/**
 * The builder result's visualization card: renders the chosen chart (when not a
 * table) above the always-present table elsewhere. Returns null for `table` (the
 * shared EnvelopeTable is the visualization in that case).
 */
export function BuilderVizCard({ env }: { env: ReportEnvelope }) {
  const chart = (env.chart_data as BuilderChartData | null) ?? null;
  if (!chart || chart.viz === 'table') return null;
  const label = VIZ_OPTIONS.find((v) => v.value === chart.viz)?.label ?? 'Chart';
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{label}</CardTitle>
      </CardHeader>
      <CardContent>
        <BuilderChart chart={chart} />
      </CardContent>
    </Card>
  );
}

/**
 * Export the builder result's TABLE as a CSV, client-side. The builder's
 * ReportEnvelope carries no server-side export_options (the unified export
 * endpoint only knows the fixed catalog reports, not custom specs), so we offer
 * an honest, decimal-safe CSV of exactly the rows shown — every cell is already a
 * string from the numeric::text contract, so no money is reparsed to a float.
 */
export function buildCsv(table: ReportEnvelope['table']): string {
  const cols = table?.columns ?? [];
  const rows = table?.rows ?? [];
  const esc = (v: string) => {
    const s = v ?? '';
    return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
  };
  const lines = [cols.map(esc).join(',')];
  for (const row of rows) lines.push(row.map(esc).join(','));
  return lines.join('\n');
}

/** Trigger a browser download for an in-memory CSV string. */
export function downloadCsv(filename: string, csv: string): void {
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

/** A short, human description of a spec's shape for the gallery cards. */
export function describeSpec(spec: BuilderSpec, dataset?: BuilderDataset): string {
  const dims = spec.dimensions.length;
  const measures = spec.measures.length;
  const filters = spec.filters?.length ?? 0;
  const parts: string[] = [];
  parts.push(dataset?.name ?? spec.dataset);
  parts.push(`${measures} measure${measures === 1 ? '' : 's'}`);
  if (dims) parts.push(`by ${dims} dimension${dims === 1 ? '' : 's'}`);
  if (filters) parts.push(`${filters} filter${filters === 1 ? '' : 's'}`);
  return parts.join(' · ');
}

/** Read a machine error `code` off an SdkError body, when present. */
export function errorCode(err: unknown): string | undefined {
  if (err && typeof err === 'object' && 'body' in err) {
    const body = (err as { body?: unknown }).body;
    if (body && typeof body === 'object' && 'code' in body) {
      const code = (body as { code?: unknown }).code;
      if (typeof code === 'string') return code;
    }
  }
  return undefined;
}

/** Render a list of filters as a compact, human summary for review. */
export function summarizeFilters(
  filters: BuilderSpecFilter[] | undefined,
  dataset: BuilderDataset | undefined,
): string[] {
  if (!filters || filters.length === 0) return [];
  return filters.map((f) => {
    const label = dataset?.filters.find((d) => d.id === f.filter)?.label ?? f.filter;
    const op = OPERATOR_LABELS[f.operator] ?? f.operator;
    let val = '';
    if (operatorIsList(f.operator) || operatorIsRange(f.operator)) {
      val = (f.values ?? [])
        .map((v) => String(v))
        .join(operatorIsRange(f.operator) ? ' and ' : ', ');
    } else {
      val = String(f.value ?? '');
    }
    return `${label} ${op} ${val}`.trim();
  });
}

void React;
