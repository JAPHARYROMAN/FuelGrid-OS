'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, ArrowRight, Check, Loader2 } from 'lucide-react';

import {
  SdkError,
  type AttendantAssignment,
  type AttendantCurrentShift,
  type AttendantReading,
} from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  Input,
  Skeleton,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { toast } from '@/lib/toast';
import {
  addMeterDecimals,
  compareMeterDecimals,
  isMeterDecimal,
  meterFractionDigits,
  multiplyMeterDecimal,
  subtractMeterDecimals,
} from '@/lib/meter-decimal';
import {
  getSyncEngine,
  isOfflineError,
  useAttendantSnapshot,
  useSyncEngineState,
} from '@/lib/offline';

const QUERY_KEY = ['attendant-current-shift'];

/**
 * Mirrors the server's rollback rule (readings.ErrMeterRollback): a closing
 * meter can never read below its opening — blocked client-side with the same
 * explanation the server would give.
 */
const LOWER_BLOCKED_MESSAGE = 'Closing reading cannot be lower than opening reading.';

/**
 * High-delta warning heuristic (CLIENT-SIDE ONLY — deliberately not a backend
 * rule; the server accepts the reading and the supervisor review catches it):
 * a nozzle's litres sold is flagged "unusually high" when it exceeds
 *
 *   1. 50,000 litres absolute — no single nozzle plausibly dispenses that in
 *      one shift (a fast pump at ~40 L/min flat out for 12 hours is under
 *      30,000 L), or
 *   2. 10× the median of the other nozzles' litres sold — only checked when
 *      at least two other deltas are known and the median is positive.
 *
 * All comparisons are exact decimal-string math (PRD §7.7 "flag abnormal
 * readings"). Amber warning: the attendant may still submit.
 */
const HIGH_DELTA_ABSOLUTE_LITRES = '50000';
const HIGH_DELTA_MEDIAN_FACTOR = 10;

/** Per-nozzle live status, always conveyed as text + colour (PRD §15.1). */
type RowStatus =
  | { kind: 'submitted' } // a closing already exists server-side — locked
  | { kind: 'queued' } // saved on this phone, waiting to sync (Phase 6a)
  | { kind: 'no_opening' } // opening missing; closing cannot be validated
  | { kind: 'empty' }
  | { kind: 'invalid' }
  | { kind: 'scale'; places: number }
  | { kind: 'lower' } // closing < opening — blocked
  | { kind: 'high'; litres: string } // unusually high delta — warn, allowed
  | { kind: 'ok'; litres: string };

/** The outcome of one nozzle's capture attempt in the last submit round. */
interface RowResult {
  ok: boolean;
  message?: string;
  /** Saved to the offline queue rather than the server. */
  queued?: boolean;
}

/** The litres a row would sell, or null while the input isn't a valid figure >= opening. */
function litresFor(assignment: AttendantAssignment, opening: string | undefined, value: string) {
  const v = value.trim();
  if (
    opening == null ||
    !isMeterDecimal(v) ||
    meterFractionDigits(v) > assignment.meter_decimal_places ||
    compareMeterDecimals(v, opening) < 0
  ) {
    return null;
  }
  return subtractMeterDecimals(v, opening);
}

/** Median of exact decimal strings: sort, take the (lower) middle element. */
function medianMeterDecimal(values: string[]): string | null {
  if (values.length === 0) return null;
  const sorted = [...values].sort((a, b) => compareMeterDecimals(a, b));
  return sorted[Math.floor((sorted.length - 1) / 2)] ?? null;
}

/** Whether litres is "unusually high" against the other nozzles' deltas. */
function isHighDelta(litres: string, otherDeltas: string[]): boolean {
  if (compareMeterDecimals(litres, HIGH_DELTA_ABSOLUTE_LITRES) > 0) return true;
  if (otherDeltas.length < 2) return false;
  const median = medianMeterDecimal(otherDeltas);
  if (median == null || compareMeterDecimals(median, '0') <= 0) return false;
  return compareMeterDecimals(litres, multiplyMeterDecimal(median, HIGH_DELTA_MEDIAN_FACTOR)) > 0;
}

function rowStatus(
  assignment: AttendantAssignment,
  reading: AttendantReading | undefined,
  queued: string | undefined,
  value: string,
  otherDeltas: string[],
): RowStatus {
  if (reading?.closing_reading != null) return { kind: 'submitted' };
  if (queued) return { kind: 'queued' };
  if (reading?.opening_reading == null) return { kind: 'no_opening' };
  const v = value.trim();
  if (v === '') return { kind: 'empty' };
  if (!isMeterDecimal(v)) return { kind: 'invalid' };
  if (meterFractionDigits(v) > assignment.meter_decimal_places) {
    return { kind: 'scale', places: assignment.meter_decimal_places };
  }
  if (compareMeterDecimals(v, reading.opening_reading) < 0) return { kind: 'lower' };
  const litres = subtractMeterDecimals(v, reading.opening_reading);
  if (isHighDelta(litres, otherDeltas)) return { kind: 'high', litres };
  return { kind: 'ok', litres };
}

