/**
 * Replay mapping for queued offline actions (Mobile Attendant Phase 6a).
 *
 * Each action type maps the REAL server contract onto a queue outcome —
 * studied from the handlers, not guessed:
 *
 *   check-in / check-out     attendance_handlers.go — idempotent (a repeat
 *                            returns the existing record) → replay-safe;
 *                            409 ("shift is not open" / "you have not checked
 *                            in") is a genuine failure the attendant must see.
 *   assignment confirm       attendance_handlers.go — idempotent → replay-
 *                            safe; 404 means the assignment row was replaced
 *                            (reassignment) → conflict, needs attention.
 *   opening capture          meter_readings_handlers.go — a duplicate is a
 *                            PLAIN 409 (no machine code: "a opening reading
 *                            already exists for this nozzle"); verify against
 *                            a fresh snapshot — same figure → already applied
 *                            (synced), different figure → conflict. 422
 *                            (opening_below_expected, meter scale) → failed
 *                            with the server's message; never retried forever.
 *   closing capture          meter_readings_handlers.go — duplicate → 409
 *                            code closing_already_submitted; verified against
 *                            the snapshot the same way.
 *   collection submission    shift_close_handlers.go — one-per-shift unique →
 *                            plain 409; the SAME 409 status also means "shift
 *                            must be closed", so the refetch decides: a
 *                            cash_submission on the server with a matching
 *                            submitted_total → synced-as-duplicate, a
 *                            different total → conflict, no submission at all
 *                            → failed with the server's message. 422
 *                            variance_reason_required → failed.
 *
 * Cross-cutting:
 *   status 0 (SDK network failure) → offline: stay pending, retry later.
 *   401 → auth: pause the whole queue ("Sign in to finish syncing") — never
 *         discard.
 *   5xx → transient: stay pending, retry on the next trigger.
 *   any other unrecognized 4xx → failed (surfaced, kept, explicit-discard
 *         only). Data is NEVER dropped by the replay engine itself.
 */

import { SdkError, type AttendantCurrentShift } from '@fuelgrid/sdk';

import { compareMeterDecimals, addMeterDecimals, isMeterDecimal } from '@/lib/meter-decimal';

import type {
  CollectionPayload,
  QueuedAction,
  QueueMessageCode,
  QueueMessageParams,
  ReadingPayload,
} from './types';

/** The slice of the SDK client the replay engine needs. */
export interface ReplayApi {
  attendantCurrentShift(signal?: AbortSignal): Promise<AttendantCurrentShift>;
  checkInToShift(
    shiftID: string,
    req?: { device_info?: Record<string, unknown> },
  ): Promise<unknown>;
  checkOutOfShift(shiftID: string): Promise<unknown>;
  confirmNozzleAssignment(shiftID: string, assignmentID: string): Promise<unknown>;
  captureMeterReading(
    shiftID: string,
    req: { nozzle_id: string; reading_type: 'opening' | 'closing'; reading: string },
  ): Promise<unknown>;
  submitCash(
    shiftID: string,
    req: {
      cash_amount: string;
      mobile_money_amount?: string;
      card_amount?: string;
      credit_amount?: string;
      notes?: string;
    },
  ): Promise<unknown>;
}

/**
 * `message` is always the developer/English fallback prose (raw server text
 * for uncoded outcomes). Client-classified outcomes ALSO carry `code` +
 * `params` so the sync sheet can render them in the attendant's language
 * (Phase 6b) — the queue stores the code, not the prose.
 */
export type ReplayOutcome =
  | { kind: 'synced'; note?: string }
  | {
      kind: 'conflict';
      message: string;
      code?: QueueMessageCode;
      params?: QueueMessageParams;
      serverValue?: string;
    }
  | { kind: 'failed'; message: string; code?: QueueMessageCode; params?: QueueMessageParams }
  | { kind: 'offline' }
  | { kind: 'auth' }
  | { kind: 'transient'; message: string; code?: QueueMessageCode };

