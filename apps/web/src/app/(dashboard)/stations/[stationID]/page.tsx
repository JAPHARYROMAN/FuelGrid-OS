'use client';

import { useMemo, useState } from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { ChevronDown, ChevronRight } from 'lucide-react';

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
  LoadingState,
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
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

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

  function toggle(id: string) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function color(productID: string) {
    return productLookup.get(productID)?.color ?? '#64748b';
  }

  if (overview.isPending) return <LoadingState />;
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

  const { station, tanks, pumps, open_incidents: incidents } = overview.data;

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          <Link href="/stations" className="text-sm text-muted-foreground hover:text-foreground">
            Stations
          </Link>
          <ChevronRight className="size-3 text-muted-foreground" />
          <span className="text-sm text-muted-foreground">{station.code}</span>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">{station.name}</h1>
          <Badge tone={station.status === 'active' ? 'success' : 'warning'}>{station.status}</Badge>
          {[station.city, station.country].filter(Boolean).length ? (
            <span className="text-sm text-muted-foreground">
              {[station.city, station.country].filter(Boolean).join(', ')}
            </span>
          ) : null}
        </div>
      </header>

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
            {tanks.map((t) => {
              const product = productLookup.get(t.product_id);
              return (
                <Card key={t.id} className="relative overflow-hidden">
                  <span
                    className="absolute inset-y-0 left-0 w-1.5"
                    style={{ backgroundColor: color(t.product_id) }}
                    aria-hidden
                  />
                  <CardHeader className="pl-5">
                    <CardTitle className="flex items-center justify-between gap-2 text-base">
                      <span>{t.name}</span>
                      <span className="font-mono text-xs text-muted-foreground">{t.code}</span>
                    </CardTitle>
                  </CardHeader>
                  <CardContent className="flex flex-col gap-1 pl-5 text-sm">
                    <span className="inline-flex items-center gap-2">
                      <span
                        className="inline-block size-3 rounded-full border border-border"
                        style={{ backgroundColor: color(t.product_id) }}
                        aria-hidden
                      />
                      {product?.name ?? '—'}
                    </span>
                    <span className="text-muted-foreground">
                      Capacity{' '}
                      <span className="tabular-nums text-foreground">
                        {t.capacity_litres.toLocaleString()} L
                      </span>
                    </span>
                    {t.status !== 'active' ? (
                      <Badge tone="warning" className="mt-1 w-fit">
                        {t.status}
                      </Badge>
                    ) : null}
                  </CardContent>
                </Card>
              );
            })}
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
            {pumps.map((pump) => {
              const isOpen = expanded.has(pump.id);
              return (
                <Card key={pump.id}>
                  <CardHeader className="flex-row items-center justify-between gap-3 space-y-0">
                    <button
                      type="button"
                      className="flex items-center gap-2 text-left"
                      onClick={() => toggle(pump.id)}
                    >
                      {isOpen ? (
                        <ChevronDown className="size-4 text-muted-foreground" />
                      ) : (
                        <ChevronRight className="size-4 text-muted-foreground" />
                      )}
                      <span className="font-semibold">Pump {pump.number}</span>
                      <Badge tone={pump.status === 'active' ? 'success' : 'warning'}>
                        {pump.status}
                      </Badge>
                      <span className="text-xs text-muted-foreground">
                        {pump.nozzles.length} nozzle{pump.nozzles.length === 1 ? '' : 's'}
                      </span>
                    </button>
                    <Button variant="ghost" size="sm" asChild>
                      <Link href={`/stations/${stationID}/pumps/${pump.id}`}>Details</Link>
                    </Button>
                  </CardHeader>
                  {isOpen ? (
                    <CardContent>
                      {pump.nozzles.length === 0 ? (
                        <p className="text-sm text-muted-foreground">No nozzles.</p>
                      ) : (
                        <div className="flex flex-col divide-y divide-border">
                          {pump.nozzles
                            .slice()
                            .sort((a, b) => a.number - b.number)
                            .map((n) => {
                              const product = productLookup.get(n.product_id);
                              const tank = tankLookup.get(n.tank_id);
                              return (
                                <div
                                  key={n.id}
                                  className="flex items-center justify-between gap-3 py-2 text-sm"
                                >
                                  <div className="flex items-center gap-3">
                                    <span className="w-10 font-mono text-xs text-muted-foreground">
                                      N{n.number}
                                    </span>
                                    <span className="inline-flex items-center gap-2">
                                      <span
                                        className="inline-block size-3 rounded-full border border-border"
                                        style={{ backgroundColor: color(n.product_id) }}
                                        aria-hidden
                                      />
                                      {product?.name ?? '—'}
                                    </span>
                                    <span className="text-muted-foreground">
                                      ← {tank ? tank.code : 'tank'}
                                    </span>
                                  </div>
                                  <span className="tabular-nums">{n.default_price.toFixed(2)}</span>
                                </div>
                              );
                            })}
                        </div>
                      )}
                    </CardContent>
                  ) : null}
                </Card>
              );
            })}
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
