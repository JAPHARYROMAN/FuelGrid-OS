'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { Coins, Percent, Receipt, Wallet } from 'lucide-react';

import { SdkError, type RevenueDay } from '@fuelgrid/sdk';
import {
  Badge,
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
import { formatMoney } from '@/lib/money';

import { ReportDownloads } from '../_components/downloads';
import { FinalBadge, StationSelect, useStationSelection } from '../_components/filters';
import { ReportInsightsPanel } from '../_components/insights';

function shortDate(d: unknown): string {
  const s = String(d ?? '');
  return s.length >= 10 ? s.slice(5) : s;
}

export default function DailyClosePage() {
  const { stations, items, stationId, setStationId, current } = useStationSelection();

  const overview = useQuery({
    queryKey: ['revenue-overview', stationId],
    queryFn: ({ signal }) => api.getRevenueOverview(stationId, signal),
    enabled: !!stationId,
  });
  const insights = useQuery({
    queryKey: ['report-insights', 'daily-close', stationId],
    queryFn: ({ signal }) => api.getReportInsights('daily-close', { stationID: stationId }, signal),
    enabled: !!stationId,
  });

  const stationCode = current?.code ?? 'station';
  const days = overview.data?.recent_days ?? [];
  const trend = [...days].reverse();
  const summary = overview.data?.summary;
  const latest = days[0];
  const locked = latest?.status === 'locked';

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Daily close"
        title="Daily Station Close"
        description="Each operating day's recognized revenue, margin, tender split and cash position."
        actions={
          items.length > 0 ? (
            <div className="flex items-center gap-3">
              {latest ? <FinalBadge locked={locked} /> : null}
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
                    filename: `daily-close-${stationCode}.csv`,
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
                    filename: `daily-close-${stationCode}.xlsx`,
                  }
                : null,
          },
          {
            label: 'PDF',
            format: 'pdf',
            build: () =>
              stationId
                ? {
                    spec: { kind: 'daily-close-pdf', stationID: stationId },
                    filename: `daily-close-${stationCode}.pdf`,
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
        <EmptyState
          title="No stations yet"
          description="Create a station to view its daily close."
        />
      ) : overview.isError ? (
        (() => {
          const forbidden = overview.error instanceof SdkError && overview.error.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access to this station' : "Couldn't load the report"}
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
              <Stat label="Gross" value={formatMoney(summary.gross_revenue)} icon={<Coins />}>
                {trend.length >= 2 ? <Sparkline data={trend} valueKey="gross_revenue" /> : null}
              </Stat>
              <Stat label="Net" value={formatMoney(summary.net_revenue)} icon={<Wallet />} />
              <Stat label="Tax" value={formatMoney(summary.tax_total)} icon={<Receipt />} />
              <Stat
                label="Margin"
                value={formatMoney(summary.margin_total)}
                hint={`COGS ${formatMoney(summary.cogs_total)}`}
                icon={<Percent />}
              />
            </section>
          ) : null}

          {trend.length >= 2 ? (
            <Card>
              <CardHeader>
                <CardTitle>Recent days</CardTitle>
                <p className="text-sm text-muted-foreground">
                  Gross revenue and margin across recent days.
                </p>
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
              <CardTitle>Daily close detail</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {days.length === 0 ? (
                <div className="p-4">
                  <EmptyState
                    title="No revenue days"
                    description="No recognized days for this station yet."
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
                      <TableHead className="text-right">Cash variance</TableHead>
                      <TableHead>Status</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {days.map((d: RevenueDay) => (
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
                          {formatMoney(d.cash_variance)}
                        </TableCell>
                        <TableCell>
                          <Badge tone={d.status === 'locked' ? 'success' : 'warning'}>
                            {d.status}
                          </Badge>
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
