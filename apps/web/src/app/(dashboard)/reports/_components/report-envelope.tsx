'use client';

import * as React from 'react';
import Link from 'next/link';
import { ArrowUpRight } from 'lucide-react';

import type { ReportEnvelope, ReportExportOption, ReportSummaryMetric } from '@fuelgrid/sdk';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  DataQualityCard,
  type DataQualityLevel,
  ExportButtonGroup,
  type ExportAction,
  type ExportFormat,
  InsightCard,
  MetricCard,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatLitres, formatMoney } from '@/lib/money';

/**
 * Shared presentation for the structured ReportEnvelope (report_envelope.go).
 *
 * The envelope endpoints all return the same drillable shape — a headline
 * summary, a report-specific chart payload, a generic table, deterministic
 * insights + data-quality warnings, drill-down links and export affordances.
 * These helpers render each slice consistently so the signature report views
 * only have to supply the chart (the one report-specific visual).
 *
 * Money/litre figures arrive as exact decimal strings; we format for display
 * via formatMoney/formatLitres and never coerce them back into business math.
 */

/** Format a summary metric's string value by its declared unit. */
export function formatMetricValue(m: ReportSummaryMetric): string {
  const unit = (m.unit ?? '').toUpperCase();
  if (unit === 'TZS') return formatMoney(m.value);
  if (unit === 'L') return formatLitres(m.value);
  // count / status / unitless — show verbatim.
  return m.value;
}

/** Map a metric's `direction` onto the MetricCard trend vocabulary. */
function trendFor(direction?: string): 'up' | 'down' | 'flat' {
  if (direction === 'up' || direction === 'positive') return 'up';
  if (direction === 'down' || direction === 'negative') return 'down';
  return 'flat';
}

/** The headline summary metrics → a responsive grid of MetricCards. */
export function SummaryGrid({ summary }: { summary: ReportSummaryMetric[] }) {
  if (summary.length === 0) return null;
  return (
    <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
      {summary.map((m) => (
        <MetricCard
          key={m.label}
          label={m.label}
          value={formatMetricValue(m)}
          unit={m.unit && m.unit !== 'count' ? m.unit : undefined}
          trend={m.delta ? trendFor(m.direction) : undefined}
          trendValue={m.delta || undefined}
        />
      ))}
    </section>
  );
}

/** The data-quality items → a single banner-style card (highest level wins). */
export function DataQualityPanel({ items }: { items: ReportEnvelope['data_quality'] }) {
  if (!items || items.length === 0) return null;
  const rank: Record<string, number> = { info: 0, warning: 1, critical: 2 };
  let level: DataQualityLevel = 'info';
  for (const it of items) {
    const lv = (it.level || 'warning') as DataQualityLevel;
    if ((rank[lv] ?? 1) >= (rank[level] ?? 0)) level = lv;
  }
  return <DataQualityCard level={level} messages={items.map((d) => d.message)} />;
}

/**
 * The deterministic insights + recommended actions → InsightCards. Insights
 * carry their own severity; standalone recommended_actions (not already paired
 * to an insight) render as info cards so nothing is dropped.
 */
export function InsightPanel({
  insights,
  recommendedActions,
}: {
  insights: ReportEnvelope['insights'];
  recommendedActions: string[];
}) {
  const paired = new Set(insights.map((i) => i.recommended_action).filter((a): a is string => !!a));
  const extras = (recommendedActions ?? []).filter((a) => !paired.has(a));
  if (insights.length === 0 && extras.length === 0) return null;
  return (
    <div className="flex flex-col gap-2.5">
      {insights.map((ins, i) => (
        <InsightCard
          key={`ins-${i}`}
          severity={
            ins.severity === 'critical'
              ? 'critical'
              : ins.severity === 'warning'
                ? 'warning'
                : 'info'
          }
          message={ins.message}
          recommendedAction={ins.recommended_action}
        />
      ))}
      {extras.map((a, i) => (
        <InsightCard key={`rec-${i}`} severity="info" label="Recommended" message={a} />
      ))}
    </div>
  );
}

/** Decide how to format a table cell from its column name. */
function formatCell(column: string, value: string): React.ReactNode {
  const lc = column.toLowerCase();
  if (value === '' || value == null) return <span className="text-muted-foreground">—</span>;
  if (/litre|sales|deliveries|opening|closing|adjustments/.test(lc) && !/pct|percent/.test(lc)) {
    return formatLitres(value);
  }
  if (
    /gross|net|margin|tender|cash|expected|submitted|deposited|variance$|shortage|excess|value|balance/.test(
      lc,
    ) &&
    !/pct|percent|_pct/.test(lc)
  ) {
    // Money columns — but variance_litres is litres; guard that above.
    if (lc.includes('variance') && lc.includes('litre')) return formatLitres(value);
    return formatMoney(value);
  }
  if (/pct|percent/.test(lc)) return `${value}%`;
  return value;
}

/** Right-align numeric/measure columns. */
function isNumericColumn(column: string): boolean {
  return /litre|sales|deliveries|opening|closing|adjustments|gross|net|margin|tender|cash|expected|submitted|deposited|variance|shortage|excess|value|balance|pct|percent|tolerance/.test(
    column.toLowerCase(),
  );
}

