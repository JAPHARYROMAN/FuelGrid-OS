'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowRight, Check, CircleDashed, Loader2 } from 'lucide-react';

import { SdkError, type AttendantCurrentShift } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  ErrorState,
  Skeleton,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';

const QUERY_KEY = ['attendant-current-shift'];

/**
 * Workflow checklist stages in order. next_action maps onto a stage so the
 * attendant always sees where they are (PRD §5.2 guided workflow). Wait
 * states (await_*, blocked) sit on the stage being waited on.
 */
const STAGES = [
  { key: 'check_in', label: 'Check in' },
  { key: 'confirm_assignment', label: 'Confirm nozzles' },
  { key: 'verify_opening_readings', label: 'Verify opening readings' },
  { key: 'working', label: 'Work the shift' },
  { key: 'submit_closing_readings', label: 'Submit closing readings' },
  { key: 'await_reading_verification', label: 'Supervisor verifies readings' },
  { key: 'submit_collections', label: 'Submit collections' },
  { key: 'await_collection_receipt', label: 'Supervisor confirms cash' },
  { key: 'complete', label: 'Shift complete' },
] as const;

function stageIndex(snapshot: AttendantCurrentShift): number {
  if (snapshot.next_action === 'blocked') {
    switch (snapshot.blocking_code) {
      case 'awaiting_nozzle_assignment':
        return 1;
      case 'awaiting_shift_close':
        return 5;
      case 'collection_rejected':
        return 7;
      default:
        return 0;
    }
  }
  const idx = STAGES.findIndex((s) => s.key === snapshot.next_action);
  return idx === -1 ? 0 : idx;
}

