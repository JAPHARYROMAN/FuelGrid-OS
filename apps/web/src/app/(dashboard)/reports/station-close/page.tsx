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
  chartColors,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';
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

/** A day in the station-close chart_data payload (decimal strings). */
interface CloseChartDay {
  date: string;
  gross: string;
  margin: string;
  tendered: string;
  cash_variance: string;
}

const shortDate = (v: unknown) => {
  const s = String(v ?? '');
  return s.length >= 10 ? s.slice(5) : s;
};

export default function StationClosePage() {
  const { stations, items, stationId, setStationId } = useStationSelection();
  const allowed = usePermission('revenue.read', { stationID: stationId });

  const report = useQuery({
    queryKey: ['report', 'station-close', stationId],
    queryFn: ({ signal }) => api.getStationCloseReport(stationId, {}, signal),
    enabled: !!stationId,
  });

  const filters = { station_id: stationId };

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Daily close"
        title="Daily Station Close"
        description="Recognized sales and litres, stock variance, cash position, deliveries, open exceptions and approval status for the latest operating day, with a recent-day trend."
      />

      <ReportFilterBar
        items={items}
        stationId={stationId}
        onStation={setStationId}
        showPeriod={false}
      />

      <ReportStates
        stationsPending={stations.isPending}
        noStations={!stations.isPending && items.length === 0}
        query={report}
        loadingLabel="station close report"
      >
        {(env: ReportEnvelope) => {
          const days = (env.chart_data as CloseChartDay[] | null) ?? [];
          return (
            <div className="flex flex-col gap-6">
              <DataQualityPanel items={env.data_quality} />
              <SummaryGrid summary={env.summary} />

              <Card>
                <CardHeader>
                  <CardTitle>Recent-day revenue trend</CardTitle>
                  <p className="text-sm text-muted-foreground">
                    Gross revenue and tendered totals over recent operating days.
                  </p>
                </CardHeader>
                <CardContent>
                  {days.length < 2 ? (
                    <EmptyState
                      title="Not enough history"
                      description="At least two operating days are needed to plot a trend."
                    />
                  ) : (
                    <AreaChart
                      data={days}
                      xKey="date"
                      xFormatter={shortDate}
                      valueFormatter={(v) => formatMoney(v as string)}
                      series={[
                        { key: 'gross', label: 'Gross', color: chartColors.accent },
                        { key: 'tendered', label: 'Tendered', color: chartColors.success },
                      ]}
                      height={260}
                    />
                  )}
                </CardContent>
              </Card>

              <InsightPanel insights={env.insights} recommendedActions={env.recommended_actions} />

              <EnvelopeTable table={env.table} caption="Operating days" />

              <div className="flex flex-col gap-3">
                <DrilldownLinks links={env.drilldown} />
                <EnvelopeExports
                  options={env.export_options}
                  reportKey="station-close"
                  filters={filters}
                  filenameBase={`station-close-${stationId.slice(0, 8)}`}
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
