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

/** One product slice of the profitability chart_data payload (decimal strings). */
interface ProfitProductSlice {
  product: string;
  litres: string;
  revenue: string;
  cogs: string;
  gross_margin: string;
}

export default function ProfitabilityPage() {
  const { stations, items, stationId, setStationId } = useStationSelection();
  const [period, setPeriod] = React.useState<ReportPeriod>('this-month');
  const allowed = usePermission('revenue.read', { stationID: stationId });

  const report = useQuery({
    queryKey: ['report', 'profitability', stationId, period],
    queryFn: ({ signal }) => api.getProfitabilityReport(stationId, { period }, signal),
    enabled: !!stationId,
  });

  const filters = { station_id: stationId, period };

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Profitability"
        title="Profitability"
        description="Net revenue, COGS, gross margin, operating expenses and net operating result for a station over the selected period, broken down by product. Money and litres are exact decimals throughout."
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
        loadingLabel="profitability report"
      >
        {(env: ReportEnvelope) => {
          const products = (env.chart_data as ProfitProductSlice[] | null) ?? [];
          return (
            <div className="flex flex-col gap-6">
              <DataQualityPanel items={env.data_quality} />
              <SummaryGrid summary={env.summary} />

              <Card>
                <CardHeader>
                  <CardTitle>Revenue and gross margin by product</CardTitle>
                  <p className="text-sm text-muted-foreground">
                    Net revenue and the gross margin it yields, per product, for the period.
                  </p>
                </CardHeader>
                <CardContent>
                  {products.length === 0 ? (
                    <EmptyState
                      title="No product sales"
                      description="No recognized sales for this station in the selected period."
                    />
                  ) : (
                    <BarChart
                      data={products}
                      xKey="product"
                      valueFormatter={(v) => formatMoney(v as string)}
                      series={[
                        { key: 'revenue', label: 'Revenue', color: chartColors.accent },
                        { key: 'gross_margin', label: 'Gross margin', color: chartColors.success },
                      ]}
                      height={260}
                    />
                  )}
                </CardContent>
              </Card>

              <InsightPanel insights={env.insights} recommendedActions={env.recommended_actions} />

              <EnvelopeTable table={env.table} caption="Per-product profitability" />

              <div className="flex flex-col gap-3">
                <DrilldownLinks links={env.drilldown} />
                <EnvelopeExports
                  options={env.export_options}
                  reportKey="financials"
                  filters={filters}
                  filenameBase={`profitability-${stationId.slice(0, 8)}-${period}`}
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
