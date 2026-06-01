'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { Coins, Droplet, Percent, ShoppingCart } from 'lucide-react';

import { SdkError } from '@fuelgrid/sdk';
import {
  BarChart,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  chartColors,
  EmptyState,
  ErrorState,
  LineChart,
  PageHeader,
  Skeleton,
  Sparkline,
  Stat,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatLitres, formatMoney } from '@/lib/money';

import { ReportDownloads } from '../_components/downloads';
import { FinalBadge, StationSelect, useStationSelection } from '../_components/filters';
import { ReportInsightsPanel } from '../_components/insights';

function shortDate(d: unknown): string {
  const s = String(d ?? '');
  return s.length >= 10 ? s.slice(5) : s;
}

export default function SalesSummaryPage() {
  const { stations, items, stationId, setStationId, current } = useStationSelection();

  const overview = useQuery({
    queryKey: ['revenue-overview', stationId],
    queryFn: ({ signal }) => api.getRevenueOverview(stationId, signal),
    enabled: !!stationId,
  });
  const insights = useQuery({
    queryKey: ['report-insights', 'sales-summary', stationId],
    queryFn: ({ signal }) =>
      api.getReportInsights('sales-summary', { stationID: stationId }, signal),
    enabled: !!stationId,
  });

  const stationCode = current?.code ?? 'station';
  const days = overview.data?.recent_days ?? [];
  const trend = [...days].reverse();
  const summary = overview.data?.summary;
  const tenders = overview.data?.tenders;
  const locked = days[0]?.status === 'locked';

  const tenderData = tenders
    ? [
        { tender: 'Cash', amount: tenders.cash },
        { tender: 'Mobile', amount: tenders.mobile_money },
        { tender: 'Card', amount: tenders.card },
        { tender: 'Credit', amount: tenders.credit },
        { tender: 'Voucher', amount: tenders.voucher },
      ]
    : [];

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Sales"
        title="Sales Summary"
        description="Gross sales, litres, margin and tender mix across recent operating days."
        actions={
          items.length > 0 ? (
            <div className="flex items-center gap-3">
              {days[0] ? <FinalBadge locked={locked} /> : null}
              <StationSelect items={items} value={stationId} onChange={setStationId} />
            </div>
          ) : undefined
        }
      />

      <ReportDownloads
        permission="revenue.read"
        stationId={stationId}
        downloads={[
          {
            label: 'CSV',
            format: 'csv',
            build: () =>
              stationId
                ? {
                    spec: { kind: 'revenue', stationID: stationId },
                    filename: `sales-${stationCode}.csv`,
                  }
                : null,
          },
          {
            label: 'Excel',
            format: 'xlsx',
            build: () =>
              stationId
                ? {
                    spec: { kind: 'revenue-xlsx', stationID: stationId },
                    filename: `sales-${stationCode}.xlsx`,
                  }
                : null,
          },
        ]}
      />

      <ReportInsightsPanel
        data={insights.data}
        isPending={insights.isPending}
        isError={insights.isError}
      />

      {stations.isPending || overview.isPending ? (
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-[120px] rounded-xl" />
          ))}
        </section>
      ) : items.length === 0 ? (
        <EmptyState title="No stations yet" description="Create a station to view sales." />
      ) : overview.isError ? (
        (() => {
          const forbidden = overview.error instanceof SdkError && overview.error.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access to this station' : "Couldn't load sales"}
              description={
                forbidden
                  ? "You don't have permission to view this station."
                  : String((overview.error as Error).message)
              }
              onRetry={forbidden ? undefined : () => overview.refetch()}
            />
          );
        })()
      ) : (
        <>
          {summary ? (
            <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
              <Stat label="Gross sales" value={formatMoney(summary.gross_revenue)} icon={<Coins />}>
                {trend.length >= 2 ? <Sparkline data={trend} valueKey="gross_revenue" /> : null}
              </Stat>
              <Stat
                label="Litres sold"
                value={formatLitres(summary.litres_sold)}
                icon={<Droplet />}
              />
              <Stat label="Sales" value={String(summary.sale_count ?? 0)} icon={<ShoppingCart />} />
              <Stat label="Margin" value={formatMoney(summary.margin_total)} icon={<Percent />} />
            </section>
          ) : null}

          {tenders ? (
            <Card>
              <CardHeader>
                <CardTitle>Tender mix</CardTitle>
                <p className="text-sm text-muted-foreground">Today&apos;s payments by method.</p>
              </CardHeader>
              <CardContent>
                <BarChart
                  data={tenderData}
                  xKey="tender"
                  series={[{ key: 'amount', label: 'Amount' }]}
                  valueFormatter={(v) => formatMoney(v as string)}
                  height={200}
                />
              </CardContent>
            </Card>
          ) : null}

          {trend.length >= 2 ? (
            <Card>
              <CardHeader>
                <CardTitle>Gross vs margin</CardTitle>
              </CardHeader>
              <CardContent>
                <LineChart
                  data={trend}
                  xKey="business_date"
                  xFormatter={shortDate}
                  valueFormatter={(v) => formatMoney(v as string)}
                  series={[
                    { key: 'gross_revenue', label: 'Gross', color: chartColors.accent },
                    { key: 'margin_total', label: 'Margin', color: chartColors.success },
                  ]}
                  height={240}
                />
              </CardContent>
            </Card>
          ) : null}

          <Card>
            <CardHeader>
              <CardTitle>Daily sales</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {days.length === 0 ? (
                <div className="p-4">
                  <EmptyState
                    title="No sales days"
                    description="No recognized sales for this station yet."
                  />
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Business date</TableHead>
                      <TableHead className="text-right">Gross</TableHead>
                      <TableHead className="text-right">Net</TableHead>
                      <TableHead className="text-right">Margin</TableHead>
                      <TableHead className="text-right">Credit</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {days.map((d) => (
                      <TableRow key={d.id}>
                        <TableCell>{d.business_date}</TableCell>
                        <TableCell className="text-right font-mono tabular-nums">
                          {formatMoney(d.gross_revenue)}
                        </TableCell>
                        <TableCell className="text-right font-mono tabular-nums">
                          {formatMoney(d.net_revenue)}
                        </TableCell>
                        <TableCell className="text-right font-mono tabular-nums">
                          {formatMoney(d.margin_total)}
                        </TableCell>
                        <TableCell className="text-right font-mono tabular-nums">
                          {formatMoney(d.credit_total)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
