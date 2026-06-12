'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertTriangle, ArrowLeft, Check, Loader2, Megaphone } from 'lucide-react';

import {
  SdkError,
  type AttendantAssignment,
  type AttendantCurrentShift,
  type ExpectedOpeningReadingList,
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
import { usePermission } from '@/hooks/use-permissions';
import { toast } from '@/lib/toast';
import {
  compareMeterDecimals,
  isMeterDecimal,
  meterFractionDigits,
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
 * The message mirrored from the server's 422 `opening_below_expected` —
 * meters only move forward, so a lower-than-handover figure is blocked
 * client-side with the same explanation the server would give (PRD §7.5).
 */
const LOWER_BLOCKED_MESSAGE =
  "Reading is lower than the previous shift's approved closing. Call your supervisor.";

/** Per-nozzle live verification status, always conveyed as text + colour. */
type RowStatus =
  | { kind: 'recorded' } // an opening already exists server-side
  | { kind: 'queued' } // saved on this phone, waiting to sync (Phase 6a)
  | { kind: 'empty' }
  | { kind: 'invalid' }
  | { kind: 'scale'; places: number }
  | { kind: 'no_expected' } // no previous reading — first shift for this nozzle
  | { kind: 'matched' }
  | { kind: 'higher'; difference: string }
  | { kind: 'lower' };

/** The outcome of one nozzle's capture attempt in the last submit round. */
interface RowResult {
  ok: boolean;
  message?: string;
  /** Saved to the offline queue rather than the server. */
  queued?: boolean;
}

function rowStatus(
  assignment: AttendantAssignment,
  expected: string | undefined,
  recorded: string | undefined,
  queued: string | undefined,
  value: string,
): RowStatus {
  if (recorded) return { kind: 'recorded' };
  if (queued) return { kind: 'queued' };
  const v = value.trim();
  if (v === '') return { kind: 'empty' };
  if (!isMeterDecimal(v)) return { kind: 'invalid' };
  if (meterFractionDigits(v) > assignment.meter_decimal_places) {
    return { kind: 'scale', places: assignment.meter_decimal_places };
  }
  if (expected == null) return { kind: 'no_expected' };
  const cmp = compareMeterDecimals(v, expected);
  if (cmp === 0) return { kind: 'matched' };
  if (cmp > 0) return { kind: 'higher', difference: subtractMeterDecimals(v, expected) };
  return { kind: 'lower' };
}

/** Statuses that allow the reading to be submitted. */
function submittable(status: RowStatus): boolean {
  return status.kind === 'matched' || status.kind === 'higher' || status.kind === 'no_expected';
}

export default function OpeningReadingsPage() {
  const router = useRouter();
  const qc = useQueryClient();

  // Free-typed values per nozzle; absent key = "not edited yet" (prefilled
  // with the expected reading).
  const [inputs, setInputs] = useState<Record<string, string>>({});
  const [results, setResults] = useState<Record<string, RowResult>>({});
  const [confirming, setConfirming] = useState(false);
  const [submitSummary, setSubmitSummary] = useState<string | null>(null);
  const [reported, setReported] = useState(false);

  const snapshot = useAttendantSnapshot();
  const engineState = useSyncEngineState();
  const shiftID = snapshot.data?.shift?.id ?? '';
  const stationID = snapshot.data?.station?.id;

  const expected = useQuery<ExpectedOpeningReadingList>({
    queryKey: ['attendant-expected-openings', shiftID],
    queryFn: ({ signal }) => api.listExpectedOpeningReadings(shiftID, signal),
    enabled: shiftID !== '',
  });

  // Openings already saved on this phone for this shift (Phase 6a queue):
  // rendered as captured-but-unsynced, excluded from the submit set.
  const queuedByNozzle = new Map(
    engineState.items
      .filter(
        (i) =>
          i.action_type === 'opening_reading' &&
          i.shift_id === shiftID &&
          (i.sync_status === 'pending' || i.sync_status === 'syncing'),
      )
      .map((i) => [
        (i.payload as { nozzle_id: string }).nozzle_id,
        (i.payload as { reading: string }).reading,
      ]),
  );

  // Whether THIS user may open an incident (incidents.manage is a
  // supervisor-tier permission today; attendants normally fall back to the
  // call-your-supervisor message). UX hint only — the backend re-checks.
  const canReportIncident = usePermission('incidents.manage', { stationID });

  const submit = useMutation({
    mutationFn: async (rows: Array<{ assignment: AttendantAssignment; reading: string }>) => {
      // Capture sequentially and keep every per-nozzle outcome — a partial
      // failure must be reported honestly, not collapsed into one error.
      const outcome: Record<string, RowResult> = {};
      for (const { assignment, reading } of rows) {
        try {
          await api.captureMeterReading(shiftID, {
            nozzle_id: assignment.nozzle_id,
            reading_type: 'opening',
            reading,
          });
          outcome[assignment.nozzle_id] = { ok: true };
        } catch (e) {
          // Connectivity failure → save the reading on this phone (decimal
          // string preserved verbatim) and replay it in order when online.
          if (isOfflineError(e)) {
            await getSyncEngine().enqueue({
              action_type: 'opening_reading',
              shift_id: shiftID,
              payload: {
                nozzle_id: assignment.nozzle_id,
                reading,
                pump_number: assignment.pump_number,
                nozzle_number: assignment.nozzle_number,
              },
              label: `Opening reading ${reading} — pump ${assignment.pump_number} · nozzle ${assignment.nozzle_number}`,
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
      const results = Object.values(outcome);
      const saved = results.filter((r) => r.ok).length;
      const queued = results.filter((r) => r.queued).length;
      if (saved === rows.length) {
        if (queued > 0) {
          toast.success(
            'Opening readings saved on this phone',
            'They will sync when you are back online.',
          );
        } else {
          toast.success('Opening readings saved', 'All your nozzles are verified.');
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

  const reportIssue = useMutation({
    mutationFn: (description: string) =>
      api.createIncident({
        station_id: stationID ?? '',
        type: 'variance',
        severity: 'high',
        related_entity_type: 'shift',
        related_entity_id: shiftID,
        description,
      }),
    onSuccess: () => setReported(true),
    onError: (e) =>
      toast.error(
        'Could not report the issue',
        e instanceof SdkError ? e.message : 'Try again or call your supervisor.',
      ),
  });

  if (snapshot.isPending || (shiftID !== '' && expected.isPending)) {
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
  // Offline, the expected figures may be unreachable — degrade honestly (the
  // rows say "expected unavailable"; the server re-validates every reading at
  // sync). A failure while ONLINE is a real problem and keeps the error state.
  if (expected.isError && engineState.online) {
    return (
      <ErrorState
        title="Couldn't load the expected readings"
        description={String((expected.error as Error).message)}
        onRetry={() => expected.refetch()}
      />
    );
  }

  const data = snapshot.data as AttendantCurrentShift;
  if (!data.shift || data.shift.status !== 'open' || data.assignments.length === 0) {
    return (
      <div className="flex flex-col gap-4">
        <BackHome />
        <EmptyState
          title="Nothing to verify right now"
          description={
            !data.shift
              ? 'You are not on a shift. Opening readings are captured at the start of your shift.'
              : data.shift.status !== 'open'
                ? 'Your shift is no longer open, so opening readings can no longer be captured.'
                : 'No nozzles are assigned to you yet. Your supervisor assigns them after you check in.'
          }
        />
      </div>
    );
  }

  const expectedByAssignment = new Map(
    (expected.data?.items ?? []).map((e) => [e.assignment_id, e]),
  );
  const recordedByNozzle = new Map(
    data.readings
      .filter((r) => r.opening_reading != null)
      .map((r) => [r.nozzle_id, r.opening_reading as string]),
  );

  const rows = data.assignments.map((a) => {
    const exp = expectedByAssignment.get(a.assignment_id)?.expected_opening_reading;
    const recorded = recordedByNozzle.get(a.nozzle_id);
    const queued = queuedByNozzle.get(a.nozzle_id);
    const value = inputs[a.nozzle_id] ?? exp ?? '';
    return {
      assignment: a,
      expected: exp,
      recorded,
      queued,
      value,
      status: rowStatus(a, exp, recorded, queued, value),
    };
  });

  const pending = rows.filter((r) => !r.recorded && !r.queued);
  const verifiedCount = rows.length - pending.length;
  const lowerRows = pending.filter((r) => r.status.kind === 'lower');
  const allSubmittable = pending.length > 0 && pending.every((r) => submittable(r.status));

  // Done state: every assigned nozzle already has an opening reading.
  if (pending.length === 0) {
    return (
      <div className="flex flex-col gap-4">
        <BackHome />
        <Card>
          <CardContent className="flex flex-col items-center gap-3 pt-6 text-center">
            <span className="flex size-12 items-center justify-center rounded-full bg-success/15 text-success">
              <Check className="size-6" aria-hidden />
            </span>
            <p className="text-lg font-semibold" role="status">
              All opening readings are recorded
            </p>
            <p className="text-base text-muted-foreground">
              {rows.length} of {rows.length} nozzles verified. You are set for this shift.
            </p>
            {rows.some((r) => r.queued) ? (
              <p className="text-sm font-medium text-warning" role="status">
                {rows.filter((r) => r.queued).length} reading
                {rows.filter((r) => r.queued).length === 1 ? ' is' : 's are'} saved on this phone
                and will sync when you are back online.
              </p>
            ) : null}
            <Button asChild className="h-14 w-full text-lg">
              <Link href="/attendant">Back to my shift</Link>
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  const confirmRows = pending.filter((r) => submittable(r.status));

  return (
    <div className="flex flex-col gap-4">
      <BackHome />

      <div>
        <h1 className="text-xl font-semibold leading-tight">Opening readings</h1>
        <p className="text-base text-muted-foreground" role="status">
          {verifiedCount} of {rows.length} nozzles verified. Compare each meter with the expected
          figure and save.
        </p>
      </div>

      {submitSummary ? (
        <p className="rounded-md bg-warning/10 px-3 py-2 text-base text-warning" role="alert">
          {submitSummary}
        </p>
      ) : null}

      {/* Per-nozzle capture cards */}
      {rows.map(({ assignment: a, expected: exp, recorded, queued, value, status }) => {
        const result = results[a.nozzle_id];
        const inputID = `opening-${a.nozzle_id}`;
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
                <span className="text-muted-foreground">Expected opening</span>
                <span className="font-mono font-medium tabular-nums">
                  {exp ?? 'No previous reading'}
                </span>
              </p>

              {recorded ? (
                <div className="flex items-center justify-between gap-2">
                  <span className="font-mono text-lg font-semibold tabular-nums">{recorded}</span>
                  <Badge tone="success">Recorded</Badge>
                </div>
              ) : queued ? (
                <div className="flex items-center justify-between gap-2">
                  <span className="font-mono text-lg font-semibold tabular-nums">{queued}</span>
                  <Badge tone="info">Saved on this phone — will sync</Badge>
                </div>
              ) : (
                <>
                  <label htmlFor={inputID} className="text-sm text-muted-foreground">
                    Meter reading ({a.meter_decimal_places} decimal
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

      {/* Blocked-low panel: mirrors the server's 422 and offers the
          supervisor path (incident when permitted, phone otherwise). */}
      {lowerRows.length > 0 ? (
        <div
          className="flex flex-col gap-3 rounded-xl border border-danger/40 bg-danger/10 p-4"
          role="alert"
        >
          <p className="flex items-start gap-2 text-base font-medium text-danger">
            <AlertTriangle className="mt-0.5 size-5 shrink-0" aria-hidden />
            {LOWER_BLOCKED_MESSAGE}
          </p>
          {reported ? (
            <p className="text-base text-danger" role="status">
              Issue reported. Your supervisor has been notified.
            </p>
          ) : canReportIncident ? (
            <Button
              variant="outline"
              className="h-12 text-base"
              disabled={reportIssue.isPending}
              onClick={() =>
                reportIssue.mutate(
                  lowerRows
                    .map(
                      (r) =>
                        `Opening reading mismatch at pump ${r.assignment.pump_number} nozzle ${r.assignment.nozzle_number} (${r.assignment.product_name}): entered ${r.value.trim()}, expected ${r.expected ?? 'n/a'}.`,
                    )
                    .join(' '),
                )
              }
            >
              {reportIssue.isPending ? (
                <Loader2 className="size-5 animate-spin" aria-hidden />
              ) : (
                <Megaphone className="size-5" aria-hidden />
              )}
              Report issue to supervisor
            </Button>
          ) : (
            <p className="text-sm text-danger/90">
              You cannot file the report from here yet — call your supervisor to resolve this before
              the shift can continue.
            </p>
          )}
        </div>
      ) : null}

      {/* Confirm-then-save: one primary action, with an explicit confirmation
          step before anything is submitted (PRD §15.3). */}
      {confirming ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Confirm your readings</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
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
                  <span className="font-mono font-semibold tabular-nums">{r.value.trim()}</span>
                </li>
              ))}
            </ul>
            <p className="text-sm text-muted-foreground">
              Saved readings are locked once your shift opens — corrections need your supervisor.
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
              Confirm and save
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
          Save opening readings
        </Button>
      )}
    </div>
  );
}

/** Maps a capture failure to a plain-language, per-nozzle message. */
function captureErrorMessage(e: unknown): string {
  if (e instanceof SdkError) {
    const body = e.body as { code?: string } | null;
    if (body && body.code === 'opening_below_expected') return LOWER_BLOCKED_MESSAGE;
    if (e.status === 409) return 'An opening reading was already recorded for this nozzle.';
    if (e.message) return e.message;
  }
  return 'Could not save this reading. Check your connection and try again.';
}

/**
 * The live match status under each input — text always carries the meaning,
 * colour only reinforces it (PRD §15.1).
 */
function RowStatusLine({ id, status }: { id: string; status: RowStatus }) {
  switch (status.kind) {
    case 'matched':
      return (
        <p id={id} className="text-base font-medium text-success" role="status">
          Matched — same as the expected opening.
        </p>
      );
    case 'higher':
      return (
        <p id={id} className="text-base font-medium text-warning" role="status">
          Higher than expected by {status.difference}. You can save it, but tell your supervisor if
          this looks wrong.
        </p>
      );
    case 'lower':
      return (
        <p id={id} className="text-base font-medium text-danger" role="status">
          Lower than expected. {LOWER_BLOCKED_MESSAGE}
        </p>
      );
    case 'no_expected':
      return (
        <p id={id} className="text-base text-muted-foreground" role="status">
          No previous reading for this nozzle — enter the meter as you see it.
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
          Enter the reading shown on the meter.
        </p>
      );
    default:
      return null;
  }
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
