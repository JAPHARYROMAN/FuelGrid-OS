'use client';

import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Database, Droplet, Gauge } from 'lucide-react';

import type { Product, Station, Tank } from '@fuelgrid/sdk';
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
import { formatLitres, parseDecimal } from '@/lib/money';

interface TankRow {
  tank: Tank;
  station?: Station;
  product?: Product;
  capacity: number;
  /** Book/current litres, when a dip has resolved a volume. */
  current?: number;
  fillPercent?: number;
}

function fillTone(pct: number): 'success' | 'warning' | 'danger' {
  if (pct <= 15) return 'danger';
  if (pct >= 95) return 'warning';
  return 'success';
}

function statusTone(status: string): 'success' | 'warning' | 'neutral' {
  if (status === 'active') return 'success';
  if (status === 'decommissioned') return 'neutral';
  return 'warning';
}

export default function TanksPage() {
  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  // Network-wide tanks (no stationID returns every tank in scope).
  const tanks = useQuery({
    queryKey: ['tanks'],
    queryFn: ({ signal }) => api.listTanks({}, signal),
  });
  const products = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
  });

  const stationLookup = useMemo(
    () => new Map((stations.data?.items ?? []).map((s) => [s.id, s])),
    [stations.data],
  );
  const productLookup = useMemo(
    () => new Map((products.data?.items ?? []).map((p) => [p.id, p])),
    [products.data],
  );

  const rows = useMemo<TankRow[]>(() => {
    return (tanks.data?.items ?? []).map((tank) => {
      const capacity = parseDecimal(tank.capacity_litres) ?? 0;
      const current = tank.current_litres;
      const fillPercent =
        current != null && capacity > 0
          ? Math.max(0, Math.min(100, (current / capacity) * 100))
          : undefined;
      return {
        tank,
        station: stationLookup.get(tank.station_id),
        product: productLookup.get(tank.product_id),
        capacity,
        current: current ?? undefined,
        fillPercent,
      };
    });
  }, [tanks.data, stationLookup, productLookup]);

  const loading = tanks.isPending || stations.isPending || products.isPending;

  const activeCount = rows.filter((r) => r.tank.status === 'active').length;
  const totalCapacity = rows.reduce((sum, r) => sum + r.capacity, 0);
  const lowCount = rows.filter((r) => r.fillPercent != null && r.fillPercent <= 15).length;

  const columns: DataTableColumn<TankRow>[] = [
    {
      id: 'tank',
      header: 'Tank',
      sortValue: (r) => r.tank.name,
      cell: (r) => (
        <span className="flex flex-col">
          <span className="font-medium text-foreground">{r.tank.name}</span>
          <span className="font-mono text-xs text-muted-foreground">{r.tank.code}</span>
        </span>
      ),
    },
    {
      id: 'station',
      header: 'Station',
      sortValue: (r) => r.station?.name ?? '',
      cell: (r) =>
        r.station ? (
          <span className="flex flex-col">
            <span className="text-foreground">{r.station.name}</span>
            <span className="font-mono text-xs text-muted-foreground">{r.station.code}</span>
          </span>
        ) : (
          <span className="text-muted-foreground">—</span>
        ),
    },
    {
      id: 'product',
      header: 'Product',
      sortValue: (r) => r.product?.name ?? '',
      cell: (r) =>
        r.product ? (
          <span className="inline-flex items-center gap-2">
            <span
              className="inline-block size-3 rounded-full border border-border"
              style={{ backgroundColor: r.product.color }}
              aria-hidden
            />
            {r.product.name}
          </span>
        ) : (
          <span className="text-muted-foreground">—</span>
        ),
    },
    {
      id: 'fill',
      header: 'Fill',
      align: 'right',
      sortValue: (r) => r.fillPercent ?? -1,
      cell: (r) =>
        r.fillPercent != null ? (
          <span className="flex items-center justify-end gap-2">
            <span className="h-1.5 w-16 overflow-hidden rounded-full bg-muted">
              <span
                className={
                  'block h-full rounded-full ' +
                  (fillTone(r.fillPercent) === 'danger'
                    ? 'bg-danger'
                    : fillTone(r.fillPercent) === 'warning'
                      ? 'bg-warning'
                      : 'bg-success')
                }
                style={{ width: `${r.fillPercent}%` }}
              />
            </span>
            <span className="font-mono text-sm tabular-nums text-foreground">
              {Math.round(r.fillPercent)}%
            </span>
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">no dip</span>
        ),
    },
    {
      id: 'capacity',
      header: 'Capacity',
      align: 'right',
      sortValue: (r) => r.capacity,
      cell: (r) => (
        <span className="font-mono text-sm tabular-nums text-foreground">
          {formatLitres(r.tank.capacity_litres, { fallback: '0' })} L
        </span>
      ),
    },
    {
      id: 'status',
      header: 'Status',
      sortValue: (r) => r.tank.status,
      cell: (r) => <Badge tone={statusTone(r.tank.status)}>{r.tank.status}</Badge>,
    },
  ];

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Operations"
        title="Tanks"
        description="Every storage tank across the network — fill level, product, and operational status."
      />

      {tanks.isError ? (
        <ErrorState
          title="Couldn't load tanks"
          description={String((tanks.error as Error).message)}
          onRetry={() => tanks.refetch()}
        />
      ) : loading ? (
        <>
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-[120px] rounded-xl" />
            ))}
          </section>
          <Card>
            <CardContent className="flex flex-col gap-2 p-4">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-12 rounded-lg" />
              ))}
            </CardContent>
          </Card>
        </>
      ) : rows.length === 0 ? (
        <EmptyState
          title="No tanks yet"
          description="Add tanks to your stations under Settings."
          icon={<Database />}
        />
      ) : (
        <>
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <Stat label="Tanks" value={rows.length} hint="across the network" icon={<Database />} />
            <Stat label="Active" value={activeCount} hint="in service" icon={<Gauge />} />
            <Stat
              label="Total capacity"
              value={`${formatLitres(String(totalCapacity), { fallback: '0' })} L`}
              icon={<Droplet />}
            />
            <Stat label="Low stock" value={lowCount} hint="at or below 15%" icon={<Droplet />} />
          </section>

          <Card>
            <CardContent className="max-h-[640px] overflow-auto p-0">
              <DataTable
                columns={columns}
                rows={rows}
                rowKey={(r) => r.tank.id}
                defaultSort={{ columnId: 'fill', direction: 'asc' }}
              />
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
