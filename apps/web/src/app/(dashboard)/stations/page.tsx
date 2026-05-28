'use client';

import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { ChevronRight } from 'lucide-react';

import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  LoadingState,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

export default function StationsPage() {
  const list = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });

  return (
    <div className="flex flex-col gap-5">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Stations</h1>
        <p className="text-sm text-muted-foreground">
          Open a station to see its tanks, pumps, and open incidents.
        </p>
      </header>

      {list.isPending ? (
        <LoadingState />
      ) : list.isError ? (
        <ErrorState
          title="Couldn't load stations"
          description={String((list.error as Error).message)}
          onRetry={() => list.refetch()}
        />
      ) : (list.data?.items?.length ?? 0) === 0 ? (
        <EmptyState
          title="No stations yet"
          description="Create a station under Settings before opening its dashboard."
        />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {list.data!.items.map((s) => (
            <Link key={s.id} href={`/stations/${s.id}`} className="group">
              <Card className="transition-colors group-hover:border-accent">
                <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
                  <CardTitle className="text-base">{s.name}</CardTitle>
                  <ChevronRight className="size-4 text-muted-foreground" />
                </CardHeader>
                <CardContent className="flex items-center justify-between gap-2">
                  <span className="font-mono text-xs text-muted-foreground">{s.code}</span>
                  <Badge tone={s.status === 'active' ? 'success' : 'warning'}>{s.status}</Badge>
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
