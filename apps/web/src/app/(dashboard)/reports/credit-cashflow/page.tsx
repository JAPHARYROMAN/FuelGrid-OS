'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';

import type { ReportEnvelope, ReportPeriod } from '@fuelgrid/sdk';
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
import { PageHeader, ReportFilterBar, ReportStates } from '../_components/report-shell';
import {
  DataQualityPanel,
  DrilldownLinks,
  EnvelopeExports,
  EnvelopeTable,
  InsightPanel,
  SummaryGrid,
} from '../_components/report-envelope';

/** One tender slice of the credit-cashflow chart_data payload (decimal strings). */
interface TenderSlice {
  tender: string;
  amount: string;
}

export default function CreditCashflowPage() {
  const { stations, items, stationId, setStationId } = useStationSelection();
  const [period, setPeriod] = React.useState<ReportPeriod>('this-month');
  const allowed = usePermission('revenue.read', { stationID: stationId });

  const report = useQuery({
    queryKey: ['report', 'credit-cashflow', stationId, period],
    queryFn: ({ signal }) => api.getCreditCashflowReport(stationId, { period }, signal),
    enabled: !!stationId,
  });

  const filters = { station_id: stationId, period };

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Credit & cashflow"
        title="Credit & cashflow"
        description="Sales by tender (cash, mobile money, card, credit), collections, outstanding and overdue receivables, supplier payments, cash variance and the projected cash position for a station over the selected period. Money figures are exact decimals throughout; supplier payments are a network-wide figure."
      />

      <ReportFilterBar
        items={items}
        stationId={stationId}
        onStation={setStationId}
        period={period}
        onPeriod={(p) => setPeriod(p as ReportPeriod)}
      />

      <ReportStates
        stationsPending={stations.isPending}
        noStations={!stations.isPending && items.length === 0}
        query={report}
        loadingLabel="credit & cashflow report"
      >
        {(env: ReportEnvelope) => {
          const tenders = (env.chart_data as TenderSlice[] | null) ?? [];
          const hasTenders = tenders.some((t) => Number(t.amount) !== 0);
          return (
            <div className="flex flex-col gap-6">
              <DataQualityPanel items={env.data_quality} />
              <SummaryGrid summary={env.summary} />

              <Card>
                <CardHeader>
                  <CardTitle>Sales by tender type</CardTitle>
                  <p className="text-sm text-muted-foreground">
                    Recognized tenders recorded against the station&apos;s shifts for the period.
                  </p>
                </CardHeader>
                <CardContent>
                  {!hasTenders ? (
                    <EmptyState
                      title="No tenders"
                      description="No recorded tenders for this station in the selected period."
                    />
                  ) : (
                    <BarChart
                      data={tenders}
                      xKey="tender"
                      valueFormatter={(v) => formatMoney(v as string)}
                      series={[{ key: 'amount', label: 'Tendered', color: chartColors.accent }]}
                      height={260}
                    />
                  )}
                </CardContent>
              </Card>

              <InsightPanel insights={env.insights} recommendedActions={env.recommended_actions} />

              <EnvelopeTable table={env.table} caption="Credit & cashflow detail" />

              <div className="flex flex-col gap-3">
                <DrilldownLinks links={env.drilldown} />
                <EnvelopeExports
                  options={env.export_options}
                  reportKey="financials"
                  filters={filters}
                  filenameBase={`credit-cashflow-${stationId.slice(0, 8)}-${period}`}
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
