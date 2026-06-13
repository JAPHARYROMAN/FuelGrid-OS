'use client';

import { useMemo, useState } from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  ArrowLeft,
  CheckCircle2,
  CircleDot,
  ClipboardCheck,
  Gauge,
  Lock,
  PlayCircle,
  Plus,
  ShieldCheck,
  Trash2,
  UserCheck,
  UserPlus,
  Wallet,
  XCircle,
} from 'lucide-react';

import {
  SdkError,
  type AuditLogEntry,
  type CashSubmission,
  type CollectionReceipt,
  type Employee,
  type MeterReading,
  type NozzleAssignment,
  type ReadingVerification,
  type Shift,
  type ShiftAttendance,
  type ShiftDetail,
} from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  EmptyState,
  ErrorState,
  Input,
  PageHeader,
  Skeleton,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { apiErrorBody, apiErrorCode, apiErrorMessage } from '@/lib/api-error';
import { usePermission } from '@/hooks/use-permissions';
import { PermissionGate } from '@/components/permission-gate';
import { formatMoney } from '@/lib/money';
import {
  compareMeterDecimals,
  isMeterDecimal,
  meterFractionDigits,
  subtractMeterDecimals,
} from '@/lib/meter-decimal';

// ---------------------------------------------------------------------------
// Shift review (Mobile Attendant Phase 5) — the supervisor side of the
// attendant app's wait-states. One page per shift composing:
//   - attendance: who checked in/out and when (GET /attendance),
//   - the readings verification queue: every ACTIVE closing reading joined to
//     its verification row (GET /reading-verifications) with batch approve
//     (POST /readings/verify) + per-reading correct (POST /verify-correct),
//   - the collection receipt: expected vs attendant-submitted, the received
//     total, and the resulting receipt (POST /cash-submission/confirm,
//     GET /collection-receipt),
//   - approval readiness: the server's 409 gates (readings_unverified /
//     collection_unconfirmed) rendered as a human checklist,
//   - the existing lifecycle timeline (Feature 3.4), unchanged below.
// The backend stays authoritative throughout: permission hooks only hide or
// disable controls, and every server refusal (403 SoD, 409 gates) is surfaced
// as a plain-language message.
// ---------------------------------------------------------------------------

interface NozzleMeta {
  pumpNumber: number;
  nozzleNumber: number;
  productName: string;
  meterDecimalPlaces: number;
}

function nozzleLabel(nozzleID: string, meta?: NozzleMeta): string {
  if (!meta) return nozzleID.slice(0, 8);
  return `Pump ${meta.pumpNumber} · Nozzle ${meta.nozzleNumber}`;
}

// --- Timeline (Feature 3.4, unchanged) --------------------------------------

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
    'shift.attendant_checked_in': 'Attendant checked in',
    'shift.attendant_checked_out': 'Attendant checked out',
    'reading_verification.approved': 'Reading verified (approved)',
    'reading_verification.corrected': 'Reading verified (corrected)',
    'cash.collection_confirmed': 'Collection receipt recorded',
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

// --- Page --------------------------------------------------------------------

