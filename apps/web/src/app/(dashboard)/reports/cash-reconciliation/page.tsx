'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { Banknote, Scale, Wallet } from 'lucide-react';

import { SdkError } from '@fuelgrid/sdk';
import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  PageHeader,
  Skeleton,
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

function statusTone(status: string): 'success' | 'warning' | 'neutral' {
  if (status === 'posted' || status === 'approved') return 'success';
  if (status === 'submitted' || status === 'draft') return 'warning';
  return 'neutral';
}

export default function CashReconciliationPage() {
  const { stations, items, stationId, setStationId, current } = useStationSelection();

  const recons = useQuery({
    queryKey: ['cash-reconciliations', stationId],
    queryFn: ({ signal }) => api.listCashReconciliations(stationId, signal),
    enabled: !!stationId,
  });
  const overview = useQuery({
    queryKey: ['revenue-overview', stationId],
    queryFn: ({ signal }) => api.getRevenueOverview(stationId, signal),
    enabled: !!stationId,
  });
  const insights = useQuery({
    queryKey: ['report-insights', 'cash-reconciliation', stationId],
    queryFn: ({ signal }) =>
      api.getReportInsights('cash-reconciliation', { stationID: stationId }, signal),
    enabled: !!stationId,
  });

  const stationCode = current?.code ?? 'station';
  const rows = recons.data?.items ?? [];
  const latest = rows[0];
  const cashToday = overview.data?.tenders?.cash;
  const locked = overview.data?.recent_days?.[0]?.status === 'locked';

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Cash"
        title="Cash Reconciliation"
        description="Expected vs counted cash by operating day, with the variance and posting status."
        actions={
          items.length > 0 ? (
            <div className="flex items-center gap-3">
              {overview.data?.recent_days?.[0] ? <FinalBadge locked={locked} /> : null}
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
                    filename: `cash-${stationCode}.csv`,
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
                    filename: `cash-${stationCode}.xlsx`,
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

      {stations.isPending || recons.isPending ? (
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-[120px] rounded-xl" />
          ))}
        </section>
      ) : items.length === 0 ? (
        <EmptyState
          title="No stations yet"
          description="Create a station to view cash reconciliations."
        />
      ) : recons.isError ? (
        (() => {
          const forbidden = recons.error instanceof SdkError && recons.error.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access to this station' : "Couldn't load cash reconciliations"}
              description={
                forbidden
                  ? "You don't have permission to view this station."
                  : String((recons.error as Error).message)
              }
              onRetry={forbidden ? undefined : () => recons.refetch()}
            />
          );
        })()
      ) : (
        <>
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <Stat
              label="Cash today"
              value={cashToday ? formatMoney(cashToday) : '—'}
              icon={<Banknote />}
            />
            <Stat
              label="Latest expected"
              value={latest ? formatMoney(latest.expected_cash) : '—'}
              icon={<Wallet />}
            />
            <Stat
              label="Latest variance"
              value={latest ? formatMoney(latest.variance) : '—'}
              icon={<Scale />}
            />
          </section>

          <Card>
            <CardHeader>
              <CardTitle>Reconciliations</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {rows.length === 0 ? (
                <div className="p-4">
                  <EmptyState
                    title="No reconciliations"
                    description="No cash reconciliations recorded for this station."
                  />
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Operating day</TableHead>
                      <TableHead className="text-right">Expected</TableHead>
                      <TableHead className="text-right">Counted</TableHead>
                      <TableHead className="text-right">Variance</TableHead>
                      <TableHead>Status</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {rows.map((c) => (
                      <TableRow key={c.id}>
                        <TableCell className="font-mono text-xs">
                          {c.operating_day_id.slice(0, 8)}
                        </TableCell>
                        <TableCell className="text-right font-mono tabular-nums">
                          {formatMoney(c.expected_cash)}
                        </TableCell>
                        <TableCell className="text-right font-mono tabular-nums">
                          {formatMoney(c.counted_cash)}
                        </TableCell>
                        <TableCell className="text-right font-mono tabular-nums">
                          {formatMoney(c.variance)}
                        </TableCell>
                        <TableCell>
                          <Badge tone={statusTone(c.status)}>{c.status}</Badge>
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
