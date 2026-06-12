'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useMutation, useQueryClient } from '@tanstack/react-query';
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
import { useT, type Messages } from '@/lib/i18n';
import { formatMoney } from '@/lib/money';
import {
  getSyncEngine,
  isOfflineError,
  useAttendantSnapshot,
  useSyncEngineState,
} from '@/lib/offline';

const QUERY_KEY = ['attendant-current-shift'];

/** Sentinel a mutation returns when the action was queued offline instead. */
const QUEUED = Symbol('queued-offline');

/**
 * Workflow checklist stages in order. next_action maps onto a stage so the
 * attendant always sees where they are (PRD §5.2 guided workflow). Wait
 * states (await_*, blocked) sit on the stage being waited on. Labels come
 * from the dictionary at render time (Phase 6b i18n).
 */
const STAGE_KEYS = [
  'check_in',
  'confirm_assignment',
  'verify_opening_readings',
  'working',
  'submit_closing_readings',
  'await_reading_verification',
  'submit_collections',
  'await_collection_receipt',
  'complete',
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
  const idx = STAGE_KEYS.findIndex((key) => key === snapshot.next_action);
  return idx === -1 ? 0 : idx;
}

export default function AttendantHomePage() {
  const t = useT();
  const qc = useQueryClient();
  const [actionError, setActionError] = useState<string | null>(null);
  const [justCheckedIn, setJustCheckedIn] = useState(false);
  const [queuedNotice, setQueuedNotice] = useState(false);

  const snapshot = useAttendantSnapshot({
    // The workflow advances on supervisor actions too — keep the screen live.
    refetchInterval: 30_000,
  });
  const engineState = useSyncEngineState();

  const shiftID = snapshot.data?.shift?.id ?? '';

  // Actions already saved on this phone for this shift (Phase 6a offline
  // queue) — the UI treats them as done-but-unsynced rather than re-offering
  // the button.
  const waitingItems = engineState.items.filter(
    (i) => i.shift_id === shiftID && (i.sync_status === 'pending' || i.sync_status === 'syncing'),
  );
  const queuedCheckIn = waitingItems.some((i) => i.action_type === 'check_in');
  const queuedConfirmIDs = new Set(
    waitingItems
      .filter((i) => i.action_type === 'confirm_assignment')
      .map((i) => (i.payload as { assignment_id: string }).assignment_id),
  );

  // Check-in with optimistic UI: flip the local snapshot immediately, roll
  // back on failure, and reconcile with the server's computed state after.
  // A connectivity failure is NOT an error: the check-in is queued on this
  // phone (the optimistic state stands) and replays when online returns.
  const checkIn = useMutation({
    mutationFn: async () => {
      const payload = { device_info: { app: 'attendant-pwa' } };
      try {
        return await api.checkInToShift(shiftID, payload);
      } catch (e) {
        if (isOfflineError(e)) {
          await getSyncEngine().enqueue({
            action_type: 'check_in',
            shift_id: shiftID,
            payload,
          });
          return QUEUED;
        }
        throw e;
      }
    },
    onMutate: async () => {
      await qc.cancelQueries({ queryKey: QUERY_KEY });
      const prev = qc.getQueryData<AttendantCurrentShift>(QUERY_KEY);
      if (prev) {
        qc.setQueryData<AttendantCurrentShift>(QUERY_KEY, {
          ...prev,
          attendance: { ...prev.attendance, status: 'checked_in' },
          user_message: t.home.checkedInBanner,
        });
      }
      return { prev };
    },
    onError: (e, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(QUERY_KEY, ctx.prev);
      setActionError(e instanceof SdkError ? e.message : t.home.errCheckIn);
    },
    onSuccess: (result) => {
      setActionError(null);
      if (result === QUEUED) setQueuedNotice(true);
      else setJustCheckedIn(true);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: QUERY_KEY }),
  });

  // Confirms every still-unconfirmed assignment (idempotent server-side).
  // Offline, each confirmation is queued per assignment and replayed in order.
  const confirmAssignments = useMutation({
    mutationFn: async () => {
      const pending = (snapshot.data?.assignments ?? []).filter(
        (a) => !a.confirmed_at && !queuedConfirmIDs.has(a.assignment_id),
      );
      let queuedAny = false;
      for (const a of pending) {
        try {
          await api.confirmNozzleAssignment(shiftID, a.assignment_id);
        } catch (e) {
          if (isOfflineError(e)) {
            await getSyncEngine().enqueue({
              action_type: 'confirm_assignment',
              shift_id: shiftID,
              payload: {
                assignment_id: a.assignment_id,
                pump_number: a.pump_number,
                nozzle_number: a.nozzle_number,
              },
            });
            queuedAny = true;
            continue;
          }
          throw e;
        }
      }
      return queuedAny ? QUEUED : undefined;
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : t.home.errConfirm),
    onSuccess: (result) => {
      setActionError(null);
      if (result === QUEUED) setQueuedNotice(true);
    },
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
  // Hard error only when there is nothing to show; with a cached snapshot
  // the screen renders the last synced info (the shell strip marks it).
  if (snapshot.showError) {
    return (
      <ErrorState
        title={t.common.couldNotLoadShift}
        description={String((snapshot.error as Error).message)}
        action={
          <Button variant="secondary" onClick={() => snapshot.refetch()}>
            {t.common.tryAgain}
          </Button>
        }
      />
    );
  }

  const data = snapshot.data as AttendantCurrentShift;
  const s = data.shift;
  const busy = checkIn.isPending || confirmAssignments.isPending;

  // ----- No shift yet: off duty / expected today -----
  if (!s) {
    return (
      <div className="flex flex-col gap-4">
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">
              {data.status === 'expected_today' ? t.home.onDutyToday : t.home.offDuty}
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <p className="text-base text-muted-foreground" role="status">
              {data.user_message}
            </p>
            {data.expected_today ? (
              <div className="flex flex-wrap items-center gap-2">
                <Badge tone="info" className="capitalize">
                  {t.home.slotShiftBadge(data.expected_today.slot)}
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
              {t.home.checkAgain}
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  const currentStage = stageIndex(data);
  const slotLabel = s.slot ? t.home.slotShiftHeader(s.slot) : s.name;

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

  const attendanceLine =
    data.attendance.status === 'checked_in'
      ? t.home.attendanceCheckedIn
      : data.attendance.status === 'checked_out'
        ? t.home.attendanceCheckedOut
        : t.home.attendanceNotCheckedIn;

  return (
    <div className="flex flex-col gap-4">
      {/* Shift header */}
      <Card>
        <CardContent className="flex flex-col gap-1 pt-5">
          <div className="flex items-start justify-between gap-2">
            <div>
              <p className="text-lg font-semibold leading-tight">{data.station?.name}</p>
              <p className="text-sm text-muted-foreground">
                {slotLabel} ·{' '}
                {t.home.openedAt(
                  new Date(s.opened_at).toLocaleTimeString([], {
                    hour: '2-digit',
                    minute: '2-digit',
                  }),
                )}
              </p>
            </div>
            <Badge
              tone={
                s.status === 'open' ? 'success' : s.status === 'approved' ? 'neutral' : 'warning'
              }
              className="capitalize"
            >
              {t.home.shiftStatusBadge(s.status)}
            </Badge>
          </div>
          <p className="text-sm text-muted-foreground">
            {attendanceLine}
            {data.attendance.check_in_at
              ? t.home.attendanceSince(
                  new Date(data.attendance.check_in_at).toLocaleTimeString([], {
                    hour: '2-digit',
                    minute: '2-digit',
                  }),
                )
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
          {t.home.checkedInBanner}
        </p>
      ) : null}
      {queuedNotice || queuedCheckIn || queuedConfirmIDs.size > 0 ? (
        <p
          className="rounded-md bg-warning/10 px-3 py-2 text-sm font-medium text-warning"
          role="status"
        >
          {t.common.savedOnPhone}
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
        queuedCheckIn={queuedCheckIn}
        queuedConfirms={
          data.assignments.length > 0 &&
          data.assignments.every((a) => a.confirmed_at || queuedConfirmIDs.has(a.assignment_id))
        }
        onCheckIn={() => checkIn.mutate()}
        onConfirm={() => confirmAssignments.mutate()}
        t={t}
      />

      {/* Workflow checklist */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t.home.shiftProgress}</CardTitle>
        </CardHeader>
        <CardContent>
          <ol className="flex flex-col gap-2.5">
            {STAGE_KEYS.map((stageKey, i) => {
              const state = i < currentStage ? 'done' : i === currentStage ? 'current' : 'pending';
              return (
                <li key={stageKey} className="flex items-center gap-3 text-base">
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
                    {t.home.stages[stageKey]}
                    {state === 'done' ? (
                      <span className="sr-only">{t.home.srStageDone}</span>
                    ) : null}
                    {state === 'current' ? (
                      <span className="sr-only">{t.home.srStageCurrent}</span>
                    ) : null}
                  </span>
                  {stageKey === 'verify_opening_readings' && data.assignments.length > 0 ? (
                    <span className="ml-auto whitespace-nowrap text-sm tabular-nums text-muted-foreground">
                      {t.home.nozzlesVerifiedCount(openingsVerified, data.assignments.length)}
                    </span>
                  ) : null}
                  {stageKey === 'await_reading_verification' && data.assignments.length > 0 ? (
                    <Link
                      href="/attendant/review-status"
                      className="ml-auto whitespace-nowrap text-sm tabular-nums text-muted-foreground underline-offset-2 hover:underline"
                    >
                      {t.home.verifiedCount(readingsVerified, data.assignments.length)}
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
          <CardTitle className="text-base">{t.home.myNozzles}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {data.assignments.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t.home.noNozzlesYet}</p>
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
                      {t.common.pumpNozzle(a.pump_number, a.nozzle_number)}
                    </span>
                  </div>
                  <div className="mt-2 flex flex-wrap items-center gap-2 text-sm">
                    {a.confirmed_at ? (
                      <Badge tone="success">{t.home.confirmedBadge}</Badge>
                    ) : queuedConfirmIDs.has(a.assignment_id) ? (
                      <Badge tone="info">{t.home.confirmedWaitingSync}</Badge>
                    ) : (
                      <Badge tone="warning">{t.home.awaitingConfirmation}</Badge>
                    )}
                    {reading?.opening_reading ? (
                      <span className="font-mono text-xs tabular-nums text-muted-foreground">
                        {t.home.openReading(reading.opening_reading)}
                      </span>
                    ) : null}
                    {reading?.closing_reading ? (
                      <span className="font-mono text-xs tabular-nums text-muted-foreground">
                        {t.home.closeReading(reading.closing_reading)}
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
                        {t.home.readingStatusBadge(reading.verification_status)}
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
            <CardTitle className="text-base">{t.home.collections}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-1.5 text-base">
            <Row
              label={t.home.expected}
              value={formatMoney(data.expected_cash, { fallback: '0.00' })}
            />
            {data.cash_submission ? (
              <Row
                label={t.home.submitted}
                value={formatMoney(data.cash_submission.submitted_total, { fallback: '0.00' })}
              />
            ) : null}
            {data.collection_receipt ? (
              <>
                <Row
                  label={t.home.received}
                  value={formatMoney(data.collection_receipt.supervisor_received_total, {
                    fallback: '0.00',
                  })}
                />
                <div className="pt-1">
                  <Badge
                    tone={data.collection_receipt.status === 'rejected' ? 'danger' : 'success'}
                    className="capitalize"
                  >
                    {t.home.receiptStatusBadge(data.collection_receipt.status)}
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
  queuedCheckIn,
  queuedConfirms,
  onCheckIn,
  onConfirm,
  t,
}: {
  data: AttendantCurrentShift;
  busy: boolean;
  /** A check-in is already saved on this phone, waiting to sync. */
  queuedCheckIn: boolean;
  /** Every unconfirmed assignment has a confirmation saved on this phone. */
  queuedConfirms: boolean;
  onCheckIn: () => void;
  onConfirm: () => void;
  t: Messages;
}) {
  switch (data.next_action) {
    case 'check_in':
      // Already captured offline: don't re-offer the button — the queue
      // replays it (idempotent server-side) when the connection returns.
      if (queuedCheckIn) {
        return (
          <Button className="h-14 text-lg" disabled>
            {t.home.ctaCheckedInQueued}
          </Button>
        );
      }
      return (
        <Button className="h-14 text-lg" disabled={busy} onClick={onCheckIn}>
          {busy ? <Loader2 className="size-5 animate-spin" aria-hidden /> : null}
          {t.home.ctaCheckIn}
        </Button>
      );
    case 'confirm_assignment':
      if (queuedConfirms) {
        return (
          <Button className="h-14 text-lg" disabled>
            {t.home.ctaConfirmedQueued}
          </Button>
        );
      }
      return (
        <Button className="h-14 text-lg" disabled={busy} onClick={onConfirm}>
          {busy ? <Loader2 className="size-5 animate-spin" aria-hidden /> : null}
          {t.home.ctaConfirmNozzles}
        </Button>
      );
    case 'verify_opening_readings':
      // Native Phase 2 screen — no longer a deep-link to the full site.
      return (
        <Button asChild className="h-14 text-lg">
          <Link href="/attendant/opening-readings">
            {t.home.ctaVerifyOpenings}
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
            {t.home.ctaEnterClosings}
            <ArrowRight className="size-5" aria-hidden />
          </Link>
        </Button>
      );
    case 'submit_closing_readings':
      return (
        <Button asChild className="h-14 text-lg">
          <Link href="/attendant/closing-readings">
            {t.home.ctaFinishClosings}
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
            {t.home.ctaViewReviewStatus}
            <ArrowRight className="size-5" aria-hidden />
          </Link>
        </Button>
      );
    case 'submit_collections':
      // Native Phase 4 screen — expected basis + tender breakdown form.
      return (
        <Button asChild className="h-14 text-lg">
          <Link href="/attendant/collections">
            {t.home.ctaSubmitCollections}
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
            {t.home.ctaViewCollectionStatus}
            <ArrowRight className="size-5" aria-hidden />
          </Link>
        </Button>
      );
    case 'complete':
      // Native Phase 4 end-of-shift summary, where check-out lives.
      return (
        <Button asChild className="h-14 text-lg">
          <Link href="/attendant/shift-complete">
            {t.home.ctaFinishShift}
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
            {t.home.ctaViewCollectionStatus}
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
