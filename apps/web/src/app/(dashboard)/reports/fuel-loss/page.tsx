'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';

import type { ReportEnvelope } from '@fuelgrid/sdk';
import {
  AreaChart,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  RiskBadge,
  type RiskSeverity,
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

/** A loss point in the fuel-loss chart_data payload (decimal strings). */
interface LossChartPoint {
  tank: string;
  business_date: string;
  variance_litres: string;
  variance_pct: string;
}

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

/**
 * Derive an overall loss-risk severity from the report's own counts: repeated
 * incidents are the strongest signal, then breaches, then any loss litres.
 */
function lossSeverity(env: ReportEnvelope): { severity: RiskSeverity; label: string } {
  const repeated = metricNum(env, 'Repeated-incident tanks');
  const breaches = metricNum(env, 'Tolerance breaches');
  const loss = metricNum(env, 'Total loss litres');
  if (repeated > 0) return { severity: 'critical', label: 'High loss risk' };
  if (breaches > 0) return { severity: 'high', label: 'Elevated' };
  if (loss > 0) return { severity: 'medium', label: 'Watch' };
  return { severity: 'low', label: 'Stable' };
}

export default function FuelLossPage() {
  const { stations, items, stationId, setStationId } = useStationSelection();
  const [period, setPeriod] = React.useState('current');
  const allowed = usePermission('reconciliation.read', { stationID: stationId });

  const report = useQuery({
    queryKey: ['report', 'fuel-loss', stationId, period],
    queryFn: ({ signal }) => api.getFuelLossReport(stationId, { period }, signal),
    enabled: !!stationId,
  });

  const filters = { station_id: stationId, period };

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Inventory"
        title="Fuel Loss"
        description="Loss litres and value, variance %, repeated incidents and loss patterns across recent reconciliations. Litres are exact."
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
        loadingLabel="fuel loss report"
      >
        {(env: ReportEnvelope) => {
          const points = (env.chart_data as LossChartPoint[] | null) ?? [];
          const risk = lossSeverity(env);
          return (
            <div className="flex flex-col gap-6">
              <DataQualityPanel items={env.data_quality} />

              <div className="flex items-center gap-3">
                <span className="text-sm font-medium text-muted-foreground">Loss risk</span>
                <RiskBadge severity={risk.severity}>{risk.label}</RiskBadge>
              </div>

              <SummaryGrid summary={env.summary} />

              <Card>
                <CardHeader>
                  <CardTitle>Variance over time</CardTitle>
                  <p className="text-sm text-muted-foreground">
                    Per-reconciliation variance in litres — dips below zero are losses.
                  </p>
                </CardHeader>
                <CardContent>
                  {points.length < 2 ? (
                    <EmptyState
                      title="Not enough history"
                      description="At least two reconciliations are needed to plot a loss trend."
                    />
                  ) : (
                    <AreaChart
                      data={points}
                      xKey="business_date"
                      xFormatter={shortDate}
                      valueFormatter={(v) => formatLitres(v as string)}
                      series={[
                        {
                          key: 'variance_litres',
                          label: 'Variance (L)',
                          color: chartColors.danger,
                        },
                      ]}
                      height={260}
                    />
                  )}
                </CardContent>
              </Card>

              <InsightPanel insights={env.insights} recommendedActions={env.recommended_actions} />

              <EnvelopeTable table={env.table} caption="Reconciliation variance history" />

              <div className="flex flex-col gap-3">
                <DrilldownLinks links={env.drilldown} />
                <EnvelopeExports
                  options={env.export_options}
                  reportKey="reconciliation"
                  filters={filters}
                  filenameBase={`fuel-loss-${stationId.slice(0, 8)}`}
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
