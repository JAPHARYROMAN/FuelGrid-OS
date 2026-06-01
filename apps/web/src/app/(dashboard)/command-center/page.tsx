'use client';

import * as React from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import {
  ArrowRight,
  Building2,
  ChevronRight,
  Database,
  Droplet,
  Package,
  Rocket,
} from 'lucide-react';

import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  ErrorState,
  PageHeader,
  Skeleton,
  Stat,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { setSentryUser } from '@/lib/sentry';

export default function CommandCenterPage() {
  const meQuery = useQuery({ queryKey: ['me'], queryFn: ({ signal }) => api.me(signal) });
  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  const tanks = useQuery({
    queryKey: ['tanks'],
    queryFn: ({ signal }) => api.listTanks({}, signal),
  });
  const products = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
  });
  const companies = useQuery({
    queryKey: ['companies'],
    queryFn: ({ signal }) => api.listCompanies(signal),
  });

  const me = meQuery.data;
  React.useEffect(() => {
    if (me) setSentryUser({ id: me.user_id, tenantId: me.tenant_id });
  }, [me]);

  const stationItems = stations.data?.items ?? [];
  const activeCount = stationItems.filter((s) => s.status === 'active').length;
  const loading = stations.isPending || tanks.isPending || products.isPending;

  // Surface a setup CTA until the core entities exist. We only flag once every
  // count query has resolved, so we never nag during the initial load.
  const setupResolved =
    !companies.isPending && !stations.isPending && !tanks.isPending && !products.isPending;
  const setupIncomplete =
    setupResolved &&
    ((companies.data?.count ?? companies.data?.items.length ?? 0) === 0 ||
      stationItems.length === 0 ||
      (tanks.data?.count ?? tanks.data?.items.length ?? 0) === 0 ||
      (products.data?.count ?? products.data?.items.length ?? 0) === 0);

  const stats = [
    {
      label: 'Stations',
      value: stations.data?.count ?? stationItems.length,
      hint: `${activeCount} active`,
      icon: <Building2 />,
    },
    {
      label: 'Tanks',
      value: tanks.data?.count ?? tanks.data?.items.length ?? 0,
      hint: 'across the network',
      icon: <Database />,
    },
    {
      label: 'Products',
      value: products.data?.count ?? products.data?.items.length ?? 0,
      hint: 'fuel grades',
      icon: <Package />,
    },
    {
      label: 'Live inventory',
      value: tanks.data?.count ?? tanks.data?.items.length ?? 0,
      hint: 'tanks monitored',
      icon: <Droplet />,
    },
  ];

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Network overview"
        title="Command Center"
        description="A live read on your fuel network — stations, inventory, and the operational signals that need attention."
      />

      {setupIncomplete ? (
        <Link
          href="/setup"
          className="group flex items-center gap-4 rounded-xl border border-accent/40 bg-accent-muted/40 px-5 py-4 transition-colors hover:bg-accent-muted/60"
        >
          <span className="flex size-10 shrink-0 items-center justify-center rounded-full bg-accent/15 text-accent">
            <Rocket className="size-5" />
          </span>
          <div className="flex min-w-0 flex-1 flex-col">
            <span className="font-medium text-foreground">Finish setting up your tenant</span>
            <span className="text-sm text-muted-foreground">
              Some core setup is still missing. Walk through the guided checklist to get
              operational.
            </span>
          </div>
          <span className="inline-flex shrink-0 items-center gap-1 text-sm font-medium text-accent">
            Open setup
            <ArrowRight className="size-4 transition-transform group-hover:translate-x-0.5" />
          </span>
        </Link>
      ) : null}

      {stations.isError ? (
        <ErrorState
          title="Couldn't load the network"
          description={String((stations.error as Error).message)}
          onRetry={() => stations.refetch()}
        />
      ) : (
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {loading
            ? Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-[120px] rounded-xl" />
              ))
            : stats.map((s) => (
                <Stat key={s.label} label={s.label} value={s.value} hint={s.hint} icon={s.icon} />
              ))}
        </section>
      )}

      <section className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        {/* Stations list */}
        <Card className="lg:col-span-2">
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <div className="flex flex-col gap-1">
              <CardTitle>Stations</CardTitle>
              <p className="text-sm text-muted-foreground">Your sites and their current status.</p>
            </div>
            <Link
              href="/stations"
              className="inline-flex items-center gap-1 text-sm font-medium text-accent hover:underline"
            >
              View all
              <ChevronRight className="size-4" />
            </Link>
          </CardHeader>
          <CardContent className="flex flex-col gap-1">
            {loading ? (
              <div className="flex flex-col gap-2">
                {Array.from({ length: 2 }).map((_, i) => (
                  <Skeleton key={i} className="h-14 rounded-lg" />
                ))}
              </div>
            ) : stationItems.length === 0 ? (
              <p className="py-6 text-center text-sm text-muted-foreground">
                No stations yet. Create one under Settings.
              </p>
            ) : (
              stationItems.map((s) => (
                <Link
                  key={s.id}
                  href={`/stations/${s.id}`}
                  className="group -mx-2 flex items-center gap-3 rounded-lg px-2 py-2.5 transition-colors hover:bg-muted"
                >
                  <span className="flex size-9 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">
                    <Building2 className="size-4" />
                  </span>
                  <div className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate text-sm font-medium text-foreground">{s.name}</span>
                    <span className="font-mono text-xs text-muted-foreground">
                      {s.code}
                      {s.city ? ` · ${s.city}` : ''}
                    </span>
                  </div>
                  <Badge tone={s.status === 'active' ? 'success' : 'warning'}>{s.status}</Badge>
                  <ChevronRight className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
                </Link>
              ))
            )}
          </CardContent>
        </Card>

        {/* Session / context */}
        <Card>
          <CardHeader>
            <CardTitle>Session</CardTitle>
            <p className="text-sm text-muted-foreground">Your authenticated context.</p>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            {meQuery.isPending ? (
              <Skeleton className="h-24 rounded-lg" />
            ) : meQuery.isError ? (
              <p className="text-sm text-danger">Couldn&apos;t load your session.</p>
            ) : (
              <dl className="flex flex-col gap-3 text-sm">
                <div className="flex flex-col gap-0.5">
                  <dt className="text-xs font-medium text-muted-foreground">User</dt>
                  <dd className="truncate font-mono text-xs text-foreground">{me?.user_id}</dd>
                </div>
                <div className="flex flex-col gap-0.5">
                  <dt className="text-xs font-medium text-muted-foreground">Tenant</dt>
                  <dd className="truncate font-mono text-xs text-foreground">{me?.tenant_id}</dd>
                </div>
                <div className="flex items-center justify-between">
                  <dt className="text-xs font-medium text-muted-foreground">MFA</dt>
                  <dd>
                    <Badge tone={me?.mfa_satisfied ? 'success' : 'neutral'}>
                      {me?.mfa_satisfied ? 'Satisfied' : 'Not required'}
                    </Badge>
                  </dd>
                </div>
              </dl>
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
