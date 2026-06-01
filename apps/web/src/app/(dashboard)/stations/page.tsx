'use client';

import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { Building2, MapPin } from 'lucide-react';

import {
  Badge,
  Card,
  CardContent,
  EmptyState,
  ErrorState,
  PageHeader,
  Skeleton,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

export default function StationsPage() {
  const list = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });

  const items = list.data?.items ?? [];

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Network"
        title="Stations"
        description="Open a station to see its tanks, pumps, and open incidents."
      />

      {list.isPending ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-36 rounded-xl" />
          ))}
        </div>
      ) : list.isError ? (
        <ErrorState
          title="Couldn't load stations"
          description={String((list.error as Error).message)}
          onRetry={() => list.refetch()}
        />
      ) : items.length === 0 ? (
        <EmptyState
          title="No stations yet"
          description="Create a station under Settings before opening its dashboard."
        />
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {items.map((s) => (
            <Link key={s.id} href={`/stations/${s.id}`} className="group">
              <Card className="h-full transition-all group-hover:border-accent/60 group-hover:shadow-elev-md">
                <CardContent className="flex h-full flex-col gap-4 p-5">
                  <div className="flex items-start justify-between gap-3">
                    <span className="flex size-10 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">
                      <Building2 className="size-5" />
                    </span>
                    <Badge tone={s.status === 'active' ? 'success' : 'warning'}>{s.status}</Badge>
                  </div>
                  <div className="flex flex-col gap-1">
                    <h3 className="font-medium tracking-tight text-foreground">{s.name}</h3>
                    <span className="font-mono text-xs text-muted-foreground">{s.code}</span>
                  </div>
                  {s.city || s.country ? (
                    <div className="mt-auto flex items-center gap-1.5 text-xs text-muted-foreground">
                      <MapPin className="size-3.5" />
                      {[s.city, s.country].filter(Boolean).join(', ')}
                    </div>
                  ) : null}
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
