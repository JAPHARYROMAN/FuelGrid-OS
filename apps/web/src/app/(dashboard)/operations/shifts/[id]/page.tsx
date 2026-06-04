'use client';

import { useMemo } from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import {
  ArrowLeft,
  CheckCircle2,
  CircleDot,
  ClipboardCheck,
  Gauge,
  Lock,
  PlayCircle,
  XCircle,
} from 'lucide-react';

import { SdkError, type AuditLogEntry, type MeterReading, type Shift } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  PageHeader,
  Skeleton,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { usePermission } from '@/hooks/use-permissions';

// ---------------------------------------------------------------------------
// Feature 3.4 — shift timeline.
//
// There is no dedicated GET /shifts/{id}/timeline endpoint. Rather than add a
// thin backend route, the timeline is COMPOSED from real domain data already
// readable with station.read:
//   - the shift's own lifecycle fields (opened_at/by, closed_at/by,
//     approved_at/by + status) → opened / submitted / approved / rejected,
//   - the shift's meter readings (GET /shifts/{id}/meter-readings) → readings,
// and ENRICHED, when the actor holds audit.read, from the append-only audit
// trail filtered to this shift (GET /audit-logs?entity_type=shift&entity_id=…)
// so events like nozzle assignment / cash submission / revenue recognition /
// lock also surface. The page degrades gracefully without audit.read.
// ---------------------------------------------------------------------------

type TimelineKind =
  | 'opened'
  | 'reading'
  | 'submitted'
  | 'approved'
  | 'rejected'
  | 'locked'
  | 'audit';

interface TimelineEvent {
  id: string;
  kind: TimelineKind;
  at: string;
  title: string;
  detail?: string;
}

const KIND_ICON: Record<TimelineKind, typeof CircleDot> = {
  opened: PlayCircle,
  reading: Gauge,
  submitted: ClipboardCheck,
  approved: CheckCircle2,
  rejected: XCircle,
  locked: Lock,
  audit: CircleDot,
};

const KIND_TONE: Record<TimelineKind, 'success' | 'neutral' | 'warning' | 'danger' | 'accent'> = {
  opened: 'success',
  reading: 'accent',
  submitted: 'warning',
  approved: 'success',
  rejected: 'danger',
  locked: 'neutral',
  audit: 'neutral',
};

// Static icon colour per kind — Tailwind can't see dynamically-built class
// names, so the full utility strings must appear literally in the source.
const KIND_ICON_COLOR: Record<TimelineKind, string> = {
  opened: 'text-success',
  reading: 'text-accent',
  submitted: 'text-warning',
  approved: 'text-success',
  rejected: 'text-danger',
  locked: 'text-muted-foreground',
  audit: 'text-muted-foreground',
};

/** A human label for an audit action, falling back to the raw code. */
function auditTitle(action: string): string {
  const map: Record<string, string> = {
    'shift.opened': 'Shift opened',
    'shift.closed': 'Shift closed (cash submitted)',
    'shift.approved': 'Shift approved',
    'shift.rejected': 'Shift rejected',
    'shift.attendant_assigned': 'Attendant assigned',
    'shift.attendant_unassigned': 'Attendant unassigned',
    'shift.nozzle_assigned': 'Nozzle assigned',
    'shift.nozzle_unassigned': 'Nozzle unassigned',
    'revenue.recognized': 'Revenue recognized',
    'cash.submitted': 'Cash submitted',
  };
  return map[action] ?? action;
}

/** Compose the timeline from the shift's lifecycle fields + meter readings. */
function deriveEvents(shift: Shift, readings: MeterReading[]): TimelineEvent[] {
  const events: TimelineEvent[] = [];

  events.push({
    id: `opened-${shift.id}`,
    kind: 'opened',
    at: shift.opened_at,
    title: 'Shift opened',
    detail: shift.slot ? `${shift.slot} slot` : undefined,
  });

  // Reading captures (active, non-superseded). Group opening/closing per nozzle
  // is left to the audit enrichment; here each captured reading is one event.
  for (const reading of readings) {
    events.push({
      id: `reading-${reading.id}`,
      kind: 'reading',
      at: reading.recorded_at,
      title: `${reading.reading_type === 'opening' ? 'Opening' : 'Closing'} meter reading`,
      detail: `${reading.reading} on nozzle ${reading.nozzle_id.slice(0, 8)}`,
    });
  }

  if (shift.closed_at) {
    events.push({
      id: `closed-${shift.id}`,
      kind: 'submitted',
      at: shift.closed_at,
      title: 'Shift closed & submitted',
      detail: 'Readings closed; cash submitted for review.',
    });
  }

  if (shift.approved_at) {
    // The shift status distinguishes an approval from a rejection (both stamp
    // approved_at/by via the status transition handler).
    const rejected = shift.status === 'rejected';
    events.push({
      id: `decided-${shift.id}`,
      kind: rejected ? 'rejected' : 'approved',
      at: shift.approved_at,
      title: rejected ? 'Shift rejected' : 'Shift approved',
      detail: rejected ? 'Returned for correction.' : 'Revenue recognized from metered litres.',
    });
  }

  return events;
}

