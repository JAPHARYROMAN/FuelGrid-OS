'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { Activity, BarChart3, Building2, Sparkles, TrendingUp } from 'lucide-react';

import type { ReportEnvelope, ReportPeriod } from '@fuelgrid/sdk';
import { SdkError } from '@fuelgrid/sdk';
import {
  BarChart,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  FilterBar,
  FilterField,
  FinancialWaterfall,
  type FinancialWaterfallStep,
  PageHeader,
  RiskBadge,
  type RiskSeverity,
  Skeleton,
  chartColors,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatLitres, formatMoney } from '@/lib/money';

import { PERIODS } from '../_components/filters';
import {
  DataQualityPanel,
  DrilldownLinks,
  EnvelopeExports,
  EnvelopeTable,
  InsightPanel,
  SummaryGrid,
} from '../_components/report-envelope';

/** One station slice of the executive ranking chart (decimal strings). */
interface ExecChartStation {
  station: string;
  revenue: string;
  litres: string;
  net_operating: string;
  risk_alerts: number;
}

/** One step of the network P&L waterfall (FinancialWaterfall contract). */
interface ExecWaterfallStep {
  key: string;
  label: string;
  value: string;
  kind: 'base' | 'delta' | 'total';
}

/** One period-over-period comparison card. */
interface ExecComparisonCard {
  key: string;
  label: string;
  current: string;
  prior: string;
  delta_pct: string;
  unit: string;
}

/** The loss / variance summary block (value gated). */
interface ExecLossSummary {
  loss_litres: string;
  loss_value?: string;
  stock_variance: string;
  value_shown: boolean;
}

/** The deterministic §5.1 automated management narrative. */
interface ExecNarrative {
  sentences: string[];
  focus: string;
}

/** The executive report's report-specific chart_data payload. */
interface ExecChartData {
  narrative: ExecNarrative;
  stations: ExecChartStation[];
  waterfall: ExecWaterfallStep[];
  comparison: ExecComparisonCard[];
  loss_summary: ExecLossSummary;
  margin_shown: boolean;
}

const EMPTY_CHART: ExecChartData = {
  narrative: { sentences: [], focus: '' },
  stations: [],
  waterfall: [],
  comparison: [],
  loss_summary: { loss_litres: '0', stock_variance: '0', value_shown: false },
  margin_shown: false,
};

const selectClasses =
  'h-9 rounded-md border border-border bg-background px-2.5 text-sm text-foreground ' +
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50';

/** Read a labeled summary metric's numeric value (display math only). */
function metricNum(env: ReportEnvelope, label: string): number {
  const m = env.summary.find((s) => s.label === label);
  const n = m ? Number(m.value) : 0;
  return Number.isFinite(n) ? n : 0;
}

/**
 * Derive an overall network-health severity from the report's own counts — a
 * deterministic read (open investigations/alerts → elevated; loss litres or
 * shortages → watch; otherwise stable). Colour is never the only signal: the
 * label text states the read.
 */
function networkHealth(env: ReportEnvelope): { severity: RiskSeverity; label: string } {
  const invest = metricNum(env, 'Open investigations');
  const alerts = metricNum(env, 'Open risk alerts');
  const loss = metricNum(env, 'Total loss litres');
  if (invest > 0 || alerts > 2) return { severity: 'critical', label: 'Needs attention' };
  if (alerts > 0 || loss > 0) return { severity: 'medium', label: 'Watch' };
  return { severity: 'low', label: 'Stable' };
}

/** Format a comparison card's current value by its unit. */
function fmtByUnit(value: string, unit: string): string {
  if (unit === 'TZS') return formatMoney(value);
  if (unit === 'L') return formatLitres(value);
  return value;
}

/**
 * The §5.1 automated management narrative band — the deterministic, board-level
 * summary. Every sentence is filled with a computed figure server-side, so the
 * band is fully traceable and reproducible. The recommended-focus line is
 * highlighted as the single next action.
 */
function NarrativeBand({ narrative }: { narrative: ExecNarrative }) {
  if (narrative.sentences.length === 0) return null;
  return (
    <Card className="border-accent/30 bg-accent-muted/20">
      <CardHeader className="flex-row items-center gap-2 space-y-0">
        <Sparkles className="size-4 text-accent" />
        <CardTitle className="text-base">Management summary</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <p className="text-sm leading-relaxed text-foreground">{narrative.sentences.join(' ')}</p>
        {narrative.focus ? (
          <p className="rounded-md border border-accent/40 bg-card px-3 py-2 text-sm font-medium text-accent">
            {narrative.focus}
          </p>
        ) : null}
      </CardContent>
    </Card>
  );
}

