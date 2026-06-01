'use client';

import { useMemo } from 'react';
import Link from 'next/link';
import { useParams, useRouter } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { ChevronRight } from 'lucide-react';

import { SdkError, type Tank } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  PageHeader,
  PumpCard,
  Skeleton,
  TankVisual,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

function severityTone(s: string): 'neutral' | 'info' | 'warning' | 'danger' {
  switch (s) {
    case 'critical':
      return 'danger';
    case 'high':
      return 'warning';
    case 'medium':
      return 'info';
    default:
      return 'neutral';
  }
}

export default function StationDashboardPage() {
  const params = useParams<{ stationID: string }>();
  const stationID = params.stationID;
  const router = useRouter();

  const overview = useQuery({
    queryKey: ['station-overview', stationID],
    queryFn: ({ signal }) => api.getStationOverview(stationID, signal),
  });
  const products = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
  });

  const productLookup = useMemo(
    () => new Map((products.data?.items ?? []).map((p) => [p.id, p])),
    [products.data],
  );
  const tankLookup = useMemo(
    () => new Map((overview.data?.tanks ?? []).map((t: Tank) => [t.id, t])),
    [overview.data],
  );

  function color(productID: string) {
    return productLookup.get(productID)?.color ?? '#64748b';
  }

  if (overview.isPending) {
    return (
      <div className="flex flex-col gap-7">
        <Skeleton className="h-16 rounded-xl" />
        <Skeleton className="h-32 rounded-xl" />
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-44 rounded-xl" />
          ))}
        </div>
      </div>
    );
  }
  if (overview.isError) {
    const err = overview.error;
    const forbidden = err instanceof SdkError && err.status === 403;
    return (
      <ErrorState
        title={forbidden ? 'No access to this station' : "Couldn't load station"}
        description={
          forbidden
            ? "You don't have permission to view this station."
            : String((err as Error).message)
        }
        onRetry={forbidden ? undefined : () => overview.refetch()}
      />
    );
  }

  const { station, tanks, pumps, open_shifts: shifts, open_incidents: incidents } = overview.data;

  // Label assigned nozzles as "P{pump}·N{nozzle}" using the pumps already in
  // the overview.
  const nozzleLabel = new Map<string, string>();
  for (const p of pumps) {
    for (const n of p.nozzles) nozzleLabel.set(n.id, `P${p.number}·N${n.number}`);
  }

  const location = [station.city, station.country].filter(Boolean).join(', ');

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow={
          <span className="flex items-center gap-2">
            <Link href="/stations" className="hover:text-foreground">
              Stations
            </Link>
            <ChevronRight className="size-3" />
            <span className="font-mono">{station.code}</span>
          </span>
        }
        title={station.name}
        description={location || undefined}
        actions={
          <Badge tone={station.status === 'active' ? 'success' : 'warning'}>{station.status}</Badge>
        }
      />

      {/* Shifts strip */}
      <section className="flex flex-col gap-3">
        <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
          Active shifts
        </h2>
        {shifts.length === 0 ? (
          <p className="text-sm text-muted-foreground">No open shifts at this station.</p>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {shifts.map((shift) => (
              <Card key={shift.id}>
                <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
                  <CardTitle className="text-base">{shift.name}</CardTitle>
                  <Badge tone={shift.status === 'open' ? 'success' : 'neutral'}>
                    {shift.status}
                  </Badge>
                </CardHeader>
                <CardContent className="flex flex-col gap-2 text-sm">
                  <span className="text-muted-foreground">
                    {shift.attendants.length} attendant
                    {shift.attendants.length === 1 ? '' : 's'} · {shift.nozzle_assignments.length}{' '}
                    nozzle
                    {shift.nozzle_assignments.length === 1 ? '' : 's'} assigned
                  </span>
                  {shift.nozzle_assignments.length > 0 ? (
                    <div className="flex flex-wrap gap-1.5">
                      {shift.nozzle_assignments.map((na) => (
                        <span
                          key={na.id}
                          className="rounded-full bg-muted px-2 py-0.5 font-mono text-[11px] text-muted-foreground"
                        >
                          {nozzleLabel.get(na.nozzle_id) ?? 'nozzle'}
                        </span>
                      ))}
                    </div>
                  ) : null}
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </section>

      {/* Tanks strip */}
      <section className="flex flex-col gap-3">
        <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
          Tanks
        </h2>
        {tanks.length === 0 ? (
          <EmptyState
            title="No tanks yet"
            description="This station has no tanks configured."
            action={
              <Button asChild>
                <Link href="/settings/tanks">Add tanks</Link>
              </Button>
            }
          />
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {tanks.map((t) => (
              <TankVisual
                key={t.id}
                name={t.name}
                code={t.code}
                color={color(t.product_id)}
                capacityLitres={t.capacity_litres}
                safeMinLitres={t.safe_min_litres}
                safeMaxLitres={t.safe_max_litres}
                currentLitres={t.current_litres ?? null}
                status={t.status}
              />
            ))}
          </div>
        )}
      </section>

      {/* Pumps strip */}
      <section className="flex flex-col gap-3">
        <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
          Pumps
        </h2>
        {pumps.length === 0 ? (
          <EmptyState
            title="No pumps yet"
            description="This station has no pumps configured."
            action={
              <Button asChild>
                <Link href="/settings/pumps">Add pumps</Link>
              </Button>
            }
          />
        ) : (
          <div className="grid gap-3 lg:grid-cols-2">
            {pumps.map((pump) => (
              <PumpCard
                key={pump.id}
                number={pump.number}
                status={pump.status}
                nozzles={pump.nozzles.map((n) => ({
                  id: n.id,
                  number: n.number,
                  productName: productLookup.get(n.product_id)?.name ?? '—',
                  productColor: color(n.product_id),
                  tankCode: tankLookup.get(n.tank_id)?.code ?? 'tank',
                  price: n.default_price,
                }))}
                onActivate={() => router.push(`/stations/${stationID}/pumps/${pump.id}`)}
              />
            ))}
          </div>
        )}
      </section>

      {/* Open incidents */}
      <section className="flex flex-col gap-3">
        <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
          Open incidents
        </h2>
        {incidents.length === 0 ? (
          <p className="text-sm text-muted-foreground">No open incidents at this station.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Severity</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Description</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {incidents.map((inc) => (
                <TableRow key={inc.id}>
                  <TableCell>
                    <Badge tone={severityTone(inc.severity)}>{inc.severity}</Badge>
                  </TableCell>
                  <TableCell className="text-muted-foreground capitalize">{inc.type}</TableCell>
                  <TableCell className="max-w-md truncate">{inc.description}</TableCell>
                  <TableCell>
                    <Badge tone={inc.status === 'open' ? 'warning' : 'neutral'}>{inc.status}</Badge>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </section>
    </div>
  );
}
