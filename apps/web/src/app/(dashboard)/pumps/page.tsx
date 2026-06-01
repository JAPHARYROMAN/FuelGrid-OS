'use client';

import { useMemo } from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { CheckCircle2, Fuel, Gauge } from 'lucide-react';

import type { Pump, Station } from '@fuelgrid/sdk';
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

interface PumpRow {
  pump: Pump;
  station?: Station;
  nozzleCount: number;
}

function statusTone(status: string): 'success' | 'warning' | 'neutral' {
  if (status === 'active') return 'success';
  if (status === 'decommissioned') return 'neutral';
  return 'warning';
}

export default function PumpsPage() {
  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  // Network-wide pumps and nozzles (no stationID returns everything in scope).
  const pumps = useQuery({
    queryKey: ['pumps', 'network'],
    queryFn: ({ signal }) => api.listPumps({}, signal),
  });
  const nozzles = useQuery({
    queryKey: ['nozzles', 'network'],
    queryFn: ({ signal }) => api.listNozzles({}, signal),
  });

  const stationLookup = useMemo(
    () => new Map((stations.data?.items ?? []).map((s) => [s.id, s])),
    [stations.data],
  );
  const nozzleCountByPump = useMemo(() => {
    const m = new Map<string, number>();
    for (const n of nozzles.data?.items ?? []) {
      m.set(n.pump_id, (m.get(n.pump_id) ?? 0) + 1);
    }
    return m;
  }, [nozzles.data]);

  const rows = useMemo<PumpRow[]>(
    () =>
      (pumps.data?.items ?? []).map((pump) => ({
        pump,
        station: stationLookup.get(pump.station_id),
        nozzleCount: nozzleCountByPump.get(pump.id) ?? 0,
      })),
    [pumps.data, stationLookup, nozzleCountByPump],
  );

  const loading = pumps.isPending || stations.isPending || nozzles.isPending;

  const activeCount = rows.filter((r) => r.pump.status === 'active').length;
  const totalNozzles = (nozzles.data?.items ?? []).length;

  const columns: DataTableColumn<PumpRow>[] = [
    {
      id: 'pump',
      header: 'Pump',
      sortValue: (r) => r.pump.number,
      cell: (r) => (
        <span className="flex flex-col">
          <span className="font-medium text-foreground">Pump {r.pump.number}</span>
          {r.pump.name ? (
            <span className="text-xs text-muted-foreground">{r.pump.name}</span>
          ) : null}
        </span>
      ),
    },
    {
      id: 'station',
      header: 'Station',
      sortValue: (r) => r.station?.name ?? '',
      cell: (r) =>
        r.station ? (
          <Link
            href={`/stations/${r.station.id}`}
            className="flex flex-col hover:underline"
            onClick={(e) => e.stopPropagation()}
          >
            <span className="text-foreground">{r.station.name}</span>
            <span className="font-mono text-xs text-muted-foreground">{r.station.code}</span>
          </Link>
        ) : (
          <span className="text-muted-foreground">—</span>
        ),
    },
    {
      id: 'make',
      header: 'Make / model',
      sortValue: (r) => r.pump.manufacturer ?? '',
      cell: (r) => {
        const parts = [r.pump.manufacturer, r.pump.model].filter(Boolean);
        return parts.length ? (
          <span className="text-foreground">{parts.join(' · ')}</span>
        ) : (
          <span className="text-muted-foreground">—</span>
        );
      },
    },
    {
      id: 'nozzles',
      header: 'Nozzles',
      align: 'right',
      sortValue: (r) => r.nozzleCount,
      cell: (r) => (
        <span className="font-mono text-sm tabular-nums text-foreground">{r.nozzleCount}</span>
      ),
    },
    {
      id: 'status',
      header: 'Status',
      sortValue: (r) => r.pump.status,
      cell: (r) => <Badge tone={statusTone(r.pump.status)}>{r.pump.status}</Badge>,
    },
  ];

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Operations"
        title="Pumps"
        description="Every dispensing unit across the network — its station, hardware, and nozzle count."
      />

      {pumps.isError ? (
        <ErrorState
          title="Couldn't load pumps"
          description={String((pumps.error as Error).message)}
          onRetry={() => pumps.refetch()}
        />
      ) : loading ? (
        <>
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            {Array.from({ length: 3 }).map((_, i) => (
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
          title="No pumps yet"
          description="Add pumps to your stations under Settings."
          icon={<Fuel />}
        />
      ) : (
        <>
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <Stat label="Pumps" value={rows.length} hint="across the network" icon={<Fuel />} />
            <Stat label="Active" value={activeCount} hint="in service" icon={<CheckCircle2 />} />
            <Stat label="Nozzles" value={totalNozzles} hint="total" icon={<Gauge />} />
          </section>

          <Card>
            <CardContent className="max-h-[640px] overflow-auto p-0">
              <DataTable
                columns={columns}
                rows={rows}
                rowKey={(r) => r.pump.id}
                defaultSort={{ columnId: 'station', direction: 'asc' }}
              />
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
