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
import { formatMoney } from '@/lib/money';
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

/** The corrections-variances chart_data payload (decimal strings throughout). */
interface CorrectionItem {
  shift: string;
  attendant: string;
  submitted_reading: string;
  final_reading: string;
  delta_litres: string;
  status: string;
}
interface CollectionItem {
  shift: string;
  submitted_by: string;
  expected_amount: string;
  submitted_total: string;
  received_total: string;
  difference: string;
  status: string;
}
interface CorrectionsChart {
  corrections: CorrectionItem[];
  collections: CollectionItem[];
}

const shortShift = (v: unknown) => {
  const s = String(v ?? '');
  return s.length > 14 ? `${s.slice(0, 13)}…` : s;
};

export default function CorrectionsVariancesReportPage() {
  const { stations, items, stationId, setStationId } = useStationSelection();
  // Empty by default — the SDK applies its 30-day window when from/to are unset.
  const [from, setFrom] = React.useState('');
  const [to, setTo] = React.useState('');
  const allowed = usePermission('station.read', { stationID: stationId });

  const report = useQuery({
    queryKey: ['report', 'corrections-variances', stationId, from, to],
    queryFn: ({ signal }) =>
      api.getCorrectionsVariancesReport(
        stationId,
        { from: from || undefined, to: to || undefined },
        signal,
      ),
    enabled: !!stationId,
  });

  const filters: Record<string, string> = { station_id: stationId };
  if (from) filters.from = from;
  if (to) filters.to = to;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Corrections & variances"
        title="Corrections & Variances"
        description="Supervisor-corrected or rejected closing readings with both the attendant-submitted and final approved figures plus the reason, and collection receipts with expected vs received cash and the resulting shortage or excess. Meter and money figures are exact decimals."
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
        loadingLabel="corrections & variances report"
      >
        {(env: ReportEnvelope) => {
          const chart = (env.chart_data as CorrectionsChart | null) ?? {
            corrections: [],
            collections: [],
          };
          const collections = chart.collections ?? [];
          return (
            <div className="flex flex-col gap-6">
              <DataQualityPanel items={env.data_quality} />
              <SummaryGrid summary={env.summary} />

              <Card>
                <CardHeader>
                  <CardTitle>Expected vs received collections</CardTitle>
                  <p className="text-sm text-muted-foreground">
                    Per receipt: the expected cash against what the supervisor confirmed receiving.
                  </p>
                </CardHeader>
                <CardContent>
                  {collections.length === 0 ? (
                    <EmptyState
                      title="No collection receipts"
                      description="No collection receipts recorded for this station in the selected window."
                    />
                  ) : (
                    <BarChart
                      data={collections}
                      xKey="shift"
                      xFormatter={shortShift}
                      valueFormatter={(v) => formatMoney(v as string)}
                      series={[
                        {
                          key: 'expected_amount',
                          label: 'Expected',
                          color: chartColors.accent,
                        },
                        {
                          key: 'received_total',
                          label: 'Received',
                          color: chartColors.success,
                        },
                      ]}
                      height={260}
                    />
                  )}
                </CardContent>
              </Card>

              <InsightPanel insights={env.insights} recommendedActions={env.recommended_actions} />

              <EnvelopeTable table={env.table} caption="Corrections & collection variances" />

              <div className="flex flex-col gap-3">
                <DrilldownLinks links={env.drilldown} />
                <EnvelopeExports
                  options={env.export_options}
                  reportKey="corrections-variances"
                  filters={filters}
                  filenameBase={`corrections-variances-${stationId.slice(0, 8)}`}
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
