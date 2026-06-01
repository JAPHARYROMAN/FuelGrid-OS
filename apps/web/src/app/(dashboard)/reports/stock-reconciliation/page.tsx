'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { Database, Droplet, Scale } from 'lucide-react';

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
import { formatLitres } from '@/lib/money';

import { ReportDownloads } from '../_components/downloads';
import { FinalBadge, StationSelect, useStationSelection } from '../_components/filters';
import { ReportInsightsPanel } from '../_components/insights';

function pct(value: string | undefined): string {
  if (!value) return '—';
  return `${value}%`;
}

function statusTone(status?: string): 'success' | 'warning' | 'danger' | 'neutral' {
  if (status === 'sealed') return 'success';
  if (status === 'exception') return 'danger';
  if (status === 'draft') return 'warning';
  return 'neutral';
}

export default function StockReconciliationPage() {
  const { stations, items, stationId, setStationId, current } = useStationSelection();

  const overview = useQuery({
    queryKey: ['reconciliation-overview', stationId],
    queryFn: ({ signal }) => api.getReconciliationOverview(stationId, {}, signal),
    enabled: !!stationId,
  });
  const insights = useQuery({
    queryKey: ['report-insights', 'stock-reconciliation', stationId],
    queryFn: ({ signal }) =>
      api.getReportInsights('stock-reconciliation', { stationID: stationId }, signal),
    enabled: !!stationId,
  });

  const stationCode = current?.code ?? 'station';
  const tanks = overview.data?.tanks ?? [];
  const day = overview.data?.day;
  const allApproved = overview.data?.all_shifts_approved ?? false;
  const overCount = tanks.filter((t) => t.reconciliation?.over_tolerance).length;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Inventory"
        title="Fuel Stock Reconciliation"
        description="Per-tank book→physical variance for the active operating day."
        actions={
          items.length > 0 ? (
            <div className="flex items-center gap-3">
              {day ? <FinalBadge locked={allApproved} /> : null}
              <StationSelect items={items} value={stationId} onChange={setStationId} />
            </div>
          ) : undefined
        }
      />

      <ReportDownloads
        permission="reconciliation.read"
        stationId={stationId}
        downloads={[
          {
            label: 'CSV',
            format: 'csv',
            build: () =>
              stationId
                ? {
                    spec: { kind: 'reconciliation', stationID: stationId },
                    filename: `reconciliation-${stationCode}.csv`,
                  }
                : null,
          },
          {
            label: 'Excel',
            format: 'xlsx',
            build: () =>
              stationId
                ? {
                    spec: { kind: 'reconciliation-xlsx', stationID: stationId },
                    filename: `reconciliation-${stationCode}.xlsx`,
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
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-[120px] rounded-xl" />
          ))}
        </section>
      ) : items.length === 0 ? (
        <EmptyState
          title="No stations yet"
          description="Create a station to view reconciliation."
        />
      ) : overview.isError ? (
        (() => {
          const forbidden = overview.error instanceof SdkError && overview.error.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access to this station' : "Couldn't load reconciliation"}
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
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <Stat label="Tanks" value={String(tanks.length)} icon={<Database />} />
            <Stat label="Over tolerance" value={String(overCount)} icon={<Scale />} />
            <Stat
              label="Operating day"
              value={day?.business_date ?? 'None'}
              hint={allApproved ? 'All shifts approved' : 'Shifts pending'}
              icon={<Droplet />}
            />
          </section>

          <Card>
            <CardHeader>
              <CardTitle>Per-tank reconciliation</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {tanks.length === 0 ? (
                <div className="p-4">
                  <EmptyState
                    title="No tanks"
                    description="No tanks or no active day for this station."
                  />
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Tank</TableHead>
                      <TableHead className="text-right">Closing book</TableHead>
                      <TableHead className="text-right">Physical</TableHead>
                      <TableHead className="text-right">Variance (L)</TableHead>
                      <TableHead className="text-right">Variance %</TableHead>
                      <TableHead className="text-right">Tolerance %</TableHead>
                      <TableHead>Status</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {tanks.map((t) => {
                      const rec = t.reconciliation;
                      return (
                        <TableRow key={t.tank.id}>
                          <TableCell>
                            {t.tank.code}{' '}
                            <span className="text-muted-foreground">{t.tank.name}</span>
                          </TableCell>
                          <TableCell className="text-right font-mono tabular-nums">
                            {formatLitres(rec?.closing_book ?? t.book_balance)}
                          </TableCell>
                          <TableCell className="text-right font-mono tabular-nums">
                            {t.latest_physical != null ? formatLitres(t.latest_physical) : '—'}
                          </TableCell>
                          <TableCell className="text-right font-mono tabular-nums">
                            {rec ? formatLitres(rec.variance_litres) : '—'}
                          </TableCell>
                          <TableCell className="text-right font-mono tabular-nums">
                            {pct(rec?.variance_percent)}
                          </TableCell>
                          <TableCell className="text-right font-mono tabular-nums">
                            {pct(rec?.tolerance_percent)}
                          </TableCell>
                          <TableCell>
                            {rec ? (
                              <Badge tone={statusTone(rec.status)}>{rec.status}</Badge>
                            ) : (
                              <span className="text-muted-foreground">—</span>
                            )}
                          </TableCell>
                        </TableRow>
                      );
                    })}
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