/** Whether an error is the SDK's transport-failure signal (offline, DNS, …). */
export function isOfflineError(err: unknown): boolean {
  return err instanceof SdkError && err.status === 0;
}

function errorCode(err: SdkError): string | undefined {
  const body = err.body as { code?: unknown } | null;
  return body && typeof body.code === 'string' ? body.code : undefined;
}

/** The cross-cutting mapping for transport/auth/5xx; null = caller decides. */
function commonOutcome(err: unknown): ReplayOutcome | null {
  if (!(err instanceof SdkError)) {
    return { kind: 'transient', message: err instanceof Error ? err.message : String(err) };
  }
  if (err.status === 0) return { kind: 'offline' };
  if (err.status === 401) return { kind: 'auth' };
  if (err.status >= 500) return { kind: 'transient', message: err.message };
  return null;
}

/**
 * Exact decimal equality, tolerant of non-decimal garbage (never throws —
 * a malformed figure simply doesn't match).
 */
function decimalsEqual(a: string, b: string): boolean {
  if (!isMeterDecimal(a) || !isMeterDecimal(b)) return a === b;
  return compareMeterDecimals(a, b) === 0;
}

/** The submitted total of a collection payload, in exact decimal-string math. */
export function collectionTotal(p: CollectionPayload): string {
  return [p.cash_amount, p.mobile_money_amount, p.card_amount, p.credit_amount].reduce(
    (sum, v) => addMeterDecimals(sum, v.trim() === '' ? '0' : v),
    '0',
  );
}

/**
 * Refetch the workflow snapshot to verify whether a 409 means "already
 * applied with the same figure" (synced) or "the server holds something
 * else" (conflict). A failed refetch keeps the action pending (offline /
 * transient) rather than guessing.
 */
async function verifyReadingDuplicate(
  api: ReplayApi,
  action: QueuedAction & { payload: ReadingPayload },
  type: 'opening' | 'closing',
): Promise<ReplayOutcome> {
  let snapshot: AttendantCurrentShift;
  try {
    snapshot = await api.attendantCurrentShift();
  } catch (err) {
    return (
      commonOutcome(err) ?? {
        kind: 'transient',
        message: 'could not verify with the server',
        code: 'verify_unavailable',
      }
    );
  }
  const reading = snapshot.readings.find((r) => r.nozzle_id === action.payload.nozzle_id);
  const serverFigure = type === 'opening' ? reading?.opening_reading : reading?.closing_reading;
  if (serverFigure != null && decimalsEqual(serverFigure, action.payload.reading)) {
    return { kind: 'synced', note: 'already recorded on the server' };
  }
  return {
    kind: 'conflict',
    message:
      serverFigure != null
        ? `The server already has a different ${type} reading (${serverFigure}) for this nozzle. Your figure is kept here — show it to your supervisor.`
        : `The server reported this ${type} reading as already submitted but it is not visible on your shift. Your figure is kept here — show it to your supervisor.`,
    code: 'reading_conflict',
    params: { reading_type: type, server_value: serverFigure ?? undefined },
    serverValue: serverFigure ?? undefined,
  };
}

async function verifyCollectionDuplicate(
  api: ReplayApi,
  action: QueuedAction & { payload: CollectionPayload },
  originalMessage: string,
): Promise<ReplayOutcome> {
  let snapshot: AttendantCurrentShift;
  try {
    snapshot = await api.attendantCurrentShift();
  } catch (err) {
    return (
      commonOutcome(err) ?? {
        kind: 'transient',
        message: 'could not verify with the server',
        code: 'verify_unavailable',
      }
    );
  }
  const existing = snapshot.cash_submission;
  if (!existing) {
    // The 409 was NOT the one-per-shift duplicate (e.g. "shift must be
    // closed before cash is submitted") — a real failure to surface.
    return { kind: 'failed', message: originalMessage };
  }
  if (decimalsEqual(existing.submitted_total, collectionTotal(action.payload))) {
    return { kind: 'synced', note: 'collections were already submitted with the same total' };
  }
  return {
    kind: 'conflict',
    message: `Collections were already submitted for this shift with a different total (${existing.submitted_total}). Your amounts are kept here — show them to your supervisor.`,
    code: 'collection_conflict',
    params: { server_value: existing.submitted_total },
    serverValue: existing.submitted_total,
  };
}