/** Statuses that allow the reading to be submitted. */
function submittable(status: RowStatus): status is { kind: 'ok' | 'high'; litres: string } {
  return status.kind === 'ok' || status.kind === 'high';
}

export default function ClosingReadingsPage() {
  const router = useRouter();
  const qc = useQueryClient();

  const [inputs, setInputs] = useState<Record<string, string>>({});
  const [results, setResults] = useState<Record<string, RowResult>>({});
  const [confirming, setConfirming] = useState(false);
  const [submitSummary, setSubmitSummary] = useState<string | null>(null);

  const snapshot = useAttendantSnapshot();
  const engineState = useSyncEngineState();
  const shiftID = snapshot.data?.shift?.id ?? '';

  // Closings already saved on this phone for this shift (Phase 6a queue):
  // rendered as submitted-but-unsynced, excluded from the submit set.
  const queuedByNozzle = new Map(
    engineState.items
      .filter(
        (i) =>
          i.action_type === 'closing_reading' &&
          i.shift_id === shiftID &&
          (i.sync_status === 'pending' || i.sync_status === 'syncing'),
      )
      .map((i) => [
        (i.payload as { nozzle_id: string }).nozzle_id,
        (i.payload as { reading: string }).reading,
      ]),
  );

  const submit = useMutation({
    mutationFn: async (rows: Array<{ assignment: AttendantAssignment; reading: string }>) => {
      // Capture sequentially and keep every per-nozzle outcome — a partial
      // failure must be reported honestly, not collapsed into one error.
      const outcome: Record<string, RowResult> = {};
      for (const { assignment, reading } of rows) {
        try {
          await api.captureMeterReading(shiftID, {
            nozzle_id: assignment.nozzle_id,
            reading_type: 'closing',
            reading,
          });
          outcome[assignment.nozzle_id] = { ok: true };
        } catch (e) {
          // Connectivity failure → save the reading on this phone (decimal
          // string preserved verbatim) and replay it in order when online.
          if (isOfflineError(e)) {
            await getSyncEngine().enqueue({
              action_type: 'closing_reading',
              shift_id: shiftID,
              payload: {
                nozzle_id: assignment.nozzle_id,
                reading,
                pump_number: assignment.pump_number,
                nozzle_number: assignment.nozzle_number,
              },
              label: `Closing reading ${reading} — pump ${assignment.pump_number} · nozzle ${assignment.nozzle_number}`,
            });
            outcome[assignment.nozzle_id] = { ok: true, queued: true };
            continue;
          }
          outcome[assignment.nozzle_id] = { ok: false, message: captureErrorMessage(e) };
        }
      }
      return outcome;
    },
    onSuccess: async (outcome, rows) => {
      setResults(outcome);
      const allResults = Object.values(outcome);
      const saved = allResults.filter((r) => r.ok).length;
      const queued = allResults.filter((r) => r.queued).length;
      if (saved === rows.length) {
        if (queued > 0) {
          toast.success(
            'Closing readings saved on this phone',
            'They will sync when you are back online.',
          );
        } else {
          toast.success(
            'Closing readings submitted',
            'Your supervisor will now review and verify them.',
          );
        }
        await qc.invalidateQueries({ queryKey: QUERY_KEY });
        router.push('/attendant');
        return;
      }
      setSubmitSummary(
        `Saved ${saved} of ${rows.length} readings. Fix the nozzles marked below and try again.`,
      );
      await qc.invalidateQueries({ queryKey: QUERY_KEY });
    },
    onSettled: () => setConfirming(false),
  });

  if (snapshot.isPending) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-10 rounded-xl" />
        <Skeleton className="h-40 rounded-xl" />
        <Skeleton className="h-40 rounded-xl" />
      </div>
    );
  }
  if (snapshot.showError) {
    return (
      <ErrorState
        title="Couldn't load your shift"
        description={String((snapshot.error as Error).message)}
        onRetry={() => snapshot.refetch()}
      />
    );
  }

  const data = snapshot.data as AttendantCurrentShift;
  if (!data.shift || data.assignments.length === 0) {
    return (
      <div className="flex flex-col gap-4">
        <BackHome />
        <EmptyState
          title="Nothing to close right now"
          description={
            !data.shift
              ? 'You are not on a shift. Closing readings are captured at the end of your shift.'
              : 'No nozzles are assigned to you yet, so there is nothing to close.'
          }
        />
      </div>
    );
  }

  const readingByNozzle = new Map(data.readings.map((r) => [r.nozzle_id, r]));

  // Known litres figures used by the high-delta heuristic: already-submitted
  // nozzles plus every pending nozzle whose current input yields a delta.
  const deltaByNozzle = new Map<string, string>();
  for (const a of data.assignments) {
    const reading = readingByNozzle.get(a.nozzle_id);
    if (reading?.closing_reading != null && reading.opening_reading != null) {
      const litres = subtractMeterDecimals(reading.closing_reading, reading.opening_reading);
      if (!litres.startsWith('-')) deltaByNozzle.set(a.nozzle_id, litres);
      continue;
    }
    // A closing queued on this phone counts as that nozzle's figure too.
    const queued = queuedByNozzle.get(a.nozzle_id);
    const litres = litresFor(a, reading?.opening_reading, queued ?? inputs[a.nozzle_id] ?? '');
    if (litres != null) deltaByNozzle.set(a.nozzle_id, litres);
  }

  const rows = data.assignments.map((a) => {
    const reading = readingByNozzle.get(a.nozzle_id);
    const queued = queuedByNozzle.get(a.nozzle_id);
    const value = inputs[a.nozzle_id] ?? '';
    const otherDeltas = [...deltaByNozzle.entries()]
      .filter(([nozzleID]) => nozzleID !== a.nozzle_id)
      .map(([, litres]) => litres);
    return {
      assignment: a,
      reading,
      queued,
      value,
      status: rowStatus(a, reading, queued, value, otherDeltas),
    };
  });

  const pending = rows.filter((r) => r.status.kind !== 'submitted' && r.status.kind !== 'queued');
  const submittedCount = rows.length - pending.length;
  // PRD §7.7: ALL assigned nozzles must be completed — every pending nozzle
  // needs a submittable figure before anything can be sent.
  const allSubmittable = pending.length > 0 && pending.every((r) => submittable(r.status));

  const queuedCount = rows.filter((r) => r.status.kind === 'queued').length;

  // Done state: every assigned nozzle already has a closing reading.
  if (pending.length === 0 && data.shift.status !== 'open') {
    return <AllSubmitted total={rows.length} queued={queuedCount} closed />;
  }
  if (pending.length === 0) {
    return <AllSubmitted total={rows.length} queued={queuedCount} />;
  }
  if (data.shift.status !== 'open') {
    return (
      <div className="flex flex-col gap-4">
        <BackHome />
        <EmptyState
          title="Your shift is no longer open"
          description="Closing readings can no longer be captured. Talk to your supervisor about the missing nozzles."
        />
      </div>
    );
  }

  const confirmRows = pending.filter((r) => submittable(r.status));
  const totalLitres = confirmRows.reduce(
    (sum, r) => addMeterDecimals(sum, (r.status as { litres: string }).litres),
    '0',
  );

  return (
    <div className="flex flex-col gap-4">
      <BackHome />

      <div>
        <h1 className="text-xl font-semibold leading-tight">Closing readings</h1>
        <p className="text-base text-muted-foreground" role="status">
          {submittedCount} of {rows.length} nozzles submitted. Enter the closing meter on each
          nozzle — litres sold are calculated for you.
        </p>
      </div>

      {submitSummary ? (
        <p className="rounded-md bg-warning/10 px-3 py-2 text-base text-warning" role="alert">
          {submitSummary}
        </p>
      ) : null}

      {/* Per-nozzle capture cards */}
      {rows.map(({ assignment: a, reading, queued, value, status }) => {
        const result = results[a.nozzle_id];
        const inputID = `closing-${a.nozzle_id}`;
        return (
          <Card key={a.assignment_id}>
            <CardHeader className="pb-2">
              <CardTitle className="flex items-center justify-between gap-2 text-base">
                <span className="flex items-center gap-2">
                  <span
                    className="inline-block size-3 rounded-full border border-border"
                    style={{ backgroundColor: a.product_color }}
                    aria-hidden
                  />
                  {a.product_name}
                </span>
                <span className="font-mono text-xs font-normal text-muted-foreground">
                  Pump {a.pump_number} · Nozzle {a.nozzle_number}
                </span>
              </CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-2.5">
              <p className="flex items-center justify-between text-base">
                <span className="text-muted-foreground">Opening reading</span>
                <span className="font-mono font-medium tabular-nums">
                  {reading?.opening_reading ?? 'Not recorded'}
                </span>
              </p>

              {status.kind === 'submitted' && reading?.closing_reading ? (
                <>
                  <p className="flex items-center justify-between text-base">
                    <span className="text-muted-foreground">Closing reading</span>
                    <span className="font-mono text-lg font-semibold tabular-nums">
                      {reading.closing_reading}
                    </span>
                  </p>
                  {reading.opening_reading ? (
                    <p className="flex items-center justify-between text-base">
                      <span className="text-muted-foreground">Litres sold</span>
                      <span className="font-mono font-medium tabular-nums">
                        {subtractMeterDecimals(reading.closing_reading, reading.opening_reading)} L
                      </span>
                    </p>
                  ) : null}
                  <div>
                    <SubmittedBadge status={reading.verification_status} />
                  </div>
                </>
              ) : status.kind === 'queued' && queued ? (
                <>
                  <p className="flex items-center justify-between text-base">
                    <span className="text-muted-foreground">Closing reading</span>
                    <span className="font-mono text-lg font-semibold tabular-nums">{queued}</span>
                  </p>
                  {reading?.opening_reading ? (
                    <p className="flex items-center justify-between text-base">
                      <span className="text-muted-foreground">Litres sold</span>
                      <span className="font-mono font-medium tabular-nums">
                        {subtractMeterDecimals(queued, reading.opening_reading)} L
                      </span>
                    </p>
                  ) : null}
                  <div>
                    <Badge tone="info">Saved on this phone — will sync</Badge>
                  </div>
                </>
              ) : status.kind === 'no_opening' ? (
                <p className="rounded-md bg-warning/10 px-3 py-2 text-sm text-warning" role="alert">
                  No opening reading was recorded for this nozzle, so its closing cannot be
                  validated. Verify the opening reading first.
                </p>
              ) : (
                <>
                  <label htmlFor={inputID} className="text-sm text-muted-foreground">
                    Closing meter reading ({a.meter_decimal_places} decimal
                    {a.meter_decimal_places === 1 ? '' : 's'} max)
                  </label>
                  <Input
                    id={inputID}
                    className="h-14 text-right font-mono text-lg tabular-nums"
                    type="text"
                    inputMode="decimal"
                    autoComplete="off"
                    value={value}
                    disabled={submit.isPending}
                    onChange={(e) => {
                      setInputs((p) => ({ ...p, [a.nozzle_id]: e.target.value }));
                      setSubmitSummary(null);
                    }}
                    aria-describedby={`${inputID}-status`}
                    aria-invalid={
                      status.kind === 'lower' ||
                      status.kind === 'invalid' ||
                      status.kind === 'scale'
                    }
                  />
                  <RowStatusLine id={`${inputID}-status`} status={status} />
                  {result && !result.ok ? (
                    <p
                      className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger"
                      role="alert"
                    >
                      Not saved: {result.message}
                    </p>
                  ) : null}
                </>
              )}
            </CardContent>
          </Card>
        );
      })}

      {/* Confirm-then-submit: one primary action with an explicit confirmation
          step before anything is sent (PRD §15.3). */}
      {confirming ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Confirm your closing readings</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <p className="text-base" role="status">
              You are submitting {confirmRows.length} reading
              {confirmRows.length === 1 ? '' : 's'} totalling{' '}
              <span className="font-mono font-semibold tabular-nums">{totalLitres}</span> litres —
              confirm.
            </p>
            <ul className="flex flex-col gap-2">
              {confirmRows.map((r) => (
                <li
                  key={r.assignment.assignment_id}
                  className="flex items-center justify-between gap-2 text-base"
                >
                  <span>
                    Pump {r.assignment.pump_number} · Nozzle {r.assignment.nozzle_number} (
                    {r.assignment.product_name})
                  </span>
                  <span className="text-right">
                    <span className="block font-mono font-semibold tabular-nums">
                      {r.value.trim()}
                    </span>
                    <span className="block font-mono text-xs tabular-nums text-muted-foreground">
                      {(r.status as { litres: string }).litres} L sold
                    </span>
                  </span>
                </li>
              ))}
            </ul>
            <p className="text-sm text-muted-foreground">
              Submitted readings are locked — only your supervisor can correct them during review.
            </p>
            <Button
              className="h-14 text-lg"
              disabled={submit.isPending}
              onClick={() =>
                submit.mutate(
                  confirmRows.map((r) => ({ assignment: r.assignment, reading: r.value.trim() })),
                )
              }
            >
              {submit.isPending ? <Loader2 className="size-5 animate-spin" aria-hidden /> : null}
              Confirm and submit
            </Button>
            <Button
              variant="outline"
              className="h-12 text-base"
              disabled={submit.isPending}
              onClick={() => setConfirming(false)}
            >
              Go back and edit
            </Button>
          </CardContent>
        </Card>
      ) : (
        <Button
          className="h-14 text-lg"
          disabled={!allSubmittable || submit.isPending}
          onClick={() => setConfirming(true)}
        >
          Submit closing readings
        </Button>
      )}
    </div>
  );
}

