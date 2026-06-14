'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Activity,
  ArrowUpRight,
  Flame,
  ListTree,
  ShieldAlert,
  SlidersHorizontal,
} from 'lucide-react';

import type { ReportEnvelope } from '@fuelgrid/sdk';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  DonutChart,
  type DonutSlice,
  EmptyState,
  Heatmap,
  type HeatmapRow,
  LineChart,
  RiskBadge,
  type RiskSeverity,
  ShiftTimeline,
  type ShiftMilestone,
  type MilestoneStatus,
  StatusBoard,
  type StatusBoardItem,
  type StatusTone,
  chartColors,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatLitres } from '@/lib/money';
import { usePermission } from '@/hooks/use-permissions';

import { useStationSelection } from '../_components/filters';
import { PageHeader, ReportFilterBar, ReportStates } from '../_components/report-shell';
import {
  DataQualityPanel,
  DrilldownLinks,
  EnvelopeExports,
  EnvelopeTable,
  InsightPanel,
  SummaryGrid,
} from '../_components/report-envelope';

/** One station × risk-type heatmap row (integer count cells). */
interface RiskHeatRow {
  station: string;
  cells: Record<string, number>;
}

/** One day's loss on the trend line (decimal-string litres/value). */
interface RiskTrendPoint {
  date: string;
  loss_litres: string;
  loss_value?: string;
  events: number;
}

/** One station's risk-ranking row. */
interface RiskRankRow {
  station: string;
  score: number;
  band: string;
  open_alerts: number;
}

/** One root-cause / pattern donut slice (event-count value as a string). */
interface RiskDonutDatum {
  key: string;
  label: string;
  value: string;
}

/** One alert-severity board chip. */
interface RiskAlertChip {
  key: string;
  label: string;
  status: string;
  tone: string;
  count: number;
  detail: string;
}

/** One investigation timeline node. */
interface RiskInvestigationStep {
  title: string;
  status: string;
  when: string;
  detail: string;
}

/** One deterministic §5.11 pattern finding. */
interface RiskPatternFinding {
  dimension: string;
  label: string;
  count: number;
  total: number;
  share_pct: number;
}

/** One read-only risk-rule tuning row. */
interface RiskRuleSummary {
  code: string;
  name: string;
  condition: string;
  severity: string;
  threshold: string;
  enabled: boolean;
  status: string;
}

/** The Risk & Loss report's report-specific chart_data payload. */
interface RiskLossChartData {
  heatmap: RiskHeatRow[];
  heat_types: string[];
  trend: RiskTrendPoint[];
  ranking: RiskRankRow[];
  distribution: RiskDonutDatum[];
  alert_board: RiskAlertChip[];
  investigations: RiskInvestigationStep[];
  patterns: RiskPatternFinding[];
  rules: RiskRuleSummary[];
  value_shown: boolean;
}

const EMPTY_CHART: RiskLossChartData = {
  heatmap: [],
  heat_types: [],
  trend: [],
  ranking: [],
  distribution: [],
  alert_board: [],
  investigations: [],
  patterns: [],
  rules: [],
  value_shown: false,
};

const shortDate = (v: unknown) => {
  const s = String(v ?? '');
  return s.length >= 10 ? s.slice(5) : s;
};

/** Read a labeled summary metric's numeric value (display math only). */
function metricNum(env: ReportEnvelope, label: string): number {
  const m = env.summary.find((s) => s.label === label);
  const n = m ? Number(m.value) : 0;
  return Number.isFinite(n) ? n : 0;
}

/** Derive an overall loss-risk severity from the report's own counts. */
function lossSeverity(env: ReportEnvelope): { severity: RiskSeverity; label: string } {
  const repeated = metricNum(env, 'Repeated-incident tanks');
  const events = metricNum(env, 'Over-tolerance events');
  const alerts = metricNum(env, 'Open risk alerts');
  if (repeated > 0) return { severity: 'critical', label: 'High loss risk' };
  if (alerts > 0 || events > 0) return { severity: 'high', label: 'Elevated' };
  return { severity: 'low', label: 'Stable' };
}

/** Map a server tone string onto the StatusBoard tone vocabulary. */
function boardTone(t: string): StatusTone {
  if (t === 'settled' || t === 'pending' || t === 'at_risk') return t;
  return 'neutral';
}