export default function ShiftTimelinePage() {
  const params = useParams<{ id: string }>();
  const shiftID = params.id;

  const shift = useQuery({
    queryKey: ['shift', shiftID],
    queryFn: ({ signal }) => api.getShift(shiftID, signal),
  });

  const readings = useQuery({
    queryKey: ['shift-meter-readings', shiftID],
    queryFn: ({ signal }) => api.listMeterReadings(shiftID, signal),
    enabled: !!shift.data,
  });

  // Enrich from the audit trail only when the actor can read it; the derived
  // timeline stands on its own otherwise.
  const canAudit = usePermission('audit.read');
  const audit = useQuery({
    queryKey: ['shift-audit', shiftID],
    queryFn: ({ signal }) =>
      api.listAuditLogs({ entityType: 'shift', entityID: shiftID, limit: 100 }, signal),
    enabled: !!shift.data && canAudit === true,
  });

  const events = useMemo<TimelineEvent[]>(() => {
    if (!shift.data) return [];
    const derived = deriveEvents(shift.data, readings.data?.items ?? []);

    // Audit enrichment: add events for actions the derived set doesn't already
    // cover (assignments, cash, recognition, lock). Lifecycle actions
    // (opened/closed/approved/rejected) are already derived, so skip those to
    // avoid duplicates.
    const derivedActions = new Set([
      'shift.opened',
      'shift.closed',
      'shift.approved',
      'shift.rejected',
    ]);
    const auditEvents: TimelineEvent[] = (audit.data?.items ?? [])
      .filter((e: AuditLogEntry) => !derivedActions.has(e.action))
      .map((e: AuditLogEntry) => ({
        id: `audit-${e.id}`,
        kind: e.action === 'revenue.recognized' ? ('approved' as const) : ('audit' as const),
        at: e.occurred_at,
        title: auditTitle(e.action),
        detail: e.reason ?? undefined,
      }));

    return [...derived, ...auditEvents].sort(
      (a, b) => new Date(a.at).getTime() - new Date(b.at).getTime(),
    );
  }, [shift.data, readings.data, audit.data]);

  const notFound = shift.error instanceof SdkError && shift.error.status === 404;
  const forbidden = shift.error instanceof SdkError && shift.error.status === 403;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Operations"
        title={shift.data ? `${shift.data.name} · timeline` : 'Shift timeline'}
        description="The shift's lifecycle composed from domain events: opened, readings, submitted, approved or rejected."
        actions={
          <Button asChild variant="ghost" size="sm">
            <Link href="/operations">
              <ArrowLeft className="size-4" />
              Operations
            </Link>
          </Button>
        }
      />

      {shift.isPending ? (
        <div className="flex flex-col gap-3">
          <Skeleton className="h-24 rounded-xl" />
          <Skeleton className="h-64 rounded-xl" />
        </div>
      ) : notFound ? (
        <ErrorState
          title="Shift not found"
          description="This shift doesn't exist or has been removed."
        />
      ) : forbidden ? (
        <ErrorState
          title="No access to this shift"
          description="You don't have permission to view this shift."
        />
      ) : shift.isError ? (
        <ErrorState
          title="Couldn't load the shift"
          description={String((shift.error as Error).message)}
          onRetry={() => shift.refetch()}
        />
      ) : (
        <>
          <Card>
            <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
              <div className="flex flex-col gap-0.5">
                <CardTitle className="text-base">{shift.data.name}</CardTitle>
                <CardDescription>
                  Opened {new Date(shift.data.opened_at).toLocaleString()}
                </CardDescription>
              </div>
              <Badge tone={shift.data.status === 'open' ? 'success' : 'neutral'}>
                {shift.data.status}
              </Badge>
            </CardHeader>
          </Card>

          <Card>
            <CardHeader className="gap-1">
              <CardTitle className="text-base">Timeline</CardTitle>
              <CardDescription>
                {canAudit === true
                  ? 'Domain events enriched with the shift audit trail.'
                  : 'Composed from the shift lifecycle and captured readings.'}
              </CardDescription>
            </CardHeader>
            <CardContent>
              {readings.isPending || (canAudit === true && audit.isPending) ? (
                <div className="flex flex-col gap-3">
                  {Array.from({ length: 4 }).map((_, i) => (
                    <Skeleton key={i} className="h-12 rounded-lg" />
                  ))}
                </div>
              ) : events.length === 0 ? (
                <EmptyState
                  title="No events yet"
                  description="This shift has no recorded activity beyond being opened."
                  icon={<CircleDot />}
                />
              ) : (
                <ol className="flex flex-col" data-testid="shift-timeline">
                  {events.map((e, i) => {
                    const Icon = KIND_ICON[e.kind];
                    const last = i === events.length - 1;
                    return (
                      <li key={e.id} className="flex gap-3" data-testid="timeline-event">
                        <div className="flex flex-col items-center">
                          <span
                            className={`flex size-8 items-center justify-center rounded-full border border-border bg-card ${KIND_ICON_COLOR[e.kind]}`}
                          >
                            <Icon className="size-4" />
                          </span>
                          {!last ? <span className="w-px flex-1 bg-border" /> : null}
                        </div>
                        <div className={`flex flex-col gap-0.5 ${last ? 'pb-0' : 'pb-5'}`}>
                          <div className="flex items-center gap-2">
                            <span className="text-sm font-medium">{e.title}</span>
                            <Badge tone={KIND_TONE[e.kind]}>{e.kind}</Badge>
                          </div>
                          <span className="font-mono text-xs text-muted-foreground">
                            {new Date(e.at).toLocaleString()}
                          </span>
                          {e.detail ? (
                            <span className="text-sm text-muted-foreground">{e.detail}</span>
                          ) : null}
                        </div>
                      </li>
                    );
                  })}
                </ol>
              )}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
