'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';

import type { ReportEnvelope } from '@fuelgrid/sdk';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ReconciliationWaterfall,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
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
  opening: string;
  deliveries: string;
  sales: string;
  adjustments: string;
  expected_closing: string;
  actual_closing: string;
  variance: string;
  variance_pct: string;
  tolerance: string;
  sealed: boolean;
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
        description="The signature visual: per-tank Opening + Deliveries − Sales ± Adjustments = Expected vs Actual closing, with over-tolerance variance, insights and data-quality checks."
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
          return (
            <div className="flex flex-col gap-6">
              <DataQualityPanel items={env.data_quality} />
              <SummaryGrid summary={env.summary} />

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

              <InsightPanel insights={env.insights} recommendedActions={env.recommended_actions} />

              <EnvelopeTable table={env.table} caption="All tanks" />

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