/** The generic, drillable grid → a Table. Every cell is a decimal-safe string. */
export function EnvelopeTable({
  table,
  caption,
}: {
  table: ReportEnvelope['table'];
  caption?: string;
}) {
  const cols = table?.columns ?? [];
  const rows = table?.rows ?? [];
  return (
    <Card>
      {caption ? (
        <CardHeader>
          <CardTitle>{caption}</CardTitle>
        </CardHeader>
      ) : null}
      <CardContent className="p-0">
        {rows.length === 0 ? (
          <p className="p-6 text-sm text-muted-foreground">No rows for this report yet.</p>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  {cols.map((c) => (
                    <TableHead key={c} className={isNumericColumn(c) ? 'text-right' : undefined}>
                      {prettyColumn(c)}
                    </TableHead>
                  ))}
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((row, ri) => (
                  <TableRow key={ri}>
                    {row.map((cell, ci) => {
                      const col = cols[ci] ?? '';
                      return (
                        <TableCell
                          key={ci}
                          className={
                            isNumericColumn(col) ? 'text-right font-mono tabular-nums' : undefined
                          }
                        >
                          {formatCell(col, cell)}
                        </TableCell>
                      );
                    })}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

/** Humanize a snake_case column name. */
function prettyColumn(c: string): string {
  return c
    .replace(/_/g, ' ')
    .replace(/\bpct\b/g, '%')
    .replace(/^\w/, (m) => m.toUpperCase());
}

/** The drill-down links → a row of "open the deeper view" chips. */
export function DrilldownLinks({ links }: { links: ReportEnvelope['drilldown'] }) {
  if (!links || links.length === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
        Drill down
      </span>
      {links.map((l) => {
        // Envelope drilldown hrefs are API paths; map app-facing report links to
        // their on-screen route, otherwise expose the API path read-only.
        const appHref = apiHrefToAppHref(l.href);
        return appHref ? (
          <Link
            key={l.href}
            href={appHref}
            className="inline-flex items-center gap-1 rounded-md border border-border bg-card px-2.5 py-1 text-xs text-foreground hover:bg-accent-muted/40"
          >
            {l.label}
            <ArrowUpRight className="size-3" />
          </Link>
        ) : (
          <span
            key={l.href}
            className="inline-flex items-center gap-1 rounded-md border border-border/60 bg-muted/30 px-2.5 py-1 text-xs text-muted-foreground"
            title={l.href}
          >
            {l.label}
          </span>
        );
      })}
    </div>
  );
}

/** Map a known API drill-down path onto its on-screen report route, else null. */
function apiHrefToAppHref(href: string): string | null {
  if (href.includes('/reports/inventory/reconciliation'))
    return '/reports/inventory/reconciliation';
  if (href.includes('/reports/cash-reconciliation')) return '/reports/cash-reconciliation';
  return null;
}

/**
 * The export options → an ExportButtonGroup that calls the unified exportReport
 * endpoint (which audits the request) and then streams the returned same-origin
 * URL to a browser download. `reportKey`/`filters` identify what to export.
 */
export function EnvelopeExports({
  options,
  reportKey,
  filters,
  filenameBase,
  permitted,
}: {
  options: ReportExportOption[];
  reportKey: string;
  filters: Record<string, string>;
  filenameBase: string;
  permitted?: boolean | null;
}) {
  if (!options || options.length === 0) return null;
  const seen = new Set<string>();
  const actions: ExportAction[] = [];
  for (const opt of options) {
    const fmt = opt.format.toLowerCase();
    if (fmt !== 'csv' && fmt !== 'pdf' && fmt !== 'xlsx') continue;
    if (seen.has(fmt)) continue;
    seen.add(fmt);
    const format = fmt as ExportFormat;
    actions.push({
      format,
      onDownload: () => downloadExport(reportKey, format, filters, `${filenameBase}.${format}`),
    });
  }
  if (actions.length === 0) return null;
  return <ExportButtonGroup actions={actions} permitted={permitted} />;
}

/**
 * Request the unified export (audited server-side), then fetch the returned
 * same-origin URL through the BFF proxy and trigger a browser download. Throws
 * on failure so ExportButtonGroup surfaces the error inline.
 */
export async function downloadExport(
  reportKey: string,
  format: ExportFormat,
  filters: Record<string, string>,
  filename: string,
): Promise<void> {
  const result = await api.exportReport({ report_key: reportKey, format, filters });
  // The export URL is an /api/v1/... path on the Go API; the browser must reach
  // it through the same-origin BFF proxy so the httpOnly session is attached.
  const url = result.url.startsWith('/api/bff') ? result.url : `/api/bff${result.url}`;
  const res = await fetch(url, { credentials: 'same-origin' });
  if (!res.ok) throw new Error('Could not generate the export.');
  const blob = await res.blob();
  triggerDownload(blob, filename);
}

/** Trigger a browser download for an already-fetched blob. */
function triggerDownload(blob: Blob, filename: string) {
  const objectUrl = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = objectUrl;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(objectUrl);
}
