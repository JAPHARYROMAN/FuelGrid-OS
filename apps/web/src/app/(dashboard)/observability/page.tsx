'use client';

import * as React from 'react';
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { Activity, CheckCircle2, Clock, Database, Inbox, Server, XCircle } from 'lucide-react';

import { SdkError, type JobRun, type JobRunStatus, type ObservabilityHealth } from '@fuelgrid/sdk';
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

function depTone(status: string | undefined): 'success' | 'danger' | 'neutral' {
  if (status === 'ok') return 'success';
  if (status === 'unconfigured' || status == null) return 'neutral';
  return 'danger';
}

export default function ObservabilityPage() {
  const canView = usePermission('audit.read');

  const jobs = useQuery({
    queryKey: ['observability', 'job-runs'],
    queryFn: ({ signal }) => api.listJobRuns(signal),
    enabled: canView !== false,
    refetchInterval: REFRESH_MS,
  });

  // Broader health snapshot (feature 13.3): postgres/redis reachability, outbox
  // backlog + dead-letter, scheduler last run — the BFF-reachable equivalent of
  // /readyz + /metrics, which live outside /api/v1.
  const health = useQuery({
    queryKey: ['observability', 'health'],
    queryFn: ({ signal }) => api.getObservabilityHealth(signal),
    enabled: canView !== false,
    refetchInterval: REFRESH_MS,
  });

  const items = jobs.data?.items ?? [];
  const forbidden =
    canView === false ||
    (jobs.error instanceof SdkError && jobs.error.status === 403) ||
    (health.error instanceof SdkError && health.error.status === 403);

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
      ) : (
        <>
          <HealthSection health={health} />
          {renderJobs()}
        </>
      )}
    </div>
  );

  function renderJobs() {
    return jobs.isPending ? (
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
    );
  }
}

function HealthSection({ health }: { health: UseQueryResult<ObservabilityHealth, Error> }) {
  if (health.isPending) {
    return (
      <section className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-[120px] rounded-xl" />
        ))}
      </section>
    );
  }
  if (health.isError) {
    return (
      <ErrorState
        title="Couldn't load health snapshot"
        description={String(health.error.message)}
        onRetry={() => health.refetch()}
      />
    );
  }

  const h = health.data;
  const last = h.scheduler_last_run;

  return (
    <>
      <section className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Stat
          label="System health"
          value={h.healthy ? 'Healthy' : 'Degraded'}
          hint={h.healthy ? 'all checks green' : 'attention needed'}
          icon={<Server />}
        />
        <Stat
          label="Outbox backlog"
          value={h.outbox.backlog}
          hint="events awaiting dispatch"
          icon={<Inbox />}
        />
        <Stat
          label="Dead-letter"
          value={h.outbox.dead_letter}
          hint={h.outbox.dead_letter === 0 ? 'none parked' : 'parked events'}
          icon={<Inbox />}
        />
      </section>

      <Card>
        <CardHeader>
          <CardTitle>Dependencies & scheduler</CardTitle>
          <p className="text-sm text-muted-foreground">
            Backing-service reachability and the most recent background run — the in-app equivalent
            of the platform readiness probe.
          </p>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-1 gap-x-6 gap-y-3 text-sm sm:grid-cols-2">
            {Object.entries(h.checks).map(([dep, status]) => (
              <div key={dep} className="flex items-center justify-between gap-2">
                <dt className="flex items-center gap-2 text-muted-foreground">
                  <Database className="size-4" /> {dep}
                </dt>
                <dd>
                  <Badge tone={depTone(status)}>{status}</Badge>
                </dd>
              </div>
            ))}
            <div className="flex items-center justify-between gap-2">
              <dt className="flex items-center gap-2 text-muted-foreground">
                <Clock className="size-4" /> scheduler last run
              </dt>
              <dd className="text-right">
                {last ? (
                  <span className="flex items-center justify-end gap-2">
                    <span className="font-mono text-xs text-muted-foreground">{last.job_name}</span>
                    <Badge tone={statusTone(last.status)}>{last.status}</Badge>
                  </span>
                ) : (
                  <span className="text-muted-foreground">—</span>
                )}
              </dd>
            </div>
          </dl>
        </CardContent>
      </Card>
    </>
  );
}
