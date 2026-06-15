'use client';

import * as React from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { ArrowRight, CalendarClock, Clock, Download, Lock } from 'lucide-react';

import { SdkError, type ExportJob, type ReportSnapshot, type ScheduledReport } from '@fuelgrid/sdk';
import { Badge, Button, Card, CardContent, CardHeader, CardTitle, Skeleton } from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { usePermission } from '@/hooks/use-permissions';

/**
 * Reports Home bottom rails (blueprint §4.2): Recent reports, Scheduled, Locked,
 * and Exports. Each rail is data-quality / empty-state aware and NEVER fakes a
 * row.
 *
 *   - Exports  — backed by /reports/exports (export_jobs); shows real recent
 *                exports, or an honest empty state when none exist / no access.
 *   - Recent   — no report_runs store yet (Phase 12/14); honest empty state.
 *   - Scheduled— backed by /reports/scheduled (scheduled_reports, Phase 12); lists
 *                the actor's upcoming permitted schedules (the list endpoint is
 *                gated by reports.schedule and tenant-scoped), or an honest empty
 *                state when none exist / no access.
 *   - Locked   — backed by /reports/snapshots/recent (report_snapshots, Phase 14);
 *                lists recent SIGNED-OFF snapshots, permission-filtered server-side,
 *                or an honest empty state when none exist / no access.
 */

function RailShell({
  icon,
  title,
  hint,
  children,
}: {
  icon: React.ReactNode;
  title: string;
  hint?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <Card className="flex flex-col">
      <CardHeader className="flex-row items-center justify-between gap-2 space-y-0 pb-3">
        <CardTitle className="flex items-center gap-2 text-base">
          <span className="text-muted-foreground [&_svg]:size-4">{icon}</span>
          {title}
        </CardTitle>
        {hint}
      </CardHeader>
      <CardContent className="flex flex-1 flex-col gap-2">{children}</CardContent>
    </Card>
  );
}

/** An honest, non-faked empty state shown when a rail has no backing data yet. */
function RailEmpty({ children }: { children: React.ReactNode }) {
  return (
    <p className="rounded-lg border border-dashed border-border/70 px-3 py-4 text-xs text-muted-foreground">
      {children}
    </p>
  );
}

function shortTime(iso: string): string {
  const s = String(iso ?? '');
  return s.length >= 16 ? s.slice(0, 16).replace('T', ' ') : s;
}

function ExportsRail() {
  const canRead = usePermission('reports.export');
  const jobs = useQuery({
    queryKey: ['reports-hub', 'exports'],
    queryFn: ({ signal }) => api.listExportJobs({ limit: 5 }, signal),
    enabled: canRead !== false,
    retry: false,
  });

  const items: ExportJob[] = jobs.data?.items ?? [];
  const forbidden = jobs.error instanceof SdkError && jobs.error.status === 403;

  return (
    <RailShell
      icon={<Download />}
      title="Exports"
      hint={
        <Button variant="ghost" size="sm" asChild>
          <Link href="/reports/exports">
            View all
            <ArrowRight className="size-3.5" />
          </Link>
        </Button>
      }
    >
      {canRead === false || forbidden ? (
        <RailEmpty>You don&apos;t have access to export history.</RailEmpty>
      ) : jobs.isPending ? (
        <>
          <Skeleton className="h-9 rounded-lg" />
          <Skeleton className="h-9 rounded-lg" />
        </>
      ) : items.length === 0 ? (
        <RailEmpty>No exports yet. Generate a report and export it to see it here.</RailEmpty>
      ) : (
        items.map((j) => (
          <div
            key={j.id}
            className="flex items-center justify-between gap-2 rounded-lg border border-border/70 px-3 py-2"
          >
            <div className="flex min-w-0 flex-col">
              <span className="truncate text-sm font-medium text-foreground">{j.report_key}</span>
              <span className="text-xs text-muted-foreground">
                {j.format.toUpperCase()} · {shortTime(j.created_at)}
              </span>
            </div>
            <ExportStatusBadge status={j.status} />
          </div>
        ))
      )}
    </RailShell>
  );
}