export default function ShiftReviewPage() {
  const params = useParams<{ id: string }>();
  const shiftID = params.id;
  const qc = useQueryClient();

  const shift = useQuery({
    queryKey: ['shift', shiftID],
    queryFn: ({ signal }) => api.getShift(shiftID, signal),
  });
  const stationID = shift.data?.station_id ?? '';
  const closed = !!shift.data && shift.data.status !== 'open';

  const readings = useQuery({
    queryKey: ['shift-meter-readings', shiftID],
    queryFn: ({ signal }) => api.listMeterReadings(shiftID, signal),
    enabled: !!shift.data,
  });

  const verifications = useQuery({
    queryKey: ['shift-reading-verifications', shiftID],
    queryFn: ({ signal }) => api.listReadingVerifications(shiftID, signal),
    enabled: !!shift.data,
  });

  const attendance = useQuery({
    queryKey: ['shift-attendance', shiftID],
    queryFn: ({ signal }) => api.listShiftAttendance(shiftID, signal),
    enabled: !!shift.data,
  });

  // Display names for attendants/recorders: the station's employee register
  // links user accounts to full names.
  const employees = useQuery({
    queryKey: ['station-employees', stationID],
    queryFn: ({ signal }) => api.listEmployees(stationID, { limit: 200 }, signal),
    enabled: !!stationID,
  });

  // Pump/nozzle numbers + meter precision for the queue's labels and the
  // correction modal's client-side scale validation (mirrors the server 422).
  const stationOverview = useQuery({
    queryKey: ['station-overview', stationID, 'shift-review'],
    queryFn: ({ signal }) => api.getStationOverview(stationID, signal),
    enabled: !!stationID,
  });

  const products = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
    enabled: !!shift.data,
  });

  const closeSummary = useQuery({
    queryKey: ['shift-close-summary', shiftID],
    queryFn: ({ signal }) => api.getCloseSummary(shiftID, signal),
    enabled: closed,
  });

  // 404 = "no receipt yet" — a normal state of the panel, not an error.
  const receipt = useQuery({
    queryKey: ['shift-collection-receipt', shiftID],
    queryFn: async ({ signal }) => {
      try {
        return await api.getCollectionReceipt(shiftID, signal);
      } catch (e) {
        if (e instanceof SdkError && e.status === 404) return null;
        throw e;
      }
    },
    enabled: closed,
  });

  const exceptions = useQuery({
    queryKey: ['shift-exceptions', shiftID],
    queryFn: ({ signal }) => api.listShiftExceptions(shiftID, signal),
    enabled: closed,
  });

  // Enrich the timeline from the audit trail only when the actor can read it.
  const canAudit = usePermission('audit.read');
  const audit = useQuery({
    queryKey: ['shift-audit', shiftID],
    queryFn: ({ signal }) =>
      api.listAuditLogs({ entityType: 'shift', entityID: shiftID, limit: 100 }, signal),
    enabled: !!shift.data && canAudit === true,
  });

  function refreshReview() {
    qc.invalidateQueries({ queryKey: ['shift', shiftID] });
    qc.invalidateQueries({ queryKey: ['shift-reading-verifications', shiftID] });
    qc.invalidateQueries({ queryKey: ['shift-close-summary', shiftID] });
    qc.invalidateQueries({ queryKey: ['shift-collection-receipt', shiftID] });
    qc.invalidateQueries({ queryKey: ['shift-exceptions', shiftID] });
  }

  const nameByUserID = useMemo(() => {
    const map = new Map<string, string>();
    for (const e of employees.data?.items ?? []) {
      if (e.user_id) map.set(e.user_id, e.full_name);
    }
    return map;
  }, [employees.data]);

  const nozzleMeta = useMemo(() => {
    const productName = new Map((products.data?.items ?? []).map((p) => [p.id, p.name]));
    const map = new Map<string, NozzleMeta>();
    for (const pump of stationOverview.data?.pumps ?? []) {
      for (const nozzle of pump.nozzles) {
        map.set(nozzle.id, {
          pumpNumber: pump.number,
          nozzleNumber: nozzle.number,
          productName: productName.get(nozzle.product_id) ?? 'fuel',
          meterDecimalPlaces: nozzle.meter_decimal_places,
        });
      }
    }
    return map;
  }, [stationOverview.data, products.data]);

  const events = useMemo<TimelineEvent[]>(() => {
    if (!shift.data) return [];
    const derived = deriveEvents(shift.data, readings.data?.items ?? []);
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
        title={shift.data ? `${shift.data.name} · review` : 'Shift review'}
        description="Attendance, closing-reading verification, the collection receipt, and approval readiness for this shift."
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

          <AttendancePanel
            shift={shift.data}
            attendance={attendance.data?.items ?? []}
            pending={attendance.isPending}
            error={attendance.isError ? attendance.error : null}
            nameByUserID={nameByUserID}
          />

          {shift.data.status === 'open' ? (
            <AdHocAssignmentPanel
              shift={shift.data}
              stationID={stationID}
              nozzleMeta={nozzleMeta}
              nozzleMetaPending={stationOverview.isPending}
              employees={employees.data?.items ?? []}
              nameByUserID={nameByUserID}
              onChanged={() => {
                qc.invalidateQueries({ queryKey: ['shift', shiftID] });
                qc.invalidateQueries({ queryKey: ['shift-attendance', shiftID] });
              }}
            />
          ) : null}

          <VerificationQueue
            shift={shift.data}
            stationID={stationID}
            readings={readings.data?.items ?? []}
            readingsPending={readings.isPending}
            verifications={verifications.data?.items ?? []}
            verificationsPending={verifications.isPending}
            nozzleMeta={nozzleMeta}
            nameByUserID={nameByUserID}
            onChanged={refreshReview}
          />

          {closed ? (
            <CollectionReceiptPanel
              shiftID={shiftID}
              stationID={stationID}
              expectedCash={closeSummary.data?.expected_cash}
              cashSubmission={closeSummary.data?.cash_submission ?? null}
              loading={closeSummary.isPending || receipt.isPending}
              receipt={receipt.data ?? null}
              nameByUserID={nameByUserID}
              onChanged={refreshReview}
            />
          ) : null}

          {closed ? (
            <ApprovalReadiness
              shift={shift.data}
              stationID={stationID}
              pendingReadings={pendingClosingReadings(
                readings.data?.items ?? [],
                verifications.data?.items ?? [],
              )}
              verifications={verifications.data?.items ?? []}
              cashSubmission={closeSummary.data?.cash_submission ?? null}
              receipt={receipt.data ?? null}
              openExceptions={
                (exceptions.data?.items ?? []).filter((e) => e.status === 'open').length
              }
              loading={
                verifications.isPending ||
                closeSummary.isPending ||
                receipt.isPending ||
                exceptions.isPending
              }
              onChanged={refreshReview}
            />
          ) : null}

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

// --- Attendance --------------------------------------------------------------

function AttendancePanel({
  shift,
  attendance,
  pending,
  error,
  nameByUserID,
}: {
  shift: ShiftDetail;
  attendance: ShiftAttendance[];
  pending: boolean;
  error: unknown;
  nameByUserID: Map<string, string>;
}) {
  const byAttendant = new Map(attendance.map((a) => [a.attendant_id, a]));
  // The roster drives the rows so a rostered attendant who never checked in is
  // visible at a glance; attendance rows for since-unassigned users still show.
  const rosterIDs = shift.attendants.map((a) => a.user_id);
  const extraIDs = attendance.map((a) => a.attendant_id).filter((id) => !rosterIDs.includes(id));
  const rows = [...rosterIDs, ...extraIDs];

  return (
    <Card data-testid="attendance-panel">
      <CardHeader className="gap-1">
        <CardTitle className="flex items-center gap-2 text-base">
          <UserCheck className="size-4 text-accent" />
          Attendance
        </CardTitle>
        <CardDescription>Who checked in and out of this shift, and when.</CardDescription>
      </CardHeader>
      <CardContent>
        {pending ? (
          <Skeleton className="h-16 rounded-lg" />
        ) : error ? (
          <p className="text-sm text-danger">
            {apiErrorMessage(error, "Couldn't load attendance for this shift.")}
          </p>
        ) : rows.length === 0 ? (
          <p className="text-sm text-muted-foreground">No attendants on this shift.</p>
        ) : (
          <ul className="flex flex-col gap-1.5">
            {rows.map((userID) => {
              const rec = byAttendant.get(userID);
              return (
                <li
                  key={userID}
                  className="flex flex-wrap items-center justify-between gap-2 rounded-md bg-muted/40 px-3 py-2 text-sm"
                  data-testid="attendance-row"
                >
                  <span className="font-medium">
                    {nameByUserID.get(userID) ?? `Attendant ${userID.slice(0, 8)}`}
                  </span>
                  {rec ? (
                    <span className="flex flex-wrap items-center gap-2">
                      <Badge tone={rec.status === 'checked_in' ? 'success' : 'neutral'}>
                        {rec.status === 'checked_in' ? 'Checked in' : 'Checked out'}
                      </Badge>
                      <span className="font-mono text-xs text-muted-foreground">
                        in {new Date(rec.check_in_at).toLocaleTimeString()}
                        {rec.check_out_at
                          ? ` · out ${new Date(rec.check_out_at).toLocaleTimeString()}`
                          : ''}
                      </span>
                    </span>
                  ) : (
                    <Badge tone="warning">Not checked in</Badge>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

// --- Ad-hoc attendant assignment (PRD gap #5) ---------------------------------

/**
 * Assign or unassign an attendant to a nozzle OUTSIDE the rotation — the
 * supervisor's escape hatch for substitutes who aren't on the rostered team
 * (PRD gap #5). The assign/unassign API + SDK already exist; this is the only
 * surface that calls them for ad-hoc cover.
 *
 * A substitute may not be on the shift roster yet, so assigning chains
 * assignAttendant (idempotent on the roster) → assignNozzle. Unassign only
 * removes the nozzle link and is guarded by a confirm dialog. Gated on
 * shift.assign; the backend stays authoritative.
 */
function AdHocAssignmentPanel({
  shift,
  stationID,
  nozzleMeta,
  nozzleMetaPending,
  employees,
  nameByUserID,
  onChanged,
}: {
  shift: ShiftDetail;
  stationID: string;
  nozzleMeta: Map<string, NozzleMeta>;
  nozzleMetaPending: boolean;
  employees: Employee[];
  nameByUserID: Map<string, string>;
  onChanged: () => void;
}) {
  const [nozzleID, setNozzleID] = useState('');
  const [attendantID, setAttendantID] = useState('');
  const [unassigning, setUnassigning] = useState<NozzleAssignment | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const canAssign = usePermission('shift.assign', { stationID });

  const rosterIDs = new Set(shift.attendants.map((a) => a.user_id));
  const assignedNozzleIDs = new Set(shift.nozzle_assignments.map((a) => a.nozzle_id));
  // Every nozzle the station knows about, minus those already assigned.
  const availableNozzles = Array.from(nozzleMeta.entries())
    .filter(([id]) => !assignedNozzleIDs.has(id))
    .map(([id, meta]) => ({ id, label: nozzleLabel(id, meta) }))
    .sort((a, b) => a.label.localeCompare(b.label));
  // Any active station employee with a user account can stand in — including
  // attendants who are NOT on the rostered team (the whole point of gap #5).
  const candidates = employees
    .filter((e) => e.user_id && e.status === 'active')
    .map((e) => ({ userID: e.user_id as string, name: e.full_name }))
    .sort((a, b) => a.name.localeCompare(b.name));

  const assign = useMutation({
    mutationFn: async () => {
      // A substitute outside the rotation isn't on the roster yet — add them
      // first (the roster add is the prerequisite the assign API expects).
      if (!rosterIDs.has(attendantID)) {
        await api.assignAttendant(shift.id, attendantID);
      }
      return api.assignNozzle(shift.id, { nozzle_id: nozzleID, attendant_id: attendantID });
    },
    onSuccess: () => {
      setActionError(null);
      setNozzleID('');
      setAttendantID('');
      onChanged();
    },
    onError: (e) =>
      setActionError(
        e instanceof SdkError && e.status === 403
          ? apiErrorMessage(e, 'You cannot assign attendants on this shift.')
          : apiErrorMessage(e, "Couldn't assign the attendant. Try again."),
      ),
  });

  const unassign = useMutation({
    mutationFn: (assignmentID: string) => api.unassignNozzle(shift.id, assignmentID),
    onSuccess: () => {
      setActionError(null);
      setUnassigning(null);
      onChanged();
    },
    onError: (e) => {
      setUnassigning(null);
      setActionError(
        e instanceof SdkError && e.status === 403
          ? apiErrorMessage(e, 'You cannot unassign attendants on this shift.')
          : apiErrorMessage(e, "Couldn't unassign the attendant. Try again."),
      );
    },
  });

  const submittable = nozzleID !== '' && attendantID !== '';

  return (
    <Card data-testid="adhoc-assignment-panel">
      <CardHeader className="gap-1">
        <CardTitle className="flex items-center gap-2 text-base">
          <UserPlus className="size-4 text-accent" />
          Attendant assignment
        </CardTitle>
        <CardDescription>
          Assign an attendant to a nozzle for this shift — including substitutes who aren&apos;t on
          the rostered team. Unassigning a nozzle removes that attendant from it.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {actionError ? (
          <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
            {actionError}
          </p>
        ) : null}

        {shift.nozzle_assignments.length > 0 ? (
          <ul className="flex flex-col gap-1.5">
            {shift.nozzle_assignments.map((a) => (
              <li
                key={a.id}
                className="flex flex-wrap items-center justify-between gap-2 rounded-md bg-muted/40 px-3 py-2 text-sm"
                data-testid="adhoc-assignment-row"
              >
                <span>
                  <span className="font-medium">
                    {nozzleLabel(a.nozzle_id, nozzleMeta.get(a.nozzle_id))}
                  </span>
                  <span className="text-muted-foreground">
                    {' · '}
                    {nameByUserID.get(a.attendant_id) ?? `Attendant ${a.attendant_id.slice(0, 8)}`}
                  </span>
                </span>
                <PermissionGate permission="shift.assign" stationId={stationID}>
                  <Button
                    aria-label={`Unassign ${nozzleLabel(a.nozzle_id, nozzleMeta.get(a.nozzle_id))}`}
                    size="sm"
                    variant="ghost"
                    disabled={unassign.isPending}
                    onClick={() => {
                      setActionError(null);
                      setUnassigning(a);
                    }}
                  >
                    <Trash2 className="size-4" />
                    Unassign
                  </Button>
                </PermissionGate>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">No nozzles assigned on this shift yet.</p>
        )}

        {canAssign !== false ? (
          <div className="flex flex-col gap-2 rounded-lg border border-border/70 bg-muted/20 p-3">
            <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Assign a nozzle
            </span>
            <div className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
              <select
                aria-label="Nozzle"
                className="h-9 rounded-md border border-border bg-background px-2 text-sm"
                value={nozzleID}
                onChange={(e) => setNozzleID(e.target.value)}
                disabled={availableNozzles.length === 0 || nozzleMetaPending}
              >
                <option value="">
                  {nozzleMetaPending
                    ? 'Loading nozzles…'
                    : availableNozzles.length === 0
                      ? 'All nozzles assigned'
                      : 'Nozzle'}
                </option>
                {availableNozzles.map((n) => (
                  <option key={n.id} value={n.id}>
                    {n.label}
                  </option>
                ))}
              </select>
              <select
                aria-label="Attendant"
                className="h-9 rounded-md border border-border bg-background px-2 text-sm"
                value={attendantID}
                onChange={(e) => setAttendantID(e.target.value)}
                disabled={candidates.length === 0}
              >
                <option value="">{candidates.length === 0 ? 'No attendants' : 'Attendant'}</option>
                {candidates.map((c) => (
                  <option key={c.userID} value={c.userID}>
                    {c.name}
                    {rosterIDs.has(c.userID) ? '' : ' (substitute)'}
                  </option>
                ))}
              </select>
              <PermissionGate permission="shift.assign" stationId={stationID}>
                <Button
                  size="sm"
                  disabled={!submittable || assign.isPending}
                  onClick={() => assign.mutate()}
                >
                  <Plus className="size-4" />
                  {assign.isPending ? 'Assigning…' : 'Assign'}
                </Button>
              </PermissionGate>
            </div>
          </div>
        ) : null}
      </CardContent>

      {/* Confirm-before-unassign */}
      <Dialog
        open={!!unassigning}
        onOpenChange={(open) => (!open ? setUnassigning(null) : undefined)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Unassign this nozzle?</DialogTitle>
            <DialogDescription>
              {unassigning ? (
                <>
                  This removes{' '}
                  <span className="font-medium">
                    {nameByUserID.get(unassigning.attendant_id) ??
                      `attendant ${unassigning.attendant_id.slice(0, 8)}`}
                  </span>{' '}
                  from{' '}
                  <span className="font-medium">
                    {nozzleLabel(unassigning.nozzle_id, nozzleMeta.get(unassigning.nozzle_id))}
                  </span>
                  . You can reassign the nozzle afterwards.
                </>
              ) : null}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setUnassigning(null)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              disabled={unassign.isPending}
              onClick={() => (unassigning ? unassign.mutate(unassigning.id) : undefined)}
            >
              {unassign.isPending ? 'Unassigning…' : 'Unassign'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}

// --- Readings verification queue ----------------------------------------------

/** The shift's ACTIVE closing readings that have no verification row yet. */
function pendingClosingReadings(
  readings: MeterReading[],
  verifications: ReadingVerification[],
): MeterReading[] {
  const verified = new Set(verifications.map((v) => v.reading_id));
  return readings.filter(
    (r) => r.reading_type === 'closing' && r.status === 'active' && !verified.has(r.id),
  );
}

function VerificationQueue({
  shift,
  stationID,
  readings,
  readingsPending,
  verifications,
  verificationsPending,
  nozzleMeta,
  nameByUserID,
  onChanged,
}: {
  shift: ShiftDetail;
  stationID: string;
  readings: MeterReading[];
  readingsPending: boolean;
  verifications: ReadingVerification[];
  verificationsPending: boolean;
  nozzleMeta: Map<string, NozzleMeta>;
  nameByUserID: Map<string, string>;
  onChanged: () => void;
}) {
  const [approveAllOpen, setApproveAllOpen] = useState(false);
  const [correcting, setCorrecting] = useState<MeterReading | null>(null);
  const [rejecting, setRejecting] = useState<MeterReading | null>(null);
  const [flagging, setFlagging] = useState<MeterReading | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const canVerify = usePermission('reading.override', { stationID });

  const verificationByReading = new Map(verifications.map((v) => [v.reading_id, v]));
  const openingByNozzle = new Map(
    readings
      .filter((r) => r.reading_type === 'opening' && r.status === 'active')
      .map((r) => [r.nozzle_id, r.reading]),
  );
  const attendantByNozzle = new Map(
    shift.nozzle_assignments.map((a) => [a.nozzle_id, a.attendant_id]),
  );

  const closing = readings.filter((r) => r.reading_type === 'closing' && r.status === 'active');
  const pending = closing.filter((r) => !verificationByReading.has(r.id));

  const approveAll = useMutation({
    mutationFn: () => api.verifyShiftReadings(shift.id),
    onSuccess: () => {
      setActionError(null);
      setApproveAllOpen(false);
      onChanged();
    },
    onError: (e) => {
      setApproveAllOpen(false);
      setActionError(
        e instanceof SdkError && e.status === 403
          ? apiErrorMessage(e, 'You cannot verify these readings.')
          : apiErrorMessage(e, "Couldn't verify the readings. Try again."),
      );
    },
  });

  // Clear a HELD (rejected/flagged) reading by approving it as-submitted — the
  // per-reading approve path that overwrites the hold with a terminal verdict
  // (the supervisor decided the attendant's figure was fine). Correcting a held
  // reading uses the existing CorrectReadingDialog (also clears the hold).
  const approveSingle = useMutation({
    mutationFn: (readingID: string) => api.approveReading(shift.id, readingID),
    onSuccess: () => {
      setActionError(null);
      onChanged();
    },
    onError: (e) => {
      setActionError(
        e instanceof SdkError && e.status === 403
          ? apiErrorMessage(e, 'You cannot verify a reading you recorded.')
          : apiErrorMessage(e, "Couldn't approve the reading. Try again."),
      );
    },
  });

  return (
    <Card data-testid="verification-queue">
      <CardHeader className="gap-1">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <CardTitle className="flex items-center gap-2 text-base">
            <ShieldCheck className="size-4 text-accent" />
            Closing readings verification
          </CardTitle>
          {pending.length > 0 ? (
            <PermissionGate permission="reading.override" stationId={stationID} mode="hide">
              <Button size="sm" onClick={() => setApproveAllOpen(true)}>
                Approve all ({pending.length})
              </Button>
            </PermissionGate>
          ) : null}
        </div>
        <CardDescription>
          Each submitted closing reading awaits your verification: approve it as submitted, correct
          it with a reason, reject it back to the attendant to re-capture, or flag it for
          investigation. Separation of duties — you cannot verify readings you recorded yourself.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        {actionError ? (
          <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
            {actionError}
          </p>
        ) : null}

        {readingsPending || verificationsPending ? (
          <Skeleton className="h-24 rounded-lg" />
        ) : closing.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No closing readings submitted yet
            {shift.status === 'open' ? ' — the shift is still running.' : '.'}
          </p>
        ) : (
          <ul className="flex flex-col gap-2">
            {closing.map((reading) => {
              const verification = verificationByReading.get(reading.id);
              const meta = nozzleMeta.get(reading.nozzle_id);
              const opening = openingByNozzle.get(reading.nozzle_id);
              const finalReading = verification?.final_approved_reading ?? reading.reading;
              const litres =
                opening != null && compareMeterDecimals(finalReading, opening) >= 0
                  ? subtractMeterDecimals(finalReading, opening)
                  : null;
              const attendantID = attendantByNozzle.get(reading.nozzle_id);
              return (
                <li
                  key={reading.id}
                  className="flex flex-col gap-1.5 rounded-lg border border-border/70 bg-muted/20 p-3"
                  data-testid="verification-row"
                >
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="text-sm font-medium">
                      {nozzleLabel(reading.nozzle_id, meta)}
                      {meta ? (
                        <span className="text-muted-foreground"> · {meta.productName}</span>
                      ) : null}
                    </span>
                    <VerificationBadge verification={verification} />
                  </div>
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
                    <span>
                      Attendant:{' '}
                      {attendantID
                        ? (nameByUserID.get(attendantID) ?? attendantID.slice(0, 8))
                        : 'unassigned'}
                    </span>
                    <span className="font-mono tabular-nums">
                      {opening ?? '—'} → {reading.reading}
                    </span>
                    {litres != null ? (
                      <span className="font-mono tabular-nums">{litres} L</span>
                    ) : null}
                  </div>
                  {verification?.status === 'corrected' ? (
                    <p className="text-sm text-warning">
                      Submitted{' '}
                      <span className="font-mono tabular-nums">
                        {verification.attendant_submitted_reading}
                      </span>{' '}
                      → approved{' '}
                      <span className="font-mono tabular-nums">
                        {verification.final_approved_reading}
                      </span>
                      {verification.reason ? <> — {verification.reason}</> : null}
                    </p>
                  ) : null}
                  {verification?.status === 'rejected' && verification.reason ? (
                    <p className="text-sm text-danger">
                      Rejected — {verification.reason}
                      <span className="block text-muted-foreground">
                        Sent back to the attendant to re-capture this closing reading. Approve or
                        correct it here once it is resolved.
                      </span>
                    </p>
                  ) : null}
                  {verification?.status === 'flagged' && verification.reason ? (
                    <p className="text-sm text-warning">
                      Flagged for investigation — {verification.reason}
                      <span className="block text-muted-foreground">
                        Approve or correct it here to clear the hold once your investigation is
                        done.
                      </span>
                    </p>
                  ) : null}
                  {canVerify &&
                  (!verification ||
                    verification.status === 'rejected' ||
                    verification.status === 'flagged') ? (
                    <div className="flex flex-wrap gap-2">
                      {/* A held reading can be APPROVED as-submitted to clear the
                          hold; a never-verified one is approved via "Approve all". */}
                      {verification ? (
                        <Button
                          size="sm"
                          disabled={approveSingle.isPending}
                          onClick={() => {
                            setActionError(null);
                            approveSingle.mutate(reading.id);
                          }}
                        >
                          Approve as submitted
                        </Button>
                      ) : null}
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => {
                          setActionError(null);
                          setCorrecting(reading);
                        }}
                      >
                        Correct…
                      </Button>
                      {verification?.status !== 'rejected' ? (
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => {
                            setActionError(null);
                            setRejecting(reading);
                          }}
                        >
                          Reject…
                        </Button>
                      ) : null}
                      {verification?.status !== 'flagged' ? (
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => {
                            setActionError(null);
                            setFlagging(reading);
                          }}
                        >
                          Flag for investigation…
                        </Button>
                      ) : null}
                    </div>
                  ) : null}
                </li>
              );
            })}
          </ul>
        )}
      </CardContent>

      {/* Approve-all confirmation */}
      <Dialog open={approveAllOpen} onOpenChange={setApproveAllOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Approve all pending readings?</DialogTitle>
            <DialogDescription>
              This approves {pending.length} closing reading{pending.length === 1 ? '' : 's'}{' '}
              exactly as submitted by the attendants. Use “Correct…” on a reading instead if a
              figure is wrong.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setApproveAllOpen(false)}>
              Cancel
            </Button>
            <Button disabled={approveAll.isPending} onClick={() => approveAll.mutate()}>
              {approveAll.isPending ? 'Approving…' : 'Approve all as submitted'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Per-reading correction */}
      {correcting ? (
        <CorrectReadingDialog
          shiftID={shift.id}
          reading={correcting}
          meta={nozzleMeta.get(correcting.nozzle_id)}
          opening={openingByNozzle.get(correcting.nozzle_id)}
          onClose={() => setCorrecting(null)}
          onCorrected={() => {
            setCorrecting(null);
            onChanged();
          }}
        />
      ) : null}

      {/* Per-reading reject — sends the closing back to the attendant to re-capture. */}
      {rejecting ? (
        <ReadingHoldDialog
          kind="reject"
          shiftID={shift.id}
          reading={rejecting}
          meta={nozzleMeta.get(rejecting.nozzle_id)}
          onClose={() => setRejecting(null)}
          onDone={() => {
            setRejecting(null);
            onChanged();
          }}
        />
      ) : null}

      {/* Per-reading flag — opens a supervisor investigation hold. */}
      {flagging ? (
        <ReadingHoldDialog
          kind="flag"
          shiftID={shift.id}
          reading={flagging}
          meta={nozzleMeta.get(flagging.nozzle_id)}
          onClose={() => setFlagging(null)}
          onDone={() => {
            setFlagging(null);
            onChanged();
          }}
        />
      ) : null}
    </Card>
  );
}

/**
 * Reject or flag a single closing reading. Both are non-terminal HOLDS that
 * block shift approval and require a reason (server 400 if absent). A REJECT
 * sends the closing back to the attendant to re-capture; a FLAG opens a
 * supervisor investigation that the attendant cannot resolve. Separation of
 * duties is enforced server-side — a 403 surfaces verbatim.
 */
function ReadingHoldDialog({
  kind,
  shiftID,
  reading,
  meta,
  onClose,
  onDone,
}: {
  kind: 'reject' | 'flag';
  shiftID: string;
  reading: MeterReading;
  meta?: NozzleMeta;
  onClose: () => void;
  onDone: () => void;
}) {
  const [reason, setReason] = useState('');
  const reasonMissing = reason.trim() === '';

  const hold = useMutation({
    mutationFn: () =>
      kind === 'reject'
        ? api.rejectReading(shiftID, reading.id, { reason: reason.trim() })
        : api.flagReading(shiftID, reading.id, { reason: reason.trim() }),
    onSuccess: onDone,
  });

  const title = kind === 'reject' ? 'Reject closing reading' : 'Flag reading for investigation';
  const description =
    kind === 'reject'
      ? "The attendant's submitted figure is sent back to them to re-capture — the original stays stored as history, and the shift cannot be approved until they resubmit and you re-verify."
      : 'This opens an investigation hold on the reading. The shift cannot be approved while a flag is open; clear it by correcting the reading and re-verifying.';

  return (
    <Dialog open onOpenChange={(open) => (!open ? onClose() : undefined)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {nozzleLabel(reading.nozzle_id, meta)} — the attendant submitted{' '}
            <span className="font-mono tabular-nums">{reading.reading}</span>. {description}
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-muted-foreground">Reason (required)</span>
            <Input
              aria-label={kind === 'reject' ? 'Rejection reason' : 'Flag reason'}
              type="text"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={
                kind === 'reject'
                  ? 'e.g. photo does not match the meter — please re-capture'
                  : 'e.g. figure looks tampered — escalating to the manager'
              }
            />
          </label>
          {hold.isError ? (
            <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
              {hold.error instanceof SdkError && hold.error.status === 403
                ? apiErrorMessage(hold.error, 'You cannot verify a reading you recorded yourself.')
                : apiErrorMessage(
                    hold.error,
                    kind === 'reject'
                      ? "Couldn't reject the reading."
                      : "Couldn't flag the reading.",
                  )}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant={kind === 'reject' ? 'danger' : 'primary'}
            disabled={reasonMissing || hold.isPending}
            onClick={() => hold.mutate()}
          >
            {hold.isPending
              ? kind === 'reject'
                ? 'Rejecting…'
                : 'Flagging…'
              : kind === 'reject'
                ? 'Reject reading'
                : 'Flag reading'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function VerificationBadge({ verification }: { verification?: ReadingVerification }) {
  if (!verification) return <Badge tone="warning">Pending verification</Badge>;
  switch (verification.status) {
    case 'approved':
      return <Badge tone="success">Approved</Badge>;
    case 'corrected':
      return <Badge tone="warning">Corrected</Badge>;
    case 'rejected':
      return <Badge tone="danger">Rejected</Badge>;
    case 'flagged':
      return <Badge tone="warning">Flagged for investigation</Badge>;
    default:
      return <Badge tone="neutral">{verification.status}</Badge>;
  }
}

function CorrectReadingDialog({
  shiftID,
  reading,
  meta,
  opening,
  onClose,
  onCorrected,
}: {
  shiftID: string;
  reading: MeterReading;
  meta?: NozzleMeta;
  opening?: string;
  onClose: () => void;
  onCorrected: () => void;
}) {
  const [value, setValue] = useState('');
  const [reason, setReason] = useState('');

  const places = meta?.meterDecimalPlaces ?? 3;
  const trimmed = value.trim();
  const invalid = trimmed !== '' && !isMeterDecimal(trimmed);
  const overScale = !invalid && trimmed !== '' && meterFractionDigits(trimmed) > places;
  const belowOpening =
    !invalid &&
    !overScale &&
    trimmed !== '' &&
    opening != null &&
    compareMeterDecimals(trimmed, opening) < 0;
  const reasonMissing = reason.trim() === '';
  const submittable = trimmed !== '' && !invalid && !overScale && !belowOpening && !reasonMissing;

  const correct = useMutation({
    mutationFn: () =>
      api.verifyCorrectReading(shiftID, reading.id, {
        verified_reading: trimmed,
        reason: reason.trim(),
      }),
    onSuccess: onCorrected,
  });

  return (
    <Dialog open onOpenChange={(open) => (!open ? onClose() : undefined)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Correct closing reading</DialogTitle>
          <DialogDescription>
            {nozzleLabel(reading.nozzle_id, meta)} — the attendant submitted{' '}
            <span className="font-mono tabular-nums">{reading.reading}</span>. The original stays
            stored; your verified figure becomes the final approved reading.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-muted-foreground">
              Verified reading ({places} decimal{places === 1 ? '' : 's'} max)
            </span>
            <Input
              aria-label="Verified reading"
              className="text-right font-mono tabular-nums"
              type="text"
              inputMode="decimal"
              autoComplete="off"
              value={value}
              onChange={(e) => setValue(e.target.value)}
              aria-invalid={invalid || overScale || belowOpening}
            />
          </label>
          {invalid ? (
            <p className="text-sm text-danger" role="alert">
              Enter numbers only, like 1500 or 1500.25.
            </p>
          ) : overScale ? (
            <p className="text-sm text-danger" role="alert">
              Too many decimals — this meter records at most {places} decimal
              {places === 1 ? '' : 's'}.
            </p>
          ) : belowOpening ? (
            <p className="text-sm text-danger" role="alert">
              The verified reading cannot be below the opening reading ({opening}).
            </p>
          ) : null}
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-muted-foreground">Reason (required)</span>
            <Input
              aria-label="Correction reason"
              type="text"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="e.g. pump display misread by attendant"
            />
          </label>
          {correct.isError ? (
            <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
              {apiErrorMessage(correct.error, "Couldn't record the correction.")}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button disabled={!submittable || correct.isPending} onClick={() => correct.mutate()}>
            {correct.isPending ? 'Saving…' : 'Verify with correction'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// --- Collection receipt --------------------------------------------------------

const RECEIPT_TENDERS = [
  { key: 'cash_amount', label: 'Cash' },
  { key: 'mobile_money_amount', label: 'Mobile money' },
  { key: 'card_amount', label: 'Card' },
  { key: 'credit_amount', label: 'Credit' },
] as const;

function CollectionReceiptPanel({
  shiftID,
  stationID,
  expectedCash,
  cashSubmission,
  loading,
  receipt,
  nameByUserID,
  onChanged,
}: {
  shiftID: string;
  stationID: string;
  expectedCash?: string;
  cashSubmission: CashSubmission | null;
  loading: boolean;
  receipt: CollectionReceipt | null;
  nameByUserID: Map<string, string>;
  onChanged: () => void;
}) {
  const [received, setReceived] = useState('');
  const [reason, setReason] = useState('');
  const [comment, setComment] = useState('');
  // The supervisor's verdict on the handover. received (default) clears the
  // gate; rejected sends it back to the attendant; flagged opens an
  // investigation. rejected and flagged are non-terminal HOLDS.
  const [mode, setMode] = useState<'received' | 'rejected' | 'flagged'>('received');

  const canConfirm = usePermission('cash.confirm', { stationID });

  const expected = cashSubmission?.expected_cash ?? expectedCash ?? '0';
  const trimmed = received.trim();
  const validMoney = trimmed !== '' && isMeterDecimal(trimmed) && meterFractionDigits(trimmed) <= 2;
  const differs = validMoney && compareMeterDecimals(trimmed, expected) !== 0;
  // The server demands a reason when received ≠ expected, and always on a
  // rejection or flag — mirror it client-side so the 400 never has to fire.
  const reasonRequired = differs || mode !== 'received';
  const submittable = validMoney && (!reasonRequired || reason.trim() !== '');

  const confirm = useMutation({
    mutationFn: () =>
      api.confirmCashSubmission(shiftID, {
        received_total: trimmed,
        ...(mode !== 'received' ? { status: mode } : {}),
        ...(reason.trim() ? { reason: reason.trim() } : {}),
        ...(comment.trim() ? { supervisor_comment: comment.trim() } : {}),
      }),
    onSuccess: () => {
      setReceived('');
      setReason('');
      setComment('');
      setMode('received');
      onChanged();
    },
  });

  return (
    <Card data-testid="collection-receipt-panel">
      <CardHeader className="gap-1">
        <CardTitle className="flex items-center gap-2 text-base">
          <Wallet className="size-4 text-accent" />
          Collection receipt
        </CardTitle>
        <CardDescription>
          Confirm the cash physically handed over against the expected collection. Separation of
          duties — you cannot confirm a submission you made yourself.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {loading ? (
          <Skeleton className="h-24 rounded-lg" />
        ) : !cashSubmission ? (
          <p className="text-sm text-muted-foreground">
            No cash submission yet — the attendant submits collections after the closing readings
            are verified.
          </p>
        ) : (
          <>
            <div className="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-2">
              <ReceiptRow label="Expected collection" value={formatMoney(expected)} strong />
              <ReceiptRow
                label="Attendant submitted"
                value={formatMoney(cashSubmission.submitted_total)}
                strong
              />
              {RECEIPT_TENDERS.map((t) => (
                <ReceiptRow
                  key={t.key}
                  label={t.label}
                  value={formatMoney(cashSubmission[t.key])}
                />
              ))}
              <ReceiptRow
                label="Variance vs expected"
                value={formatMoney(cashSubmission.variance)}
                tone={
                  compareMeterDecimals(
                    cashSubmission.submitted_total,
                    cashSubmission.expected_cash,
                  ) === 0
                    ? 'success'
                    : 'danger'
                }
              />
            </div>
            {cashSubmission.notes ? (
              <p className="rounded-md bg-muted/40 px-3 py-2 text-sm">
                <span className="text-muted-foreground">Attendant&apos;s note: </span>
                {cashSubmission.notes}
              </p>
            ) : null}

            {/* A HOLD receipt (rejected/flagged) is not terminal: the supervisor
                can re-confirm it after settling the dispute. A terminal receipt
                (received/approved_with_difference) is final — read-only. */}
            {receipt ? <ReceiptStatus receipt={receipt} nameByUserID={nameByUserID} /> : null}
            {receipt &&
            (receipt.status === 'received' ||
              receipt.status === 'approved_with_difference') ? null : canConfirm === false ? (
              receipt ? null : (
                <p className="text-sm text-muted-foreground" data-testid="receipt-readonly">
                  Awaiting collection confirmation — recording the receipt requires the cash.confirm
                  permission.
                </p>
              )
            ) : (
              <div className="flex flex-col gap-2 rounded-lg border border-border/70 bg-muted/20 p-3">
                {receipt ? (
                  <p className="text-sm text-muted-foreground">
                    Re-confirm the handover below once the {receipt.status} is resolved.
                  </p>
                ) : null}
                <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                  Record receipt
                </span>
                <label className="flex flex-col gap-1 text-sm">
                  <span className="text-muted-foreground">Received total</span>
                  <Input
                    aria-label="Received total"
                    className="text-right font-mono tabular-nums"
                    type="text"
                    inputMode="decimal"
                    autoComplete="off"
                    value={received}
                    onChange={(e) => setReceived(e.target.value)}
                    placeholder="0.00"
                  />
                </label>
                {trimmed !== '' && !validMoney ? (
                  <p className="text-sm text-danger" role="alert">
                    Enter a money amount with at most 2 decimals, like 1445500 or 1445500.50.
                  </p>
                ) : differs ? (
                  <p className="text-sm text-warning" role="status">
                    Received differs from expected by{' '}
                    <span className="font-mono tabular-nums">
                      {compareMeterDecimals(trimmed, expected) < 0 ? '-' : '+'}
                      {subtractMeterDecimals(
                        compareMeterDecimals(trimmed, expected) < 0 ? expected : trimmed,
                        compareMeterDecimals(trimmed, expected) < 0 ? trimmed : expected,
                      )}
                    </span>{' '}
                    — a reason is required.
                  </p>
                ) : null}
                <label className="flex flex-col gap-1 text-sm">
                  <span className="text-muted-foreground">
                    Reason{reasonRequired ? ' (required)' : ' (optional)'}
                  </span>
                  <Input
                    aria-label="Receipt reason"
                    type="text"
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                    placeholder={
                      mode === 'rejected'
                        ? 'Why the handover is rejected'
                        : mode === 'flagged'
                          ? 'Why the handover is flagged for investigation'
                          : 'Why the received amount differs'
                    }
                  />
                </label>
                <label className="flex flex-col gap-1 text-sm">
                  <span className="text-muted-foreground">Comment (optional)</span>
                  <Input
                    aria-label="Receipt comment"
                    type="text"
                    value={comment}
                    onChange={(e) => setComment(e.target.value)}
                  />
                </label>
                <fieldset className="flex flex-col gap-1.5 text-sm">
                  <legend className="text-muted-foreground">Verdict</legend>
                  <label className="flex items-center gap-2">
                    <input
                      type="radio"
                      name="receipt-verdict"
                      checked={mode === 'received'}
                      onChange={() => setMode('received')}
                      aria-label="Confirm the handover"
                    />
                    Confirm the cash received
                  </label>
                  <label className="flex items-center gap-2">
                    <input
                      type="radio"
                      name="receipt-verdict"
                      checked={mode === 'rejected'}
                      onChange={() => setMode('rejected')}
                      aria-label="Reject this handover"
                    />
                    Reject — send back to the attendant to resubmit (reason required)
                  </label>
                  <label className="flex items-center gap-2">
                    <input
                      type="radio"
                      name="receipt-verdict"
                      checked={mode === 'flagged'}
                      onChange={() => setMode('flagged')}
                      aria-label="Flag this handover for investigation"
                    />
                    Flag for investigation — hold the cash review (reason required)
                  </label>
                </fieldset>
                {confirm.isError ? (
                  <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                    {confirm.error instanceof SdkError && confirm.error.status === 403
                      ? apiErrorMessage(
                          confirm.error,
                          'You cannot confirm a submission you made yourself.',
                        )
                      : apiErrorMessage(confirm.error, "Couldn't record the receipt.")}
                  </p>
                ) : null}
                <PermissionGate permission="cash.confirm" stationId={stationID}>
                  <Button
                    className="self-start"
                    size="sm"
                    variant={mode === 'rejected' ? 'danger' : 'primary'}
                    disabled={!submittable || confirm.isPending}
                    onClick={() => confirm.mutate()}
                  >
                    {confirm.isPending
                      ? 'Recording…'
                      : mode === 'rejected'
                        ? 'Reject handover'
                        : mode === 'flagged'
                          ? 'Flag handover'
                          : 'Confirm receipt'}
                  </Button>
                </PermissionGate>
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}

function ReceiptRow({
  label,
  value,
  strong,
  tone,
}: {
  label: string;
  value: string;
  strong?: boolean;
  tone?: 'success' | 'danger';
}) {
  return (
    <div className="flex items-center justify-between gap-2">
      <span className="text-muted-foreground">{label}</span>
      <span
        className={
          `font-mono tabular-nums${strong ? ' font-semibold' : ''}` +
          (tone === 'danger' ? ' text-danger' : tone === 'success' ? ' text-success' : '')
        }
      >
        {value}
      </span>
    </div>
  );
}

function ReceiptStatus({
  receipt,
  nameByUserID,
}: {
  receipt: CollectionReceipt;
  nameByUserID: Map<string, string>;
}) {
  const badge =
    receipt.status === 'received' ? (
      <Badge tone="success">Received — balanced</Badge>
    ) : receipt.status === 'approved_with_difference' ? (
      <Badge tone="warning">Approved with difference</Badge>
    ) : receipt.status === 'flagged' ? (
      <Badge tone="warning">Flagged for investigation</Badge>
    ) : (
      <Badge tone="danger">Rejected</Badge>
    );
  return (
    <div
      className="flex flex-col gap-1.5 rounded-lg border border-border/70 bg-muted/20 p-3 text-sm"
      data-testid="receipt-status"
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="font-medium">
          Receipt recorded by{' '}
          {nameByUserID.get(receipt.received_by) ?? receipt.received_by.slice(0, 8)} ·{' '}
          {new Date(receipt.received_at).toLocaleString()}
        </span>
        {badge}
      </div>
      <div className="flex flex-wrap gap-x-6 gap-y-1 text-muted-foreground">
        <span>
          Received:{' '}
          <span className="font-mono tabular-nums text-foreground">
            {formatMoney(receipt.supervisor_received_total)}
          </span>
        </span>
        <span>
          Difference:{' '}
          <span
            className={`font-mono tabular-nums ${
              compareMeterDecimals(receipt.supervisor_received_total, receipt.expected_amount) === 0
                ? 'text-success'
                : 'text-danger'
            }`}
          >
            {formatMoney(receipt.difference)}
          </span>
        </span>
      </div>
      {receipt.reason ? (
        <p>
          <span className="text-muted-foreground">Reason: </span>
          {receipt.reason}
        </p>
      ) : null}
      {receipt.supervisor_comment ? (
        <p>
          <span className="text-muted-foreground">Comment: </span>
          {receipt.supervisor_comment}
        </p>
      ) : null}
    </div>
  );
}

// --- Approval readiness ---------------------------------------------------------

interface ChecklistItem {
  key: string;
  ok: boolean;
  label: string;
}

function ApprovalReadiness({
  shift,
  stationID,
  pendingReadings,
  verifications,
  cashSubmission,
  receipt,
  openExceptions,
  loading,
  onChanged,
}: {
  shift: ShiftDetail;
  stationID: string;
  pendingReadings: MeterReading[];
  verifications: ReadingVerification[];
  cashSubmission: CashSubmission | null;
  receipt: CollectionReceipt | null;
  openExceptions: number;
  loading: boolean;
  onChanged: () => void;
}) {
  const [gateError, setGateError] = useState<string | null>(null);

  // Rejected/flagged are non-terminal HOLDS the server gates approval on,
  // distinct from a reading that simply has no verification row yet.
  const rejectedCount = verifications.filter((v) => v.status === 'rejected').length;
  const flaggedCount = verifications.filter((v) => v.status === 'flagged').length;

  const approve = useMutation({
    mutationFn: () => api.approveShift(shift.id),
    onSuccess: () => {
      setGateError(null);
      onChanged();
    },
    onError: (e) => {
      // The server's 409 gates carry machine-readable codes — translate them
      // into the same language as the checklist instead of a raw error.
      const code = apiErrorCode(e);
      if (code === 'readings_rejected_pending') {
        const raw = apiErrorBody(e)?.rejected_count;
        const n = typeof raw === 'number' ? raw : null;
        setGateError(
          `${n ?? 'Some'} reading${n === 1 ? '' : 's'} rejected — the attendant must resubmit the closing reading${n === 1 ? '' : 's'}, then re-verify before approving.`,
        );
      } else if (code === 'readings_flagged_pending') {
        const raw = apiErrorBody(e)?.flagged_count;
        const n = typeof raw === 'number' ? raw : null;
        setGateError(
          `${n ?? 'Some'} reading${n === 1 ? ' is' : 's are'} flagged for investigation — resolve the flag${n === 1 ? '' : 's'} (correct and re-verify) before approving.`,
        );
      } else if (code === 'readings_unverified') {
        const raw = apiErrorBody(e)?.unverified_count;
        const n = typeof raw === 'number' ? raw : null;
        setGateError(
          `${n ?? 'Some'} closing reading${n === 1 ? ' is' : 's are'} still awaiting verification — verify them above, then approve.`,
        );
      } else if (code === 'collection_unconfirmed') {
        setGateError(
          'The collection has not been confirmed — record the cash receipt above, then approve.',
        );
      } else {
        setGateError(apiErrorMessage(e, "Couldn't approve the shift."));
      }
      onChanged();
    },
  });

  if (shift.status === 'approved') {
    return (
      <Card data-testid="approval-readiness">
        <CardContent className="flex items-center gap-2 pt-6 text-sm">
          <CheckCircle2 className="size-4 text-success" />
          Shift approved
          {shift.approved_at ? ` ${new Date(shift.approved_at).toLocaleString()}` : ''} — revenue
          recognized from the verified readings.
        </CardContent>
      </Card>
    );
  }

  const readingsBlocked = pendingReadings.length + rejectedCount + flaggedCount;
  const items: ChecklistItem[] = [
    {
      key: 'readings',
      ok: readingsBlocked === 0,
      // Holds are reported ahead of unverified readings so the supervisor sees
      // the actionable blocker first, matching the server's gate precedence.
      label:
        readingsBlocked === 0
          ? 'All closing readings verified'
          : rejectedCount > 0
            ? `${rejectedCount} reading${rejectedCount === 1 ? '' : 's'} rejected — attendant must resubmit`
            : flaggedCount > 0
              ? `${flaggedCount} reading${flaggedCount === 1 ? '' : 's'} flagged for investigation`
              : `${pendingReadings.length} reading${pendingReadings.length === 1 ? '' : 's'} awaiting verification`,
    },
    {
      key: 'collection',
      // Only a TERMINAL-GOOD receipt clears the gate; rejected/flagged are holds.
      ok:
        !!cashSubmission &&
        !!receipt &&
        (receipt.status === 'received' || receipt.status === 'approved_with_difference'),
      label: !cashSubmission
        ? 'Cash not submitted yet'
        : !receipt
          ? 'Collection not confirmed'
          : receipt.status === 'rejected'
            ? 'Collection rejected — awaiting resubmission and a fresh receipt'
            : receipt.status === 'flagged'
              ? 'Collection flagged for investigation — resolve before approving'
              : 'Collection receipt recorded',
    },
    {
      key: 'exceptions',
      ok: openExceptions === 0,
      label:
        openExceptions === 0
          ? 'No open exceptions'
          : `${openExceptions} open exception${openExceptions === 1 ? '' : 's'} to resolve`,
    },
  ];
  const ready = items.every((i) => i.ok);

  return (
    <Card data-testid="approval-readiness">
      <CardHeader className="gap-1">
        <CardTitle className="text-base">Approval readiness</CardTitle>
        <CardDescription>
          Everything the server requires before this shift can be approved.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {loading ? (
          <Skeleton className="h-20 rounded-lg" />
        ) : (
          <ul className="flex flex-col gap-1.5 text-sm">
            {items.map((item) => (
              <li key={item.key} className="flex items-center gap-2" data-testid="readiness-item">
                {item.ok ? (
                  <CheckCircle2 className="size-4 shrink-0 text-success" aria-hidden />
                ) : (
                  <XCircle className="size-4 shrink-0 text-warning" aria-hidden />
                )}
                <span className={item.ok ? '' : 'text-warning'}>{item.label}</span>
              </li>
            ))}
          </ul>
        )}
        {gateError ? (
          <p className="rounded-md bg-warning/10 px-3 py-2 text-sm text-warning" role="alert">
            {gateError}
          </p>
        ) : null}
        <PermissionGate permission="shift.approve" stationId={stationID}>
          <Button
            className="self-start"
            disabled={loading || !ready || approve.isPending}
            title={ready ? undefined : 'Complete the checklist first'}
            onClick={() => approve.mutate()}
          >
            {approve.isPending ? 'Approving…' : 'Approve shift'}
          </Button>
        </PermissionGate>
      </CardContent>
    </Card>
  );
}