/** Maps a capture failure to a plain-language, per-nozzle message. */
function captureErrorMessage(e: unknown): string {
  if (e instanceof SdkError) {
    const body = e.body as { code?: string } | null;
    if (body && body.code === 'closing_already_submitted') {
      return 'A closing reading was already submitted for this nozzle — it is pending supervisor review.';
    }
    if (e.status === 409) return 'A closing reading was already recorded for this nozzle.';
    if (e.message) return e.message;
  }
  return 'Could not save this reading. Check your connection and try again.';
}

/**
 * The read-only badge on an already-submitted nozzle. The attendant has no
 * edit path here — corrections are the supervisor's verify-correct flow
 * (PRD §7.7 submission lock; the server enforces it with 409
 * closing_already_submitted).
 */
function SubmittedBadge({ status }: { status?: string }) {
  switch (status) {
    case 'approved':
      return <Badge tone="success">Approved by supervisor</Badge>;
    case 'corrected':
      return <Badge tone="warning">Corrected by supervisor</Badge>;
    case 'rejected':
      return <Badge tone="danger">Rejected by supervisor</Badge>;
    default:
      return <Badge tone="info">Submitted — pending supervisor review</Badge>;
  }
}

/**
 * The live status under each input — text always carries the meaning, colour
 * only reinforces it (PRD §15.1).
 */
