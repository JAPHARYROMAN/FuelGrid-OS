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
import { useT, type Messages } from '@/lib/i18n';
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
  const t = useT();
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
            });
            outcome[assignment.nozzle_id] = { ok: true, queued: true };
            continue;
          }
          outcome[assignment.nozzle_id] = { ok: false, message: captureErrorMessage(e, t) };
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
          toast.success(t.closing.toastQueuedTitle, t.closing.toastQueuedBody);
        } else {
          toast.success(t.closing.toastSubmittedTitle, t.closing.toastSubmittedBody);
        }
        await qc.invalidateQueries({ queryKey: QUERY_KEY });
        router.push('/attendant');
        return;
      }
      setSubmitSummary(t.closing.partialSummary(saved, rows.length));
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
  if (!data.shift || data.assignments.length === 0) {
    return (
      <div className="flex flex-col gap-4">
        <BackHome />
        <EmptyState
          title={t.closing.emptyTitle}
          description={!data.shift ? t.closing.emptyNoShift : t.closing.emptyNoAssignments}
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
        <EmptyState title={t.closing.shiftNotOpenTitle} description={t.closing.shiftNotOpenBody} />
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
        <h1 className="text-xl font-semibold leading-tight">{t.closing.title}</h1>
        <p className="text-base text-muted-foreground" role="status">
          {t.closing.progress(submittedCount, rows.length)}
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
                  {t.common.pumpNozzle(a.pump_number, a.nozzle_number)}
                </span>
              </CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-2.5">
              <p className="flex items-center justify-between text-base">
                <span className="text-muted-foreground">{t.closing.openingReading}</span>
                <span className="font-mono font-medium tabular-nums">
                  {reading?.opening_reading ?? t.closing.notRecorded}
                </span>
              </p>

              {status.kind === 'submitted' && reading?.closing_reading ? (
                <>
                  <p className="flex items-center justify-between text-base">
                    <span className="text-muted-foreground">{t.closing.closingReading}</span>
                    <span className="font-mono text-lg font-semibold tabular-nums">
                      {reading.closing_reading}
                    </span>
                  </p>
                  {reading.opening_reading ? (
                    <p className="flex items-center justify-between text-base">
                      <span className="text-muted-foreground">{t.closing.litresSold}</span>
                      <span className="font-mono font-medium tabular-nums">
                        {t.closing.litresValue(
                          subtractMeterDecimals(reading.closing_reading, reading.opening_reading),
                        )}
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
                    <span className="text-muted-foreground">{t.closing.closingReading}</span>
                    <span className="font-mono text-lg font-semibold tabular-nums">{queued}</span>
                  </p>
                  {reading?.opening_reading ? (
                    <p className="flex items-center justify-between text-base">
                      <span className="text-muted-foreground">{t.closing.litresSold}</span>
                      <span className="font-mono font-medium tabular-nums">
                        {t.closing.litresValue(
                          subtractMeterDecimals(queued, reading.opening_reading),
                        )}
                      </span>
                    </p>
                  ) : null}
                  <div>
                    <Badge tone="info">{t.common.savedOnPhoneBadge}</Badge>
                  </div>
                </>
              ) : status.kind === 'no_opening' ? (
                <p className="rounded-md bg-warning/10 px-3 py-2 text-sm text-warning" role="alert">
                  {t.closing.noOpening}
                </p>
              ) : (
                <>
                  <label htmlFor={inputID} className="text-sm text-muted-foreground">
                    {t.closing.meterLabel(a.meter_decimal_places)}
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
                      {t.closing.notSaved(result.message ?? '')}
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
            <CardTitle className="text-base">{t.closing.confirmTitle}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <p className="text-base" role="status">
              {t.closing.confirmSummaryPrefix(confirmRows.length)}
              <span className="font-mono font-semibold tabular-nums">{totalLitres}</span>
              {t.closing.confirmSummarySuffix}
            </p>
            <ul className="flex flex-col gap-2">
              {confirmRows.map((r) => (
                <li
                  key={r.assignment.assignment_id}
                  className="flex items-center justify-between gap-2 text-base"
                >
                  <span>
                    {t.common.pumpNozzle(r.assignment.pump_number, r.assignment.nozzle_number)} (
                    {r.assignment.product_name})
                  </span>
                  <span className="text-right">
                    <span className="block font-mono font-semibold tabular-nums">
                      {r.value.trim()}
                    </span>
                    <span className="block font-mono text-xs tabular-nums text-muted-foreground">
                      {t.closing.litresSoldShort((r.status as { litres: string }).litres)}
                    </span>
                  </span>
                </li>
              ))}
            </ul>
            <p className="text-sm text-muted-foreground">{t.closing.lockNote}</p>
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
              {t.common.confirmAndSubmit}
            </Button>
            <Button
              variant="outline"
              className="h-12 text-base"
              disabled={submit.isPending}
              onClick={() => setConfirming(false)}
            >
              {t.common.goBackAndEdit}
            </Button>
          </CardContent>
        </Card>
      ) : (
        <Button
          className="h-14 text-lg"
          disabled={!allSubmittable || submit.isPending}
          onClick={() => setConfirming(true)}
        >
          {t.closing.submitButton}
        </Button>
      )}
    </div>
  );
}

/** Maps a capture failure to a plain-language, per-nozzle message. */
function captureErrorMessage(e: unknown, t: Messages): string {
  if (e instanceof SdkError) {
    const body = e.body as { code?: string } | null;
    if (body && body.code === 'closing_already_submitted') {
      return t.closing.errAlreadySubmitted;
    }
    if (e.status === 409) return t.closing.errAlreadyRecorded;
    // Raw server prose — shown verbatim (untranslated) as the honest fallback.
    if (e.message) return e.message;
  }
  return t.closing.errGeneric;
}

/**
 * The read-only badge on an already-submitted nozzle. The attendant has no
 * edit path here — corrections are the supervisor's verify-correct flow
 * (PRD §7.7 submission lock; the server enforces it with 409
 * closing_already_submitted).
 */
function SubmittedBadge({ status }: { status?: string }) {
  const t = useT();
  switch (status) {
    case 'approved':
      return <Badge tone="success">{t.closing.badgeApproved}</Badge>;
    case 'corrected':
      return <Badge tone="warning">{t.closing.badgeCorrected}</Badge>;
    case 'rejected':
      return <Badge tone="danger">{t.closing.badgeRejected}</Badge>;
    default:
      return <Badge tone="info">{t.closing.badgePending}</Badge>;
  }
}

/**
 * The live status under each input — text always carries the meaning, colour
 * only reinforces it (PRD §15.1).
 */
function RowStatusLine({ id, status }: { id: string; status: RowStatus }) {
  const t = useT();
  switch (status.kind) {
    case 'ok':
      return (
        <p id={id} className="text-base font-medium text-success" role="status">
          {t.closing.statusOk(status.litres)}
        </p>
      );
    case 'high':
      return (
        <p id={id} className="text-base font-medium text-warning" role="status">
          {t.closing.statusHigh(status.litres)}
        </p>
      );
    case 'lower':
      return (
        <p id={id} className="text-base font-medium text-danger" role="status">
          {t.closing.lowerBlocked}
        </p>
      );
    case 'scale':
      return (
        <p id={id} className="text-base font-medium text-danger" role="status">
          {t.closing.statusScale(status.places)}
        </p>
      );
    case 'invalid':
      return (
        <p id={id} className="text-base font-medium text-danger" role="status">
          {t.closing.statusInvalid}
        </p>
      );
    case 'empty':
      return (
        <p id={id} className="text-base text-muted-foreground" role="status">
          {t.closing.statusEmpty}
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
  const t = useT();
  return (
    <div className="flex flex-col gap-4">
      <BackHome />
      <Card>
        <CardContent className="flex flex-col items-center gap-3 pt-6 text-center">
          <span className="flex size-12 items-center justify-center rounded-full bg-success/15 text-success">
            <Check className="size-6" aria-hidden />
          </span>
          <p className="text-lg font-semibold" role="status">
            {t.closing.allSubmittedTitle}
          </p>
          <p className="text-base text-muted-foreground">
            {t.closing.allSubmittedBody(total, Boolean(closed))}
          </p>
          {queued > 0 ? (
            <p className="text-sm font-medium text-warning" role="status">
              {t.closing.queuedNote(queued)}
            </p>
          ) : null}
          <Button asChild className="h-14 w-full text-lg">
            <Link href="/attendant/review-status">
              {t.closing.viewReviewStatus}
              <ArrowRight className="size-5" aria-hidden />
            </Link>
          </Button>
          <Button asChild variant="outline" className="h-12 w-full text-base">
            <Link href="/attendant">{t.common.backToMyShift}</Link>
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}

function BackHome() {
  const t = useT();
  return (
    <Button asChild variant="ghost" className="h-12 w-fit -ml-2 text-base">
      <Link href="/attendant">
        <ArrowLeft className="size-5" aria-hidden />
        {t.common.myShift}
      </Link>
    </Button>
  );
}
