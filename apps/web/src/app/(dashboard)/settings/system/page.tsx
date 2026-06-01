'use client';

import { useQuery } from '@tanstack/react-query';
import { Activity, Bell, ServerCog } from 'lucide-react';

import type { JobRun, JobRunStatus, Notification, NotificationSeverity } from '@fuelgrid/sdk';
import {
  Badge,
  Card,
  CardContent,
  CardDescription,
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

import { api } from '@/lib/api';
import { useAuthStore } from '@/stores/auth-store';

// Badge tone per terminal job status. 'running' is in-progress (info);
// 'skipped' means another replica won the advisory lock this tick (neutral).
const jobStatusTone: Record<JobRunStatus, 'info' | 'success' | 'warning' | 'danger' | 'neutral'> = {
  running: 'info',
  success: 'success',
  failure: 'danger',
  skipped: 'neutral',
};

// Badge tone per notification severity (mirrors the topbar bell).
const severityTone: Record<NotificationSeverity, 'info' | 'success' | 'warning' | 'danger'> = {
  info: 'info',
  success: 'success',
  warning: 'warning',
  critical: 'danger',
};

function formatDuration(ms?: number): string {
  if (ms == null) return '—';
  if (ms < 1000) return `${ms} ms`;
  const sec = ms / 1000;
  if (sec < 60) return `${sec.toFixed(sec < 10 ? 1 : 0)} s`;
  const min = Math.floor(sec / 60);
  const rem = Math.round(sec % 60);
  return `${min}m ${rem}s`;
}

function relativeTime(iso?: string): string {
  if (!iso) return '—';
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return '—';
  const diffSec = Math.round((Date.now() - then) / 1000);
  if (diffSec < 60) return 'just now';
  const diffMin = Math.round(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.round(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h ago`;
  const diffDay = Math.round(diffHr / 24);
  if (diffDay < 7) return `${diffDay}d ago`;
  return new Date(iso).toLocaleDateString();
}

export default function SystemPage() {
  const authed = useAuthStore((s) => s.authed);

  // Scheduler job health — latest run of every background job. Refreshed on a
  // slow interval since job cadence is minutes/hours, not seconds.
  const jobs = useQuery({
    queryKey: ['system', 'jobs'],
    queryFn: ({ signal }) => api.listJobRuns(signal),
    enabled: authed,
    refetchInterval: 60_000,
  });

  // Notification delivery status — the recent feed plus the unread count, to
  // confirm the event->notification subscriber is producing entries.
  const unread = useQuery({
    queryKey: ['system', 'notifications', 'unread'],
    queryFn: ({ signal }) => api.notificationUnreadCount(signal),
    enabled: authed,
    refetchInterval: 60_000,
  });
  const feed = useQuery({
    queryKey: ['system', 'notifications', 'feed'],
    queryFn: ({ signal }) => api.listNotifications({ limit: 8 }, signal),
    enabled: authed,
    refetchInterval: 60_000,
  });

  const runs: JobRun[] = jobs.data?.items ?? [];
  const failing = runs.filter((r) => r.status === 'failure').length;
  const succeeding = runs.filter((r) => r.status === 'success').length;
  const recent: Notification[] = feed.data?.items ?? [];

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="System"
        title="Operations health"
        description="Background scheduler job health and notification delivery status across the platform."
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Stat
          label="Jobs tracked"
          value={jobs.isPending ? '—' : runs.length}
          icon={<ServerCog />}
          hint="Scheduler jobs with a recorded run"
        />
        <Stat
          label="Last run succeeded"
          value={jobs.isPending ? '—' : succeeding}
          icon={<Activity />}
          hint="Jobs whose most recent run was success"
        />
        <Stat
          label="Last run failed"
          value={jobs.isPending ? '—' : failing}
          icon={<Activity />}
          hint="Jobs needing attention"
        />
        <Stat
          label="Unread notifications"
          value={unread.isPending ? '—' : (unread.data?.unread_count ?? 0)}
          icon={<Bell />}
          hint="Visible to you (own + tenant-wide)"
        />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Scheduler job health</CardTitle>
          <CardDescription>
            The latest run of every background job — name, last run, status, and duration. Sourced
            from the job_runs ledger; one row per job.
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {jobs.isPending ? (
            <div className="flex flex-col gap-2 p-4">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-12 rounded-lg" />
              ))}
            </div>
          ) : jobs.isError ? (
            <div className="p-4">
              <ErrorState
                title="Couldn't load scheduler jobs"
                description={String((jobs.error as Error).message)}
                onRetry={() => jobs.refetch()}
              />
            </div>
          ) : runs.length === 0 ? (
            <div className="p-4">
              <EmptyState
                title="No job runs recorded yet"
                description="Background jobs append a row here each time they run. If the scheduler is enabled, entries appear after the first tick."
                icon={<ServerCog />}
              />
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Job</TableHead>
                  <TableHead>Last run</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Duration</TableHead>
                  <TableHead>Detail</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {runs.map((r) => (
                  <TableRow key={r.id}>
                    <TableCell className="font-medium text-foreground">{r.job_name}</TableCell>
                    <TableCell className="whitespace-nowrap font-mono text-xs text-muted-foreground">
                      {relativeTime(r.started_at)}
                    </TableCell>
                    <TableCell>
                      <Badge tone={jobStatusTone[r.status]}>{r.status}</Badge>
                    </TableCell>
                    <TableCell className="whitespace-nowrap font-mono tabular-nums text-xs text-muted-foreground">
                      {formatDuration(r.duration_ms)}
                    </TableCell>
                    <TableCell className="max-w-md truncate text-xs text-muted-foreground">
                      {r.detail ?? '—'}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Notification delivery</CardTitle>
          <CardDescription>
            The most recent in-app notifications raised by the event subscriber. Confirms domain
            events are mapping into the feed.
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {feed.isPending ? (
            <div className="flex flex-col gap-2 p-4">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-12 rounded-lg" />
              ))}
            </div>
          ) : feed.isError ? (
            <div className="p-4">
              <ErrorState
                title="Couldn't load notifications"
                description={String((feed.error as Error).message)}
                onRetry={() => feed.refetch()}
              />
            </div>
          ) : recent.length === 0 ? (
            <div className="p-4">
              <EmptyState
                title="No notifications yet"
                description="When domain events fire (revenue recognized, shift closed, risk alerts, incidents, approvals), entries appear here."
                icon={<Bell />}
              />
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>When</TableHead>
                  <TableHead>Title</TableHead>
                  <TableHead>Severity</TableHead>
                  <TableHead>Read</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {recent.map((n) => (
                  <TableRow key={n.id}>
                    <TableCell className="whitespace-nowrap font-mono text-xs text-muted-foreground">
                      {relativeTime(n.created_at)}
                    </TableCell>
                    <TableCell className="text-foreground">{n.title}</TableCell>
                    <TableCell>
                      <Badge tone={severityTone[n.severity]}>{n.severity}</Badge>
                    </TableCell>
                    <TableCell>
                      <Badge tone={n.read_at ? 'neutral' : 'accent'}>
                        {n.read_at ? 'read' : 'unread'}
                      </Badge>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