function RowStatusLine({ id, status }: { id: string; status: RowStatus }) {
  switch (status.kind) {
    case 'ok':
      return (
        <p id={id} className="text-base font-medium text-success" role="status">
          Litres sold: {status.litres} L
        </p>
      );
    case 'high':
      return (
        <p id={id} className="text-base font-medium text-warning" role="status">
          Litres sold: {status.litres} L — this looks unusually high. Double-check the meter; you
          can still submit it.
        </p>
      );
    case 'lower':
      return (
        <p id={id} className="text-base font-medium text-danger" role="status">
          {LOWER_BLOCKED_MESSAGE}
        </p>
      );
    case 'scale':
      return (
        <p id={id} className="text-base font-medium text-danger" role="status">
          Too many decimals — this meter records at most {status.places} decimal
          {status.places === 1 ? '' : 's'}.
        </p>
      );
    case 'invalid':
      return (
        <p id={id} className="text-base font-medium text-danger" role="status">
          Enter numbers only, like 1500 or 1500.25.
        </p>
      );
    case 'empty':
      return (
        <p id={id} className="text-base text-muted-foreground" role="status">
          Enter the closing reading shown on the meter.
        </p>
      );
    default:
      return null;
  }
}

/** Every nozzle submitted: confirmation + the path onward to review status. */
function AllSubmitted({
  total,
  queued = 0,
  closed,
}: {
  total: number;
  queued?: number;
  closed?: boolean;
}) {
  return (
    <div className="flex flex-col gap-4">
      <BackHome />
      <Card>
        <CardContent className="flex flex-col items-center gap-3 pt-6 text-center">
          <span className="flex size-12 items-center justify-center rounded-full bg-success/15 text-success">
            <Check className="size-6" aria-hidden />
          </span>
          <p className="text-lg font-semibold" role="status">
            All closing readings are submitted
          </p>
          <p className="text-base text-muted-foreground">
            {total} of {total} nozzles submitted{closed ? ' and the shift is closed' : ''}. Your
            supervisor reviews them next.
          </p>
          {queued > 0 ? (
            <p className="text-sm font-medium text-warning" role="status">
              {queued} reading{queued === 1 ? ' is' : 's are'} saved on this phone and will sync
              when you are back online.
            </p>
          ) : null}
          <Button asChild className="h-14 w-full text-lg">
            <Link href="/attendant/review-status">
              View review status
              <ArrowRight className="size-5" aria-hidden />
            </Link>
          </Button>
          <Button asChild variant="outline" className="h-12 w-full text-base">
            <Link href="/attendant">Back to my shift</Link>
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}

function BackHome() {
  return (
    <Button asChild variant="ghost" className="h-12 w-fit -ml-2 text-base">
      <Link href="/attendant">
        <ArrowLeft className="size-5" aria-hidden />
        My shift
      </Link>
    </Button>
  );
}