/** Map a risk band onto the RiskBadge severity vocabulary. */
function bandSeverity(band: string): RiskSeverity {
  switch (band) {
    case 'critical':
      return 'critical';
    case 'high':
      return 'high';
    case 'elevated':
      return 'medium';
    case 'watch':
      return 'low';
    default:
      return 'info';
  }
}

/** Map an investigation status onto the ShiftTimeline milestone vocabulary. */
function milestoneStatus(status: string): MilestoneStatus {
  if (status === 'done' || status === 'current' || status === 'failed') return status;
  return 'pending';
}

/**
 * The station × risk-type heatmap (§5.11). Each row's cells are integer counts;
 * the intensity is each cell's share of the row's strongest count, so the grid
 * reads "which risk type dominates this station" without any colour-only signal
 * (the figure is printed and over-threshold cells are flagged textually).
 */
function RiskHeatmapCard({ data }: { data: RiskLossChartData }) {
  if (data.heatmap.length === 0 || data.heat_types.length === 0) {
    return (
      <EmptyState
        title="No risk signals yet"
        description="No variance events, alerts or investigations in scope to chart."
      />
    );
  }
  const rows: HeatmapRow[] = data.heatmap.map((r, ri) => {
    const max = Math.max(1, ...data.heat_types.map((t) => r.cells[t] ?? 0));
    return {
      key: `row-${ri}`,
      label: r.station,
      cells: data.heat_types.map((t, ci) => {
        const n = r.cells[t] ?? 0;
        return {
          key: `c-${ri}-${ci}`,
          display: String(n),
          intensity: n / max,
          tone:
            t === 'Open alerts' || t === 'Repeated tanks'
              ? ('danger' as const)
              : ('warning' as const),
          flagged: t === 'Repeated tanks' && n > 0,
          ariaLabel: `${r.station} ${t}: ${n}`,
        };
      }),
    };
  });
  return <Heatmap rows={rows} columns={data.heat_types} tone="warning" flagLabel="Recurring" />;
}

/** The deterministic §5.11 pattern narrative as traceable cards. */
function PatternCard({ patterns }: { patterns: RiskPatternFinding[] }) {
  if (patterns.length === 0) {
    return (
      <EmptyState
        title="No concentrated pattern"
        description="Variance events are too few or too evenly spread to call a pattern."
      />
    );
  }
  return (
    <ul className="flex flex-col gap-2.5">
      {patterns.map((p) => (
        <li
          key={`${p.dimension}-${p.label}`}
          className="flex items-center justify-between gap-3 rounded-lg border border-warning/30 bg-warning/5 p-3"
        >
          <div className="flex min-w-0 flex-col">
            <span className="text-sm font-medium text-foreground">
              {p.dimension.charAt(0).toUpperCase() + p.dimension.slice(1)} {p.label}
            </span>
            <span className="text-xs text-muted-foreground">
              appeared in {p.count} of {p.total} related variance events
            </span>
          </div>
          <span className="shrink-0 font-mono text-lg font-semibold tabular-nums text-warning">
            {p.share_pct}%
          </span>
        </li>
      ))}
    </ul>
  );
}

