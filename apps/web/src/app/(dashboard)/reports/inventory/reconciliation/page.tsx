'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { Gauge } from 'lucide-react';

import type { ReportEnvelope } from '@fuelgrid/sdk';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  Heatmap,
  ReconciliationWaterfall,
  type HeatmapRow,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatLitres, formatMoney } from '@/lib/money';
import { usePermission } from '@/hooks/use-permissions';

import { useStationSelection } from '../../_components/filters';
import { PageHeader, ReportFilterBar, ReportStates } from '../../_components/report-shell';
import {
  DataQualityPanel,
  DrilldownLinks,
  EnvelopeExports,
  EnvelopeTable,
  InsightPanel,
  SummaryGrid,
} from '../../_components/report-envelope';

/** One tank's reconciliation row in the chart_data payload (decimal strings). */
interface ReconChartTank {
  tank: string;
  product: string;
  product_color: string;
  opening: string;
  deliveries: string;
  sales: string;
  adjustments: string;
  expected_closing: string;
  actual_closing: string;
  variance: string;
  variance_pct: string;
  variance_value: string;
  priced: boolean;
  tolerance: string;
  over_tolerance: boolean;
  sealed: boolean;
}

/** Coerce a decimal string to a finite number for cell/bar geometry only. */
function num(v: string | undefined): number {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

/**
 * Build the variance heatmap rows from the tank lines. One row per tank; the
 * cell intensity is each tank's |variance %| as a share of the worst |variance
 * %| in the grid (so the heaviest breach reads strongest). Over-tolerance cells
 * are flagged (text chip + ring), within-tolerance cells wash toward success —
 * colour is never the sole signal because every cell shows its figure. Values
 * stay decimal strings; the float coercion is for the intensity ratio only.
 */
function heatmapRows(tanks: ReconChartTank[]): HeatmapRow[] {
  const worst = tanks.reduce((m, t) => Math.max(m, Math.abs(num(t.variance_pct))), 0);
  return tanks.map((t) => {
    const absPct = Math.abs(num(t.variance_pct));
    const intensity = worst > 0 ? absPct / worst : 0;
    const pct = num(t.variance_pct);
    const signed = `${pct > 0 ? '+' : ''}${t.variance_pct || '0'}%`;
    return {
      key: t.tank,
      label: t.tank,
      sublabel: t.product || undefined,
      cells: [
        {
          key: 'variance_pct',
          display: signed,
          intensity,
          tone: t.over_tolerance ? ('danger' as const) : ('success' as const),
          flagged: t.over_tolerance,
          sublabel: t.tolerance ? `tol ${t.tolerance}%` : undefined,
          ariaLabel: `${t.tank} variance ${signed}${
            t.over_tolerance ? ' over tolerance' : ' within tolerance'
          }`,
        },
        {
          key: 'variance_litres',
          display: formatLitres(t.variance),
          intensity,
          tone: t.over_tolerance ? ('danger' as const) : ('success' as const),
          flagged: t.over_tolerance,
          sublabel: t.priced ? formatMoney(t.variance_value) : 'no price',
          ariaLabel: `${t.tank} variance ${formatLitres(t.variance)} litres`,
        },
      ],
    };
  });
}

export default function InventoryReconciliationPage() {
  const { stations, items, stationId, setStationId } = useStationSelection();
  const [period, setPeriod] = React.useState('current');
  const allowed = usePermission('reconciliation.read', { stationID: stationId });

  const report = useQuery({
    queryKey: ['report', 'inventory-reconciliation', stationId, period],
    queryFn: ({ signal }) => api.getReconciliationReport(stationId, { period }, signal),
    enabled: !!stationId,
  });

  const filters = { station_id: stationId, period };

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Inventory"
        title="Inventory Reconciliation"
        description="The signature visual: per-tank Opening + Deliveries − Sales ± Adjustments = Expected vs Actual closing, with over-tolerance variance, a variance heatmap, insights and data-quality checks."
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
        loadingLabel="reconciliation report"
      >
        {(env: ReportEnvelope) => {
          const tanks = (env.chart_data as ReconChartTank[] | null) ?? [];
          const overCount = tanks.filter((t) => t.over_tolerance).length;
          const rows = heatmapRows(tanks);

          return (
            <div className="flex flex-col gap-6">
              {/* Hero: data-quality first (most prominent), then the KPI grid. */}
              <DataQualityPanel items={env.data_quality} />
              <SummaryGrid summary={env.summary} />

              {/* Two-column report view (§18.2): main visuals + right context panel. */}
              <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
                <div className="flex min-w-0 flex-col gap-6">
                  {/* The centerpiece: per-tank reconciliation waterfall (§5.3). */}
                  <Card>
                    <CardHeader>
                      <CardTitle>Per-tank reconciliation waterfall</CardTitle>
                      <p className="text-sm text-muted-foreground">
                        Opening +Deliveries −Sales ±Adjustments = Expected, measured against the
                        physical closing dip. Litres are exact.
                      </p>
                    </CardHeader>
                    <CardContent className="flex flex-col gap-5">
                      {tanks.length === 0 ? (
                        <EmptyState
                          title="No tanks reconciled"
                          description="No tanks or no active operating day for this station yet."
                        />
                      ) : (
                        tanks.map((t) => (
                          <div key={t.tank} className="flex flex-col gap-2">
                            <div className="flex items-center gap-2 text-sm font-medium text-foreground">
                              Tank {t.tank}
                              {t.product ? (
                                <span className="text-xs font-normal text-muted-foreground">
                                  {t.product}
                                </span>
                              ) : null}
                              {t.tolerance ? (
                                <span className="text-xs font-normal text-muted-foreground">
                                  tolerance {t.tolerance}%
                                </span>
                              ) : null}
                            </div>
                            <ReconciliationWaterfall
                              openingStock={t.opening}
                              deliveries={t.deliveries}
                              sales={t.sales}
                              adjustments={t.adjustments}
                              expectedClosing={t.expected_closing}
                              actualClosing={t.actual_closing}
                              variance={t.variance}
                              tolerance={toleranceLitres(t)}
                              unit="L"
                            />
                          </div>
                        ))
                      )}
                    </CardContent>
                  </Card>

                  {/* Variance heatmap across tanks/products (§5.3 Visual Requirements). */}
                  <Card>
                    <CardHeader>
                      <CardTitle>Variance heatmap</CardTitle>
                      <p className="text-sm text-muted-foreground">
                        Signed variance % and litre value per tank. Over-tolerance tanks are
                        flagged; the wash intensity tracks the size of each breach.
                      </p>
                    </CardHeader>
                    <CardContent>
                      {rows.length === 0 ? (
                        <EmptyState
                          title="No variance to map"
                          description="Reconcile at least one tank to see the variance heatmap."
                        />
                      ) : (
                        <Heatmap
                          rows={rows}
                          columns={['Variance %', 'Variance (L / value)']}
                          flagLabel="Over"
                        />
                      )}
                    </CardContent>
                  </Card>

                  <EnvelopeTable table={env.table} caption="All tanks" />
                </div>

                {/* Right panel (§18.2): over-tolerance summary, data-quality, insights,
                    recommended actions, drill-down + export. */}
                <aside className="flex flex-col gap-6">
                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <Gauge className="size-4 text-accent" />
                      <CardTitle className="text-base">Tolerance status</CardTitle>
                    </CardHeader>
                    <CardContent>
                      {tanks.length === 0 ? (
                        <p className="text-sm text-muted-foreground">
                          No tanks reconciled for this day yet.
                        </p>
                      ) : overCount === 0 ? (
                        <p className="text-sm text-success">
                          All {tanks.length} reconciled tank(s) are within tolerance.
                        </p>
                      ) : (
                        <p className="text-sm text-danger">
                          {overCount} of {tanks.length} tank(s) breached tolerance — review the
                          flagged rows and open a loss investigation if it repeats.
                        </p>
                      )}
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
                      reportKey="reconciliation"
                      filters={filters}
                      filenameBase={`reconciliation-${stationId.slice(0, 8)}`}
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

/**
 * Derive an absolute litre tolerance band for the waterfall from the per-tank
 * tolerance percent against the expected closing volume. Pure display math on
 * already-computed decimal strings; never fed back into the persisted figures.
 */
function toleranceLitres(t: ReconChartTank): string {
  const pct = Number(t.tolerance);
  const expected = Number(t.expected_closing);
  if (!Number.isFinite(pct) || !Number.isFinite(expected)) return '0';
  return String((Math.abs(expected) * pct) / 100);
}