/** Period-over-period comparison cards (revenue / litres / margin when shown). */
function ComparisonCards({ cards }: { cards: ExecComparisonCard[] }) {
  if (cards.length === 0) return null;
  return (
    <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
      {cards.map((c) => {
        const up = c.delta_pct.startsWith('+');
        const flat = !c.delta_pct;
        return (
          <Card key={c.key}>
            <CardContent className="flex flex-col gap-1 pt-6">
              <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                {c.label}
              </span>
              <span className="font-mono text-2xl font-semibold tabular-nums text-foreground">
                {fmtByUnit(c.current, c.unit)}
              </span>
              <div className="flex items-center gap-2 text-xs">
                {flat ? (
                  <span className="text-muted-foreground">no prior period to compare</span>
                ) : (
                  <>
                    <span className={up ? 'font-medium text-success' : 'font-medium text-danger'}>
                      {c.delta_pct}%
                    </span>
                    <span className="text-muted-foreground">
                      vs {fmtByUnit(c.prior, c.unit)} prior
                    </span>
                  </>
                )}
              </div>
            </CardContent>
          </Card>
        );
      })}
    </section>
  );
}

export default function ExecutiveReportPage() {
  const [period, setPeriod] = React.useState<ReportPeriod>('this-month');

  const report = useQuery({
    queryKey: ['report', 'executive', period],
    queryFn: ({ signal }) => api.getExecutiveReport({ period }, signal),
  });

  const filters = { period };

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Executive"
        title="Executive Business Report"
        description="A board-level, cross-domain rollup of the fuel business: revenue, litres, margin, fuel loss, cash, stock, risk and approvals across the stations you can access — with a deterministic management summary, a network profit-and-loss waterfall, a station league table and period-over-period comparison. Margin, loss value and credit exposure require the relevant permission."
      />

      <FilterBar>
        <FilterField label="Period">
          <select
            className={selectClasses}
            value={period}
            onChange={(e) => setPeriod(e.target.value as ReportPeriod)}
            aria-label="Reporting period"
          >
            {PERIODS.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
          </select>
        </FilterField>
      </FilterBar>

      {report.isPending ? (
        <div className="flex flex-col gap-4">
          <Skeleton className="h-28 rounded-xl" />
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-[120px] rounded-xl" />
            ))}
          </section>
          <Skeleton className="h-64 rounded-xl" />
        </div>
      ) : report.isError ? (
        <ErrorState
          title={
            report.error instanceof SdkError && report.error.status === 403
              ? 'No access to the executive report'
              : "Couldn't load the executive report"
          }
          description={
            report.error instanceof SdkError && report.error.status === 403
              ? 'The executive cockpit requires the finance read permission.'
              : String((report.error as Error).message)
          }
          onRetry={
            report.error instanceof SdkError && report.error.status === 403
              ? undefined
              : () => report.refetch()
          }
        />
      ) : (
        (() => {
          const env: ReportEnvelope = report.data;
          const data = (env.chart_data as ExecChartData | null) ?? EMPTY_CHART;
          const health = networkHealth(env);

          const waterfallSteps: FinancialWaterfallStep[] = data.waterfall.map((s) => ({
            key: s.key,
            label: s.label,
            value: s.value,
            kind: s.kind,
          }));

          return (
            <div className="flex flex-col gap-6">
              {/* Data-quality first, then the network-health badge + KPI hero. */}
              <DataQualityPanel items={env.data_quality} />

              <div className="flex items-center gap-3">
                <span className="text-sm font-medium text-muted-foreground">Network health</span>
                <RiskBadge severity={health.severity}>{health.label}</RiskBadge>
              </div>

              <SummaryGrid summary={env.summary} />

              {/* The deterministic §5.1 management narrative band. */}
              <NarrativeBand narrative={data.narrative} />

              {/* Period-over-period comparison cards. */}
              <ComparisonCards cards={data.comparison} />

              {/* Two-column report view: visuals left, context right. */}
              <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
                <div className="flex min-w-0 flex-col gap-6">
                  {/* Network P&L waterfall (only when margin is permitted). */}
                  {waterfallSteps.length > 0 ? (
                    <Card>
                      <CardHeader className="flex-row items-center gap-2 space-y-0">
                        <TrendingUp className="size-4 text-accent" />
                        <CardTitle className="text-base">Network profit &amp; loss</CardTitle>
                      </CardHeader>
                      <CardContent>
                        <FinancialWaterfall
                          steps={waterfallSteps}
                          valueFormatter={(v) => formatMoney(v as string)}
                          ariaLabel="Network profit and loss waterfall"
                          unit="TZS"
                        />
                      </CardContent>
                    </Card>
                  ) : null}

                  {/* Station league table (revenue + net operating by station). */}
                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <BarChart3 className="size-4 text-accent" />
                      <CardTitle className="text-base">Station ranking</CardTitle>
                    </CardHeader>
                    <CardContent>
                      {data.stations.length === 0 ? (
                        <EmptyState
                          title="No stations in scope"
                          description="There is no station activity to rank for this period."
                        />
                      ) : (
                        <BarChart
                          data={data.stations}
                          xKey="station"
                          valueFormatter={(v) => formatMoney(v as string)}
                          series={[
                            { key: 'revenue', label: 'Revenue', color: chartColors.accent },
                            {
                              key: 'net_operating',
                              label: 'Net operating',
                              color: chartColors.success,
                            },
                          ]}
                          height={260}
                        />
                      )}
                    </CardContent>
                  </Card>

                  {/* The drillable per-station rollup table. */}
                  <EnvelopeTable table={env.table} caption="Network league table" />
                </div>

                {/* Right panel: loss summary, insights, drilldown + export. */}
                <aside className="flex flex-col gap-6">
                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <Activity className="size-4 text-warning" />
                      <CardTitle className="text-base">Loss &amp; variance</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <dl className="flex flex-col gap-2.5 text-sm">
                        <div className="flex items-center justify-between gap-3">
                          <dt className="text-muted-foreground">Fuel loss</dt>
                          <dd className="font-mono tabular-nums text-foreground">
                            {formatLitres(data.loss_summary.loss_litres)} L
                          </dd>
                        </div>
                        {data.loss_summary.value_shown && data.loss_summary.loss_value ? (
                          <div className="flex items-center justify-between gap-3">
                            <dt className="text-muted-foreground">Loss value</dt>
                            <dd className="font-mono tabular-nums text-foreground">
                              {formatMoney(data.loss_summary.loss_value)}
                            </dd>
                          </div>
                        ) : (
                          <div className="flex items-center justify-between gap-3">
                            <dt className="text-muted-foreground">Loss value</dt>
                            <dd className="text-xs text-muted-foreground">
                              requires margin permission
                            </dd>
                          </div>
                        )}
                        <div className="flex items-center justify-between gap-3">
                          <dt className="text-muted-foreground">Stock variance</dt>
                          <dd className="font-mono tabular-nums text-foreground">
                            {formatLitres(data.loss_summary.stock_variance)} L
                          </dd>
                        </div>
                      </dl>
                    </CardContent>
                  </Card>

                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <Building2 className="size-4 text-accent" />
                      <CardTitle className="text-base">Leaders</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <dl className="flex flex-col gap-2.5 text-sm">
                        <div className="flex items-center justify-between gap-3">
                          <dt className="text-muted-foreground">Top station</dt>
                          <dd className="font-medium text-foreground">
                            {env.summary.find((s) => s.label === 'Top station')?.value ?? '—'}
                          </dd>
                        </div>
                        <div className="flex items-center justify-between gap-3">
                          <dt className="text-muted-foreground">Underperforming</dt>
                          <dd className="font-medium text-foreground">
                            {env.summary.find((s) => s.label === 'Underperforming station')
                              ?.value ?? '—'}
                          </dd>
                        </div>
                      </dl>
                    </CardContent>
                  </Card>

                  <InsightPanel
                    insights={env.insights}
                    recommendedActions={env.recommended_actions}
                  />

                  <div className="flex flex-col gap-3">
                    <DrilldownLinks links={env.drilldown} />
                    <EnvelopeExports
                      options={env.export_options}
                      reportKey="financials"
                      filters={filters}
                      filenameBase={`executive-${period}`}
                    />
                  </div>
                </aside>
              </div>
            </div>
          );
        })()
      )}
    </div>
  );
}