export default function RiskLossPage() {
  const { stations, items, stationId, setStationId } = useStationSelection();
  const [period, setPeriod] = React.useState('current');
  const allowed = usePermission('reconciliation.read', { stationID: stationId });
  // The risk-rules tuning page is gated by risk.read (held tenant-wide); only
  // surface the deep link when the actor can actually open it.
  const canTuneRules = usePermission('risk.read', { mode: 'held' });

  const report = useQuery<ReportEnvelope>({
    queryKey: ['report', 'risk-loss', stationId, period],
    queryFn: ({ signal }) => api.getRiskLossReport(stationId, { period }, signal),
    enabled: !!stationId,
  });

  const filters = { station_id: stationId, period };

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Risk & Loss"
        title="Risk & Loss Intelligence"
        description="Loss litres and value, variance %, open alerts and investigations, repeated incidents and the highest-risk station, with deterministic pattern intelligence (which pump, shift, attendant or product drives the loss), a risk heatmap, loss trend, station ranking, root-cause distribution, alert-severity board and investigation timeline. Litres are exact; loss value requires the margin.view permission."
      />

      <ReportFilterBar
        items={items}
        stationId={stationId}
        onStation={setStationId}
        period={period}
        onPeriod={setPeriod}
      />

      <ReportStates
        stationsPending={stations.isPending}
        noStations={!stations.isPending && items.length === 0}
        query={report}
        loadingLabel="risk & loss report"
      >
        {(env: ReportEnvelope) => {
          const data = (env.chart_data as RiskLossChartData | null) ?? EMPTY_CHART;
          const risk = lossSeverity(env);

          // The loss trend plots litres (always shown). The value series is gated
          // server-side via value_shown; litres alone keep the trend honest for
          // non-margin holders.
          const trendSeries = [
            { key: 'loss_litres', label: 'Loss (L)', color: chartColors.danger },
          ];

          const donutSlices: DonutSlice[] = data.distribution.map((d) => ({
            key: d.key,
            label: d.label,
            value: d.value,
          }));

          const alertItems: StatusBoardItem[] = data.alert_board.map((c) => ({
            key: c.key,
            label: c.label,
            status: c.status,
            tone: boardTone(c.tone),
            detail: c.detail,
          }));

          const investigationMilestones: ShiftMilestone[] = data.investigations.map((s) => ({
            label: s.title,
            timestamp: s.when,
            status: milestoneStatus(s.status),
            detail: s.detail,
          }));

          return (
            <div className="flex flex-col gap-6">
              {/* Hero: data-quality first, then the risk badge + KPI MetricCards. */}
              <DataQualityPanel items={env.data_quality} />

              <div className="flex items-center gap-3">
                <span className="text-sm font-medium text-muted-foreground">Loss risk</span>
                <RiskBadge severity={risk.severity}>{risk.label}</RiskBadge>
              </div>

              <SummaryGrid summary={env.summary} />

              {/* Two-column report view (§18.2): main visuals + right context. */}
              <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
                <div className="flex min-w-0 flex-col gap-6">
                  {/* The §5.11 pattern intelligence — the deterministic centrepiece. */}
                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <Flame className="size-4 text-warning" />
                      <CardTitle className="text-base">Pattern intelligence</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <p className="mb-4 text-sm text-muted-foreground">
                        Deterministic concentration of over-tolerance variance events by pump,
                        shift, attendant and product — every figure is a share of related events,
                        traceable to the reconciliation rows below.
                      </p>
                      <PatternCard patterns={data.patterns} />
                    </CardContent>
                  </Card>

                  {/* Risk heatmap (station × risk-type). */}
                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <ShieldAlert className="size-4 text-danger" />
                      <CardTitle className="text-base">Risk heatmap</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <RiskHeatmapCard data={data} />
                    </CardContent>
                  </Card>

                  {/* Loss trend. */}
                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <Activity className="size-4 text-accent" />
                      <CardTitle className="text-base">Loss trend</CardTitle>
                    </CardHeader>
                    <CardContent>
                      {data.trend.length < 2 ? (
                        <EmptyState
                          title="Not enough history"
                          description="At least two days of loss are needed to plot a trend."
                        />
                      ) : (
                        <LineChart
                          data={data.trend}
                          xKey="date"
                          xFormatter={shortDate}
                          valueFormatter={(v) => formatLitres(v as string)}
                          series={trendSeries}
                          height={240}
                        />
                      )}
                    </CardContent>
                  </Card>

                  {/* Root-cause distribution donut. */}
                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <ListTree className="size-4 text-accent" />
                      <CardTitle className="text-base">Root-cause distribution</CardTitle>
                    </CardHeader>
                    <CardContent>
                      {donutSlices.length === 0 ? (
                        <EmptyState
                          title="No root-cause split"
                          description="No over-tolerance variance events to attribute yet."
                        />
                      ) : (
                        <DonutChart
                          slices={donutSlices}
                          valueFormatter={(v) => `${v} events`}
                          centerLabel="Leading factors"
                        />
                      )}
                    </CardContent>
                  </Card>

                  {/* The drillable variance-event history (loss → station → product →
                      tank → reconciliation → shift → attendant). */}
                  <EnvelopeTable table={env.table} caption="Variance event history" />
                </div>

                {/* Right panel (§18.2): alert board, investigations, ranking, insights. */}
                <aside className="flex flex-col gap-6">
                  {alertItems.length > 0 ? (
                    <Card>
                      <CardHeader className="flex-row items-center gap-2 space-y-0">
                        <ShieldAlert className="size-4 text-danger" />
                        <CardTitle className="text-base">Alert severity</CardTitle>
                      </CardHeader>
                      <CardContent>
                        <p className="mb-3 text-sm text-muted-foreground">
                          Open risk alerts for this station by severity.
                        </p>
                        <StatusBoard items={alertItems} columnsClassName="grid-cols-1" />
                      </CardContent>
                    </Card>
                  ) : null}

                  {investigationMilestones.length > 0 ? (
                    <Card>
                      <CardHeader className="flex-row items-center gap-2 space-y-0">
                        <Activity className="size-4 text-accent" />
                        <CardTitle className="text-base">Investigations</CardTitle>
                      </CardHeader>
                      <CardContent>
                        <p className="mb-3 text-sm text-muted-foreground">
                          Investigation lifecycle: open → investigating → resolved / dismissed.
                        </p>
                        <ShiftTimeline milestones={investigationMilestones} />
                      </CardContent>
                    </Card>
                  ) : null}

                  {data.ranking.length > 0 ? (
                    <Card>
                      <CardHeader className="flex-row items-center gap-2 space-y-0">
                        <ShieldAlert className="size-4 text-warning" />
                        <CardTitle className="text-base">Station risk ranking</CardTitle>
                      </CardHeader>
                      <CardContent>
                        <ul className="flex flex-col gap-2">
                          {data.ranking.map((r) => (
                            <li
                              key={r.station}
                              className="flex items-center justify-between gap-3 rounded-md border border-border bg-card px-2.5 py-2"
                            >
                              <span className="min-w-0 flex-1 truncate text-sm text-foreground">
                                {r.station}
                              </span>
                              <RiskBadge severity={bandSeverity(r.band)}>{r.band}</RiskBadge>
                              <span className="shrink-0 font-mono text-sm font-semibold tabular-nums text-foreground">
                                {r.score}
                              </span>
                            </li>
                          ))}
                        </ul>
                      </CardContent>
                    </Card>
                  ) : null}

                  {data.rules.length > 0 ? (
                    <Card>
                      <CardHeader className="flex-row items-center gap-2 space-y-0">
                        <SlidersHorizontal className="size-4 text-accent" />
                        <CardTitle className="text-base">Rules driving alerts</CardTitle>
                      </CardHeader>
                      <CardContent>
                        <p className="mb-3 text-sm text-muted-foreground">
                          The configured risk rules behind these alerts (read-only).
                          {canTuneRules ? ' Tune them on the rules page.' : ''}
                        </p>
                        <ul className="flex flex-col gap-2">
                          {data.rules.map((rule) => (
                            <li
                              key={rule.code}
                              className="flex items-center justify-between gap-3 rounded-md border border-border bg-card px-2.5 py-2"
                            >
                              <div className="flex min-w-0 flex-col">
                                <span className="truncate text-sm text-foreground">
                                  {rule.name}
                                </span>
                                <span className="font-mono text-[11px] text-muted-foreground">
                                  {rule.condition || rule.code}
                                </span>
                              </div>
                              <RiskBadge severity={bandSeverity(rule.severity)}>
                                {rule.enabled ? rule.severity : 'off'}
                              </RiskBadge>
                            </li>
                          ))}
                        </ul>
                        {canTuneRules ? (
                          <a
                            href="/risk"
                            className="mt-3 inline-flex items-center gap-1 text-xs font-medium text-accent hover:underline"
                          >
                            Open risk centre
                            <ArrowUpRight className="size-3" />
                          </a>
                        ) : null}
                      </CardContent>
                    </Card>
                  ) : null}

                  <InsightPanel
                    insights={env.insights}
                    recommendedActions={env.recommended_actions}
                  />

                  <div className="flex flex-col gap-3">
                    <DrilldownLinks links={env.drilldown} />
                    <EnvelopeExports
                      options={env.export_options}
                      reportKey="reconciliation"
                      filters={filters}
                      filenameBase={`risk-loss-${stationId.slice(0, 8)}`}
                      permitted={allowed}
                    />
                  </div>
                </aside>
              </div>
            </div>
          );
        }}
      </ReportStates>
    </div>
  );
}
