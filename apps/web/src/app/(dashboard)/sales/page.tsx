'use client';

import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Coins, Percent, TrendingUp, Wallet } from 'lucide-react';

import { SdkError, type StationRank } from '@fuelgrid/sdk';
import {
  Badge,
  Card,
  CardContent,
  DataTable,
  EmptyState,
  ErrorState,
  PageHeader,
  Skeleton,
  Stat,
  type DataTableColumn,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatMoney, parseDecimal } from '@/lib/money';

export default function SalesPage() {
  // Network-level sales: the enterprise overview gives gross/net/margin for the
  // current window; station ranking ranks sites by gross revenue. Both reuse
  // existing read endpoints — no new routes.
  const overview = useQuery({
    queryKey: ['enterprise-overview'],
    queryFn: ({ signal }) => api.getEnterpriseOverview({}, signal),
  });
  const ranking = useQuery({
    queryKey: ['station-ranking'],
    queryFn: ({ signal }) => api.getStationRanking({}, signal),
  });

  const rankRows = ranking.data?.items ?? [];
  const topGross = useMemo(
    () => rankRows.reduce((max, r) => Math.max(max, parseDecimal(r.gross_revenue) ?? 0), 0),
    [rankRows],
  );

  const forbidden = overview.error instanceof SdkError && overview.error.status === 403;

  const columns: DataTableColumn<StationRank>[] = [
    {
      id: 'station',
      header: 'Station',
      sortValue: (r) => r.name,
      cell: (r) => <span className="font-medium text-foreground">{r.name}</span>,
    },
    {
      id: 'gross',
      header: 'Gross revenue',
      align: 'right',
      sortValue: (r) => parseDecimal(r.gross_revenue) ?? 0,
      cell: (r) => {
        const gross = parseDecimal(r.gross_revenue) ?? 0;
        const share = topGross > 0 ? (gross / topGross) * 100 : 0;
        return (
          <span className="flex items-center justify-end gap-2">
            <span className="h-1.5 w-20 overflow-hidden rounded-full bg-muted">
              <span
                className="block h-full rounded-full bg-accent"
                style={{ width: `${Math.max(2, share)}%` }}
              />
            </span>
            <span className="font-mono text-sm font-medium tabular-nums text-foreground">
              {formatMoney(r.gross_revenue)}
            </span>
          </span>
        );
      },
    },
    {
      id: 'margin',
      header: 'Margin',
      align: 'right',
      sortValue: (r) => parseDecimal(r.margin_total) ?? 0,
      cell: (r) => (
        <span className="font-mono text-sm tabular-nums text-muted-foreground">
          {formatMoney(r.margin_total)}
        </span>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Commerce"
        title="Sales"
        description="Network sales performance — gross and net revenue, margin, and how each station ranks."
      />

      {overview.isPending ? (
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-[120px] rounded-xl" />
          ))}
        </section>
      ) : overview.isError ? (
        <ErrorState
          title={forbidden ? 'No access to network sales' : "Couldn't load sales"}
          description={
            forbidden
              ? "You don't have permission to view network-level sales."
              : String((overview.error as Error).message)
          }
          onRetry={forbidden ? undefined : () => overview.refetch()}
        />
      ) : (
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <Stat
            label="Gross revenue"
            value={formatMoney(overview.data.gross_revenue)}
            hint={`${overview.data.from} → ${overview.data.to}`}
            icon={<Coins />}
          />
          <Stat
            label="Net revenue"
            value={formatMoney(overview.data.net_revenue)}
            icon={<Wallet />}
          />
          <Stat label="Margin" value={formatMoney(overview.data.margin_total)} icon={<Percent />} />
          <Stat
            label="Receivables"
            value={formatMoney(overview.data.ar_outstanding)}
            hint="outstanding"
            icon={<TrendingUp />}
          />
        </section>
      )}

      <Card>
        <CardContent className="p-0">
          {ranking.isPending ? (
            <div className="flex flex-col gap-2 p-4">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-12 rounded-lg" />
              ))}
            </div>
          ) : ranking.isError ? (
            <div className="p-4">
              <ErrorState
                title="Couldn't load station ranking"
                description={String((ranking.error as Error).message)}
                onRetry={() => ranking.refetch()}
              />
            </div>
          ) : rankRows.length === 0 ? (
            <div className="p-6">
              <EmptyState
                title="No sales yet"
                description="Station sales will rank here once revenue is recognized."
              />
            </div>
          ) : (
            <div className="flex flex-col">
              <div className="flex items-center justify-between border-b border-border px-4 py-3">
                <span className="text-sm font-semibold text-foreground">Station ranking</span>
                <Badge tone="neutral">{rankRows.length} stations</Badge>
              </div>
              <div className="max-h-[560px] overflow-auto">
                <DataTable
                  columns={columns}
                  rows={rankRows}
                  rowKey={(r) => r.station_id}
                  defaultSort={{ columnId: 'gross', direction: 'desc' }}
                />
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