/** Replay one queued action against the live API and classify the outcome. */
export async function replayAction(api: ReplayApi, action: QueuedAction): Promise<ReplayOutcome> {
  switch (action.action_type) {
    case 'check_in': {
      try {
        await api.checkInToShift(action.shift_id, action.payload);
        return { kind: 'synced' };
      } catch (err) {
        const common = commonOutcome(err);
        if (common) return common;
        // Idempotent server-side; any 4xx ("shift is not open", 403 not on
        // the roster) is a genuine failure the attendant must see.
        return { kind: 'failed', message: (err as SdkError).message };
      }
    }
    case 'check_out': {
      try {
        await api.checkOutOfShift(action.shift_id);
        return { kind: 'synced' };
      } catch (err) {
        const common = commonOutcome(err);
        if (common) return common;
        return { kind: 'failed', message: (err as SdkError).message };
      }
    }
    case 'confirm_assignment': {
      try {
        await api.confirmNozzleAssignment(action.shift_id, action.payload.assignment_id);
        return { kind: 'synced' };
      } catch (err) {
        const common = commonOutcome(err);
        if (common) return common;
        const sdkErr = err as SdkError;
        if (sdkErr.status === 404) {
          // The assignment row was replaced (the only reassignment path is
          // delete + recreate) — the new assignment needs a fresh look.
          return {
            kind: 'conflict',
            message:
              'Your nozzle assignment changed while you were offline. Check your assignment and confirm it again.',
            code: 'assignment_changed',
          };
        }
        return { kind: 'failed', message: sdkErr.message };
      }
    }
    case 'opening_reading':
    case 'closing_reading': {
      const type = action.action_type === 'opening_reading' ? 'opening' : 'closing';
      try {
        await api.captureMeterReading(action.shift_id, {
          nozzle_id: action.payload.nozzle_id,
          reading_type: type,
          reading: action.payload.reading,
        });
        return { kind: 'synced' };
      } catch (err) {
        const common = commonOutcome(err);
        if (common) return common;
        const sdkErr = err as SdkError;
        if (sdkErr.status === 409) {
          // Duplicate (opening: plain 409; closing: 409
          // closing_already_submitted) — verify whether it was THIS figure.
          return verifyReadingDuplicate(
            api,
            action as QueuedAction & { payload: ReadingPayload },
            type,
          );
        }
        // 422 opening_below_expected / meter-scale and any other 4xx: the
        // attendant must fix it — surface the server's own message.
        if (errorCode(sdkErr) === 'opening_below_expected') {
          return {
            kind: 'failed',
            message:
              "Reading is lower than the previous shift's approved closing. Call your supervisor.",
            code: 'opening_below_expected',
          };
        }
        return { kind: 'failed', message: sdkErr.message };
      }
    }
    case 'collection': {
      try {
        await api.submitCash(action.shift_id, {
          cash_amount: action.payload.cash_amount,
          mobile_money_amount: action.payload.mobile_money_amount,
          card_amount: action.payload.card_amount,
          credit_amount: action.payload.credit_amount,
          ...(action.payload.notes ? { notes: action.payload.notes } : {}),
        });
        return { kind: 'synced' };
      } catch (err) {
        const common = commonOutcome(err);
        if (common) return common;
        const sdkErr = err as SdkError;
        if (sdkErr.status === 409) {
          return verifyCollectionDuplicate(
            api,
            action as QueuedAction & { payload: CollectionPayload },
            sdkErr.message,
          );
        }
        // 422 variance_reason_required and any other 4xx → failed, re-editable.
        return { kind: 'failed', message: sdkErr.message };
      }
    }
  }
}
