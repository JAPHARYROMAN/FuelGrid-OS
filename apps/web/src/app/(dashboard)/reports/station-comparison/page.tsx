'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';

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
  PageHeader,
  Skeleton,
  chartColors,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';

import { PERIODS } from '../_components/filters';
import {
  DataQualityPanel,
  DrilldownLinks,
  EnvelopeTable,
  InsightPanel,
  SummaryGrid,
} from '../_components/report-envelope';

/** One station slice of the comparison chart_data payload (decimal strings). */
interface ComparisonSlice {
  station: string;
  revenue: string;
  litres: string;
  gross_margin: string;
  expenses: string;
  net_operating: string;
  risk_alerts: number;
}

const selectClasses =
  'h-9 rounded-md border border-border bg-background px-2.5 text-sm text-foreground ' +
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50';

export default function StationComparisonPage() {
  const [period, setPeriod] = React.useState<ReportPeriod>('this-month');

  const report = useQuery({
    queryKey: ['report', 'station-comparison', period],
    queryFn: ({ signal }) => api.getStationComparisonReport({ period }, signal),
  });

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Station comparison"
        title="Station Comparison"
        description="Per-station ranking by revenue, litres sold, gross margin, operating expenses, net operating result, stock variance, open risk alerts and outstanding collections — limited to the stations you can access."
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
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {Array.from({ length: 2 }).map((_, i) => (
              <Skeleton key={i} className="h-[120px] rounded-xl" />
            ))}
          </section>
          <Skeleton className="h-64 rounded-xl" />
        </div>
      ) : report.isError ? (
        <ErrorState
          title={
            report.error instanceof SdkError && report.error.status === 403
              ? 'No access to station reports'
              : "Couldn't load the station comparison"
          }
          description={String((report.error as Error).message)}
          onRetry={() => report.refetch()}
        />
      ) : (
        (() => {
          const env: ReportEnvelope = report.data;
          const rows = (env.chart_data as ComparisonSlice[] | null) ?? [];
          return (
            <div className="flex flex-col gap-6">
              <DataQualityPanel items={env.data_quality} />
              <SummaryGrid summary={env.summary} />

              <Card>
                <CardHeader>
                  <CardTitle>Net operating result by station</CardTitle>
                  <p className="text-sm text-muted-foreground">
                    Revenue and the net operating result it yields, ranked across the stations you
                    can access.
                  </p>
                </CardHeader>
                <CardContent>
                  {rows.length === 0 ? (
                    <EmptyState
                      title="No stations in scope"
                      description="There are no stations to compare for the selected period."
                    />
                  ) : (
                    <BarChart
                      data={rows}
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

              <InsightPanel insights={env.insights} recommendedActions={env.recommended_actions} />

              <EnvelopeTable table={env.table} caption="Station ranking" />

              <DrilldownLinks links={env.drilldown} />
            </div>
          );
        })()
      )}
    </div>
  );
}
