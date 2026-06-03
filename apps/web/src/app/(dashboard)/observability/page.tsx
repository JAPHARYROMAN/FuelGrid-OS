'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { Activity, CheckCircle2, Clock, XCircle } from 'lucide-react';

import { SdkError, type JobRun, type JobRunStatus } from '@fuelgrid/sdk';
import {
  Badge,
  Card,
  CardContent,
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

import { usePermission } from '@/hooks/use-permissions';
import { api } from '@/lib/api';

// The job-health endpoint reports failure for an indefinite period; refresh on
// a short interval so the operational dashboard stays live without a reload.
const REFRESH_MS = 30_000;

function statusTone(status: JobRunStatus): 'success' | 'danger' | 'info' | 'neutral' {
  switch (status) {
    case 'success':
      return 'success';
    case 'failure':
      return 'danger';
    case 'running':
      return 'info';
    default:
      return 'neutral';
  }
}

function formatDuration(ms?: number): string {
  if (ms == null) return '—';
  if (ms < 1000) return `${ms} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

export default function ObservabilityPage() {
  const canView = usePermission('audit.read');

  const jobs = useQuery({
    queryKey: ['observability', 'job-runs'],
    queryFn: ({ signal }) => api.listJobRuns(signal),
    enabled: canView !== false,
    refetchInterval: REFRESH_MS,
  });

  const items = jobs.data?.items ?? [];
  const forbidden =
    canView === false || (jobs.error instanceof SdkError && jobs.error.status === 403);

  const failing = items.filter((j) => j.status === 'failure').length;
  const running = items.filter((j) => j.status === 'running').length;
  const healthy = items.length > 0 && failing === 0;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Monitoring"
        title="Observability"
        description="Operational health of the background scheduler — the latest run of every job, its terminal status and duration. Read-only, refreshed automatically."
      />

      {forbidden ? (
        <ErrorState
          title="No access"
          description="You don't have permission to view system observability (audit.read)."
        />
      ) : jobs.isPending ? (
        <>
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-[120px] rounded-xl" />
            ))}
          </section>
          <div className="flex flex-col gap-2">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-12 rounded-lg" />
            ))}
          </div>
        </>
      ) : jobs.isError ? (
        <ErrorState
          title="Couldn't load system health"
          description={String((jobs.error as Error).message)}
          onRetry={() => jobs.refetch()}
        />
      ) : items.length === 0 ? (
        <EmptyState
          title="No job runs recorded yet"
          description="Background scheduler jobs report their status here once they have run at least once."
          icon={<Activity />}
        />
      ) : (
        <>
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <Stat
              label="Scheduler health"
              value={healthy ? 'Healthy' : 'Degraded'}
              hint={healthy ? 'all jobs green' : `${failing} failing`}
              icon={healthy ? <CheckCircle2 /> : <XCircle />}
            />
            <Stat
              label="Jobs tracked"
              value={items.length}
              hint="latest run per job"
              icon={<Activity />}
            />
            <Stat label="Running now" value={running} hint="in progress" icon={<Clock />} />
          </section>

          <Card>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Job</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Last run</TableHead>
                    <TableHead>Duration</TableHead>
                    <TableHead>Detail</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((j: JobRun) => (
                    <TableRow key={j.id}>
                      <TableCell className="font-medium text-foreground">{j.job_name}</TableCell>
                      <TableCell>
                        <Badge tone={statusTone(j.status)}>{j.status}</Badge>
                      </TableCell>
                      <TableCell className="whitespace-nowrap text-muted-foreground">
                        {new Date(j.started_at).toLocaleString()}
                      </TableCell>
                      <TableCell className="whitespace-nowrap font-mono text-xs text-muted-foreground">
                        {formatDuration(j.duration_ms)}
                      </TableCell>
                      <TableCell className="text-muted-foreground">{j.detail ?? '—'}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
