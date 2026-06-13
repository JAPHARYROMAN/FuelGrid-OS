'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';

import type { ReportEnvelope } from '@fuelgrid/sdk';
import {
  BarChart,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  chartColors,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { usePermission } from '@/hooks/use-permissions';

import { useStationSelection } from '../_components/filters';
import { PageHeader, ReportDateRangeFilterBar, ReportStates } from '../_components/report-shell';
import {
  DataQualityPanel,
  DrilldownLinks,
  EnvelopeExports,
  EnvelopeTable,
  InsightPanel,
  SummaryGrid,
} from '../_components/report-envelope';

/** A day in the attendance chart_data payload (counts by derived status). */
interface AttendanceChartDay {
  date: string;
  present: number;
  late: number;
  no_show: number;
  not_checked_in: number;
}

const shortDate = (v: unknown) => {
  const s = String(v ?? '');
  return s.length >= 10 ? s.slice(5) : s;
};

export default function AttendanceReportPage() {
  const { stations, items, stationId, setStationId } = useStationSelection();
  // Empty by default — the SDK applies its 30-day window when from/to are unset.
  const [from, setFrom] = React.useState('');
  const [to, setTo] = React.useState('');
  const allowed = usePermission('station.read', { stationID: stationId });

  const report = useQuery({
    queryKey: ['report', 'attendance', stationId, from, to],
    queryFn: ({ signal }) =>
      api.getAttendanceReport(stationId, { from: from || undefined, to: to || undefined }, signal),
    enabled: !!stationId,
  });

  const filters: Record<string, string> = { station_id: stationId };
  if (from) filters.from = from;
  if (to) filters.to = to;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Attendance"
        title="Attendance Report"
        description="Rostered attendants vs check-in / check-out for each shift, with late (more than 15 minutes after the shift opened) and no-show derivation over the selected window."
      />

      <ReportDateRangeFilterBar
        items={items}
        stationId={stationId}
        onStation={setStationId}
        from={from}
        to={to}
        onFrom={setFrom}
        onTo={setTo}
      />

      <ReportStates
        stationsPending={stations.isPending}
        noStations={!stations.isPending && items.length === 0}
        query={report}
        loadingLabel="attendance report"
      >
        {(env: ReportEnvelope) => {
          const days = (env.chart_data as AttendanceChartDay[] | null) ?? [];
          return (
            <div className="flex flex-col gap-6">
              <DataQualityPanel items={env.data_quality} />
              <SummaryGrid summary={env.summary} />

              <Card>
                <CardHeader>
                  <CardTitle>Attendance by day</CardTitle>
                  <p className="text-sm text-muted-foreground">
                    Present, late and no-show counts per operating day in the window.
                  </p>
                </CardHeader>
                <CardContent>
                  {days.length === 0 ? (
                    <EmptyState
                      title="No rostered shifts"
                      description="No rostered shifts in the selected window for this station."
                    />
                  ) : (
                    <BarChart
                      data={days}
                      xKey="date"
                      xFormatter={shortDate}
                      valueFormatter={(v) => String(v)}
                      series={[
                        { key: 'present', label: 'Present', color: chartColors.success },
                        { key: 'late', label: 'Late', color: chartColors.warning },
                        { key: 'no_show', label: 'No-show', color: chartColors.danger },
                      ]}
                      height={260}
                    />
                  )}
                </CardContent>
              </Card>

              <InsightPanel insights={env.insights} recommendedActions={env.recommended_actions} />

              <EnvelopeTable table={env.table} caption="Roster vs attendance" />

              <div className="flex flex-col gap-3">
                <DrilldownLinks links={env.drilldown} />
                <EnvelopeExports
                  options={env.export_options}
                  reportKey="attendance"
                  filters={filters}
                  filenameBase={`attendance-${stationId.slice(0, 8)}`}
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