export default function AttendantHomePage() {
  const qc = useQueryClient();
  const [actionError, setActionError] = useState<string | null>(null);
  const [justCheckedIn, setJustCheckedIn] = useState(false);

  const snapshot = useQuery({
    queryKey: QUERY_KEY,
    queryFn: ({ signal }) => api.attendantCurrentShift(signal),
    // The workflow advances on supervisor actions too — keep the screen live.
    refetchInterval: 30_000,
  });

  const shiftID = snapshot.data?.shift?.id ?? '';

  // Check-in with optimistic UI: flip the local snapshot immediately, roll
  // back on failure, and reconcile with the server's computed state after.
  const checkIn = useMutation({
    mutationFn: () => api.checkInToShift(shiftID, { device_info: { app: 'attendant-pwa' } }),
    onMutate: async () => {
      await qc.cancelQueries({ queryKey: QUERY_KEY });
      const prev = qc.getQueryData<AttendantCurrentShift>(QUERY_KEY);
      if (prev) {
        qc.setQueryData<AttendantCurrentShift>(QUERY_KEY, {
          ...prev,
          attendance: { ...prev.attendance, status: 'checked_in' },
          user_message: 'You are checked in.',
        });
      }
      return { prev };
    },
    onError: (e, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(QUERY_KEY, ctx.prev);
      setActionError(e instanceof SdkError ? e.message : 'Could not check in. Try again.');
    },
    onSuccess: () => {
      setActionError(null);
      setJustCheckedIn(true);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: QUERY_KEY }),
  });

  // Confirms every still-unconfirmed assignment (idempotent server-side).
  const confirmAssignments = useMutation({
    mutationFn: async () => {
      const pending = (snapshot.data?.assignments ?? []).filter((a) => !a.confirmed_at);
      for (const a of pending) {
        await api.confirmNozzleAssignment(shiftID, a.assignment_id);
      }
    },
    onError: (e) =>
      setActionError(
        e instanceof SdkError ? e.message : 'Could not confirm the assignment. Try again.',
      ),
    onSuccess: () => setActionError(null),
    onSettled: () => qc.invalidateQueries({ queryKey: QUERY_KEY }),
  });

  if (snapshot.isPending) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-28 rounded-xl" />
        <Skeleton className="h-14 rounded-xl" />
        <Skeleton className="h-48 rounded-xl" />
      </div>
    );
  }
  if (snapshot.isError) {
    return (
      <ErrorState
        title="Couldn't load your shift"
        description={String((snapshot.error as Error).message)}
        onRetry={() => snapshot.refetch()}
      />
    );
  }

  const data = snapshot.data;
  const s = data.shift;
  const busy = checkIn.isPending || confirmAssignments.isPending;

  // ----- No shift yet: off duty / expected today -----
  if (!s) {
    return (
      <div className="flex flex-col gap-4">
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">
              {data.status === 'expected_today' ? 'You are on duty today' : 'Off duty'}
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <p className="text-base text-muted-foreground" role="status">
              {data.user_message}
            </p>
            {data.expected_today ? (
              <div className="flex flex-wrap items-center gap-2">
                <Badge tone="info" className="capitalize">
                  {data.expected_today.slot} shift
                </Badge>
                <Badge tone="neutral">{data.expected_today.team_name}</Badge>
                {data.station ? <Badge tone="neutral">{data.station.name}</Badge> : null}
              </div>
            ) : null}
            <Button
              className="h-12 text-base"
              variant="outline"
              disabled={snapshot.isFetching}
              onClick={() => snapshot.refetch()}
            >
              {snapshot.isFetching ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
              Check again
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  const currentStage = stageIndex(data);
  const slotLabel = s.slot ? `${s.slot.charAt(0).toUpperCase()}${s.slot.slice(1)} shift` : s.name;

  // Per-nozzle opening verification progress (Phase 2): how many of the
  // actor's assigned nozzles already have an opening reading captured.
  const openingsVerified = data.assignments.filter((a) =>
    data.readings.some((r) => r.nozzle_id === a.nozzle_id && r.opening_reading != null),
  ).length;

  // Per-nozzle supervisor review progress (Phase 3): closings the supervisor
  // has decided on (approved / corrected / rejected — anything past pending).
  const readingsVerified = data.assignments.filter((a) =>
    data.readings.some(
      (r) =>
        r.nozzle_id === a.nozzle_id &&
        r.closing_reading != null &&
        r.verification_status != null &&
        r.verification_status !== 'pending',
    ),
  ).length;

  return (
    <div className="flex flex-col gap-4">
      {/* Shift header */}
      <Card>
        <CardContent className="flex flex-col gap-1 pt-5">
          <div className="flex items-start justify-between gap-2">
            <div>
              <p className="text-lg font-semibold leading-tight">{data.station?.name}</p>
              <p className="text-sm text-muted-foreground">
                {slotLabel} · opened{' '}
                {new Date(s.opened_at).toLocaleTimeString([], {
                  hour: '2-digit',
                  minute: '2-digit',
                })}
              </p>
            </div>
            <Badge
              tone={
                s.status === 'open' ? 'success' : s.status === 'approved' ? 'neutral' : 'warning'
              }
              className="capitalize"
            >
              Shift {s.status}
            </Badge>
          </div>
          <p className="text-sm text-muted-foreground">
            You are {data.attendance.status === 'not_checked_in' ? 'not checked in' : null}
            {data.attendance.status === 'checked_in' ? 'checked in' : null}
            {data.attendance.status === 'checked_out' ? 'checked out' : null}
            {data.attendance.check_in_at
              ? ` (since ${new Date(data.attendance.check_in_at).toLocaleTimeString([], {
                  hour: '2-digit',
                  minute: '2-digit',
                })})`
              : null}
          </p>
        </CardContent>
      </Card>

      {/* Status + errors */}
      {justCheckedIn ? (
        <p
          className="rounded-md bg-success/10 px-3 py-2 text-sm font-medium text-success"
          role="status"
        >
          You are checked in.
        </p>
      ) : null}
      {actionError ? (
        <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
          {actionError}
        </p>
      ) : null}
      <p
        className={
          'rounded-md px-3 py-2 text-base ' +
          (data.next_action === 'blocked'
            ? 'bg-warning/10 text-warning'
            : data.next_action === 'complete'
              ? 'bg-success/10 text-success'
              : 'bg-accent/10 text-foreground')
        }
        role="status"
      >
        {data.user_message}
      </p>

      {/* THE next-action button */}
      <NextActionButton
        data={data}
        busy={busy}
        onCheckIn={() => checkIn.mutate()}
        onConfirm={() => confirmAssignments.mutate()}
      />

      {/* Workflow checklist */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Shift progress</CardTitle>
        </CardHeader>
        <CardContent>
          <ol className="flex flex-col gap-2.5">
            {STAGES.map((stage, i) => {
              const state = i < currentStage ? 'done' : i === currentStage ? 'current' : 'pending';
              return (
                <li key={stage.key} className="flex items-center gap-3 text-base">
                  {state === 'done' ? (
                    <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-success/15 text-success">
                      <Check className="size-4" aria-hidden />
                    </span>
                  ) : state === 'current' ? (
                    <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-accent/15 text-accent">
                      <ArrowRight className="size-4" aria-hidden />
                    </span>
                  ) : (
                    <span className="flex size-6 shrink-0 items-center justify-center rounded-full text-muted-foreground">
                      <CircleDashed className="size-4" aria-hidden />
                    </span>
                  )}
                  <span
                    className={
                      state === 'current'
                        ? 'font-semibold'
                        : state === 'done'
                          ? 'text-muted-foreground line-through decoration-success/40'
                          : 'text-muted-foreground'
                    }
                  >
                    {stage.label}
                    {state === 'done' ? <span className="sr-only"> — done</span> : null}
                    {state === 'current' ? <span className="sr-only"> — current step</span> : null}
                  </span>
                  {stage.key === 'verify_opening_readings' && data.assignments.length > 0 ? (
                    <span className="ml-auto whitespace-nowrap text-sm tabular-nums text-muted-foreground">
                      {openingsVerified} of {data.assignments.length} nozzles verified
                    </span>
                  ) : null}
                  {stage.key === 'await_reading_verification' && data.assignments.length > 0 ? (
                    <Link
                      href="/attendant/review-status"
                      className="ml-auto whitespace-nowrap text-sm tabular-nums text-muted-foreground underline-offset-2 hover:underline"
                    >
                      {readingsVerified} of {data.assignments.length} verified
                    </Link>
                  ) : null}
                </li>
              );
            })}
          </ol>
        </CardContent>
      </Card>

      {/* My nozzles */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">My nozzles</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {data.assignments.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No nozzles assigned to you yet. Your supervisor assigns them after you check in.
            </p>
          ) : (
            data.assignments.map((a) => {
              const reading = data.readings.find((r) => r.nozzle_id === a.nozzle_id);
              return (
                <div key={a.assignment_id} className="rounded-lg border border-border p-3">
                  <div className="flex items-center justify-between gap-2">
                    <span className="flex items-center gap-2 text-base font-medium">
                      <span
                        className="inline-block size-3 rounded-full border border-border"
                        style={{ backgroundColor: a.product_color }}
                        aria-hidden
                      />
                      {a.product_name}
                    </span>
                    <span className="font-mono text-xs text-muted-foreground">
                      Pump {a.pump_number} · Nozzle {a.nozzle_number}
                    </span>
                  </div>
                  <div className="mt-2 flex flex-wrap items-center gap-2 text-sm">
                    {a.confirmed_at ? (
                      <Badge tone="success">Confirmed</Badge>
                    ) : (
                      <Badge tone="warning">Awaiting your confirmation</Badge>
                    )}
                    {reading?.opening_reading ? (
                      <span className="font-mono text-xs tabular-nums text-muted-foreground">
                        Open {reading.opening_reading}
                      </span>
                    ) : null}
                    {reading?.closing_reading ? (
                      <span className="font-mono text-xs tabular-nums text-muted-foreground">
                        Close {reading.closing_reading}
                      </span>
                    ) : null}
                    {reading?.verification_status ? (
                      <Badge
                        tone={
                          reading.verification_status === 'approved'
                            ? 'success'
                            : reading.verification_status === 'pending'
                              ? 'info'
                              : 'warning'
                        }
                        className="capitalize"
                      >
                        Reading {reading.verification_status}
                      </Badge>
                    ) : null}
                  </div>
                </div>
              );
            })
          )}
        </CardContent>
      </Card>

      {/* Collections (once the shift is closed) */}
      {data.expected_cash ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Collections</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-1.5 text-base">
            <Row label="Expected" value={formatMoney(data.expected_cash, { fallback: '0.00' })} />
            {data.cash_submission ? (
              <Row
                label="Submitted"
                value={formatMoney(data.cash_submission.submitted_total, { fallback: '0.00' })}
              />
            ) : null}
            {data.collection_receipt ? (
              <>
                <Row
                  label="Received"
                  value={formatMoney(data.collection_receipt.supervisor_received_total, {
                    fallback: '0.00',
                  })}
                />
                <div className="pt-1">
                  <Badge
                    tone={data.collection_receipt.status === 'rejected' ? 'danger' : 'success'}
                    className="capitalize"
                  >
                    {data.collection_receipt.status.replaceAll('_', ' ')}
                  </Badge>
                </div>
              </>
            ) : null}
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}

/**
 * The single primary CTA the snapshot's next_action drives. Every stage now
 * has a native mobile screen: opening (Phase 2), closing/review (Phase 3),
 * and collections / shift complete (Phase 4) — no /my-shift deep-links left.
 */
function NextActionButton({
  data,
  busy,
  onCheckIn,
  onConfirm,
}: {
  data: AttendantCurrentShift;
  busy: boolean;
  onCheckIn: () => void;
  onConfirm: () => void;
}) {
  switch (data.next_action) {
    case 'check_in':
      return (
        <Button className="h-14 text-lg" disabled={busy} onClick={onCheckIn}>
          {busy ? <Loader2 className="size-5 animate-spin" aria-hidden /> : null}
          Check in
        </Button>
      );
    case 'confirm_assignment':
      return (
        <Button className="h-14 text-lg" disabled={busy} onClick={onConfirm}>
          {busy ? <Loader2 className="size-5 animate-spin" aria-hidden /> : null}
          Confirm my nozzles
        </Button>
      );
    case 'verify_opening_readings':
      // Native Phase 2 screen — no longer a deep-link to the full site.
      return (
        <Button asChild className="h-14 text-lg">
          <Link href="/attendant/opening-readings">
            Verify opening readings
            <ArrowRight className="size-5" aria-hidden />
          </Link>
        </Button>
      );
    case 'working':
      // Native Phase 3 screen — available early but visually subdued: the
      // shift is still running.
      return (
        <Button asChild className="h-14 text-lg" variant="outline">
          <Link href="/attendant/closing-readings">
            Enter closing readings
            <ArrowRight className="size-5" aria-hidden />
          </Link>
        </Button>
      );
    case 'submit_closing_readings':
      return (
        <Button asChild className="h-14 text-lg">
          <Link href="/attendant/closing-readings">
            Finish closing readings
            <ArrowRight className="size-5" aria-hidden />
          </Link>
        </Button>
      );
    case 'await_reading_verification':
      // A wait state, but with a native place to watch it: the per-nozzle
      // review status (Phase 3).
      return (
        <Button asChild className="h-14 text-lg" variant="outline">
          <Link href="/attendant/review-status">
            View review status
            <ArrowRight className="size-5" aria-hidden />
          </Link>
        </Button>
      );
    case 'submit_collections':
      // Native Phase 4 screen — expected basis + tender breakdown form.
      return (
        <Button asChild className="h-14 text-lg">
          <Link href="/attendant/collections">
            Submit collections
            <ArrowRight className="size-5" aria-hidden />
          </Link>
        </Button>
      );
    case 'await_collection_receipt':
      // A wait state with a native place to watch it: the submitted
      // breakdown + supervisor receipt status (Phase 4).
      return (
        <Button asChild className="h-14 text-lg" variant="outline">
          <Link href="/attendant/collections">
            View collection status
            <ArrowRight className="size-5" aria-hidden />
          </Link>
        </Button>
      );
    case 'complete':
      // Native Phase 4 end-of-shift summary, where check-out lives.
      return (
        <Button asChild className="h-14 text-lg">
          <Link href="/attendant/shift-complete">
            Finish your shift
            <ArrowRight className="size-5" aria-hidden />
          </Link>
        </Button>
      );
    case 'blocked':
      // A rejected collection is the one blocked state with a native screen
      // to consult: the receipt status with the supervisor's reason.
      return data.blocking_code === 'collection_rejected' ? (
        <Button asChild className="h-14 text-lg" variant="outline">
          <Link href="/attendant/collections">
            View collection status
            <ArrowRight className="size-5" aria-hidden />
          </Link>
        </Button>
      ) : null;
    default:
      // Wait states (await_*): nothing to press — the screen refreshes
      // itself and the status strip explains what's happening.
      return null;
  }
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-mono font-medium tabular-nums">{value}</span>
    </div>
  );
}