function LockedRail() {
  const canRead = usePermission('reports.read');
  const locked = useQuery({
    queryKey: ['reports-hub', 'locked'],
    queryFn: ({ signal }) => api.listRecentLockedSnapshots(signal),
    enabled: canRead !== false,
    retry: false,
  });

  const items: ReportSnapshot[] = locked.data?.items ?? [];
  const forbidden = locked.error instanceof SdkError && locked.error.status === 403;

  return (
    <RailShell icon={<Lock />} title="Locked">
      {canRead === false || forbidden ? (
        <RailEmpty>You don&apos;t have access to locked report snapshots.</RailEmpty>
      ) : locked.isPending ? (
        <>
          <Skeleton className="h-9 rounded-lg" />
          <Skeleton className="h-9 rounded-lg" />
        </>
      ) : items.length === 0 ? (
        <RailEmpty>
          No signed-off snapshots yet. Capture and sign off a report to lock it here.
        </RailEmpty>
      ) : (
        items.map((s) => (
          <div
            key={s.id}
            className="flex items-center justify-between gap-2 rounded-lg border border-border/70 px-3 py-2"
          >
            <div className="flex min-w-0 flex-col">
              <span className="truncate text-sm font-medium text-foreground">{s.report_key}</span>
              <span className="text-xs text-muted-foreground">
                Rev {s.revision} · signed off{' '}
                {s.signed_off_at ? shortTime(s.signed_off_at) : shortTime(s.captured_at)}
              </span>
            </div>
            <Badge tone="info">Locked</Badge>
          </div>
        ))
      )}
    </RailShell>
  );
}

function ScheduledRail() {
  const canManage = usePermission('reports.schedule');
  const schedules = useQuery({
    queryKey: ['reports-hub', 'scheduled'],
    queryFn: ({ signal }) => api.listScheduledReports({ limit: 5 }, signal),
    enabled: canManage !== false,
    retry: false,
  });

  const items: ScheduledReport[] = schedules.data?.items ?? [];
  const forbidden = schedules.error instanceof SdkError && schedules.error.status === 403;

  return (
    <RailShell
      icon={<CalendarClock />}
      title="Scheduled"
      hint={
        <Button variant="ghost" size="sm" asChild>
          <Link href="/reports/scheduled">
            Open
            <ArrowRight className="size-3.5" />
          </Link>
        </Button>
      }
    >
      {canManage === false || forbidden ? (
        <RailEmpty>You don&apos;t have access to scheduled reports.</RailEmpty>
      ) : schedules.isPending ? (
        <>
          <Skeleton className="h-9 rounded-lg" />
          <Skeleton className="h-9 rounded-lg" />
        </>
      ) : items.length === 0 ? (
        <RailEmpty>
          No scheduled reports yet. Create one to have a report delivered automatically.
        </RailEmpty>
      ) : (
        items.map((s) => (
          <div
            key={s.id}
            className="flex items-center justify-between gap-2 rounded-lg border border-border/70 px-3 py-2"
          >
            <div className="flex min-w-0 flex-col">
              <span className="truncate text-sm font-medium text-foreground">{s.name}</span>
              <span className="text-xs text-muted-foreground">
                {s.report_key} · next {shortTime(s.next_run_at)}
              </span>
            </div>
            <Badge tone={s.enabled ? (s.status === 'error' ? 'danger' : 'success') : 'neutral'}>
              {s.enabled ? s.status : 'paused'}
            </Badge>
          </div>
        ))
      )}
    </RailShell>
  );
}

function ExportStatusBadge({ status }: { status: ExportJob['status'] }) {
  const tone =
    status === 'completed'
      ? 'success'
      : status === 'failed'
        ? 'danger'
        : status === 'running'
          ? 'info'
          : 'neutral';
  return <Badge tone={tone}>{status}</Badge>;
}

export function ReportRails() {
  return (
    <section
      aria-label="Report rails"
      className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4"
    >
      <RailShell icon={<Clock />} title="Recent reports">
        <RailEmpty>
          A recent-runs history will appear here once report runs are recorded (Phase 12).
        </RailEmpty>
      </RailShell>

      <ScheduledRail />

      <LockedRail />

      <ExportsRail />
    </section>
  );
}
