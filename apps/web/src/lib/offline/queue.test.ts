import { beforeEach, describe, expect, it, vi } from 'vitest';

import { SdkError, type AttendantCurrentShift } from '@fuelgrid/sdk';

import { SyncEngine } from './engine';
import { collectionTotal, replayAction, type ReplayApi } from './replay';
import { WebStorageQueueStore } from './store';
import type { CollectionPayload, QueuedAction } from './types';

/**
 * Unit tests for the offline action queue (Mobile Attendant Phase 6a):
 * enqueue/replay order, the per-action replay mapping against the REAL
 * server contracts, the no-data-loss invariants (PRD §14.3), and the
 * explicit-discard-only rule.
 */

const offlineError = () => new SdkError('network request failed: fetch failed', 0, null);
const authError = () => new SdkError('authentication required', 401, null);

function makeApi(overrides: Partial<ReplayApi> = {}): ReplayApi {
  return {
    attendantCurrentShift: vi.fn().mockResolvedValue(emptySnapshot()),
    checkInToShift: vi.fn().mockResolvedValue({}),
    checkOutOfShift: vi.fn().mockResolvedValue({}),
    confirmNozzleAssignment: vi.fn().mockResolvedValue({}),
    captureMeterReading: vi.fn().mockResolvedValue({}),
    submitCash: vi.fn().mockResolvedValue({}),
    // The SDK returns the parsed Incident on both a 201 create and a 200
    // dedupe-key replay; the default resolves to a stub incident.
    reportIncident: vi.fn().mockResolvedValue({ id: 'inc-1', dedupe_key: 'k1' }),
    ...overrides,
  };
}

function emptySnapshot(extra: Partial<AttendantCurrentShift> = {}): AttendantCurrentShift {
  return {
    status: 'on_shift',
    next_action: 'working',
    user_message: '',
    attendance: { status: 'checked_in' },
    assignments: [],
    readings: [],
    expected_openings_available: false,
    ...extra,
  };
}

function makeEngine(api: ReplayApi) {
  const store = new WebStorageQueueStore(localStorage);
  const engine = new SyncEngine(store, api, true);
  return { engine, store };
}

/** Index access that throws (instead of returning undefined) on a missing row. */
function itemAt(engine: SyncEngine, i: number): QueuedAction {
  const item = engine.getState().items[i];
  if (!item) throw new Error(`no queued action at index ${i}`);
  return item;
}

beforeEach(() => {
  localStorage.clear();
});

// -----------------------------------------------------------------------------
// Replay mapping per action type
// -----------------------------------------------------------------------------

describe('replayAction mapping', () => {
  const baseAction = (over: Partial<QueuedAction>): QueuedAction =>
    ({
      local_action_id: 'a1',
      seq: 1,
      shift_id: 'shift-1',
      label: 'test',
      created_at_local: new Date().toISOString(),
      retry_count: 0,
      sync_status: 'pending',
      action_type: 'check_in',
      payload: {},
      ...over,
    }) as QueuedAction;

  it('check-in success → synced (idempotent server-side)', async () => {
    const api = makeApi();
    const outcome = await replayAction(
      api,
      baseAction({ action_type: 'check_in', payload: { device_info: { app: 'attendant-pwa' } } }),
    );
    expect(outcome.kind).toBe('synced');
    expect(api.checkInToShift).toHaveBeenCalledWith('shift-1', {
      device_info: { app: 'attendant-pwa' },
    });
  });

  it('check-in 409 (shift not open) → failed with the server message', async () => {
    const api = makeApi({
      checkInToShift: vi.fn().mockRejectedValue(new SdkError('shift is not open', 409, null)),
    });
    const outcome = await replayAction(api, baseAction({ action_type: 'check_in', payload: {} }));
    expect(outcome).toEqual({ kind: 'failed', message: 'shift is not open' });
  });

  it('offline transport failure → offline (action stays pending)', async () => {
    const api = makeApi({ checkInToShift: vi.fn().mockRejectedValue(offlineError()) });
    const outcome = await replayAction(api, baseAction({ action_type: 'check_in', payload: {} }));
    expect(outcome).toEqual({ kind: 'offline' });
  });

  it('401 during replay → auth (queue pauses, nothing discarded)', async () => {
    const api = makeApi({ checkInToShift: vi.fn().mockRejectedValue(authError()) });
    const outcome = await replayAction(api, baseAction({ action_type: 'check_in', payload: {} }));
    expect(outcome).toEqual({ kind: 'auth' });
  });

  it('5xx → transient (stays pending for a later retry)', async () => {
    const api = makeApi({
      checkInToShift: vi.fn().mockRejectedValue(new SdkError('internal error', 500, null)),
    });
    const outcome = await replayAction(api, baseAction({ action_type: 'check_in', payload: {} }));
    expect(outcome.kind).toBe('transient');
  });

  it('assignment confirm 404 (reassigned) → conflict', async () => {
    const api = makeApi({
      confirmNozzleAssignment: vi
        .fn()
        .mockRejectedValue(new SdkError('assignment not found', 404, null)),
    });
    const outcome = await replayAction(
      api,
      baseAction({ action_type: 'confirm_assignment', payload: { assignment_id: 'as-1' } }),
    );
    expect(outcome.kind).toBe('conflict');
  });

  it('closing 409 closing_already_submitted + MATCHING payload → synced (already applied)', async () => {
    const api = makeApi({
      captureMeterReading: vi.fn().mockRejectedValue(
        new SdkError('a closing reading was already submitted for this nozzle', 409, {
          code: 'closing_already_submitted',
        }),
      ),
      attendantCurrentShift: vi.fn().mockResolvedValue(
        emptySnapshot({
          readings: [
            { nozzle_id: 'noz-1', opening_reading: '100.000', closing_reading: '1500.250' },
          ],
        }),
      ),
    });
    const outcome = await replayAction(
      api,
      baseAction({
        action_type: 'closing_reading',
        payload: { nozzle_id: 'noz-1', reading: '1500.250' },
      }),
    );
    expect(outcome.kind).toBe('synced');
  });

  it('closing 409 + numerically equal but differently scaled figure → synced', async () => {
    const api = makeApi({
      captureMeterReading: vi
        .fn()
        .mockRejectedValue(new SdkError('dup', 409, { code: 'closing_already_submitted' })),
      attendantCurrentShift: vi.fn().mockResolvedValue(
        emptySnapshot({
          readings: [{ nozzle_id: 'noz-1', closing_reading: '1500.000' }],
        }),
      ),
    });
    const outcome = await replayAction(
      api,
      baseAction({
        action_type: 'closing_reading',
        payload: { nozzle_id: 'noz-1', reading: '1500' },
      }),
    );
    expect(outcome.kind).toBe('synced');
  });

  it('closing 409 + MISMATCHED server figure → conflict carrying the server value', async () => {
    const api = makeApi({
      captureMeterReading: vi
        .fn()
        .mockRejectedValue(new SdkError('dup', 409, { code: 'closing_already_submitted' })),
      attendantCurrentShift: vi.fn().mockResolvedValue(
        emptySnapshot({
          readings: [{ nozzle_id: 'noz-1', closing_reading: '1600.000' }],
        }),
      ),
    });
    const outcome = await replayAction(
      api,
      baseAction({
        action_type: 'closing_reading',
        payload: { nozzle_id: 'noz-1', reading: '1500.250' },
      }),
    );
    expect(outcome.kind).toBe('conflict');
    expect(outcome).toMatchObject({ serverValue: '1600.000' });
  });

  it('opening duplicate (plain 409, no machine code) verifies against the snapshot too', async () => {
    const api = makeApi({
      captureMeterReading: vi
        .fn()
        .mockRejectedValue(
          new SdkError('a opening reading already exists for this nozzle', 409, null),
        ),
      attendantCurrentShift: vi.fn().mockResolvedValue(
        emptySnapshot({
          readings: [{ nozzle_id: 'noz-1', opening_reading: '250.500' }],
        }),
      ),
    });
    const matching = await replayAction(
      api,
      baseAction({
        action_type: 'opening_reading',
        payload: { nozzle_id: 'noz-1', reading: '250.500' },
      }),
    );
    expect(matching.kind).toBe('synced');

    const mismatched = await replayAction(
      api,
      baseAction({
        action_type: 'opening_reading',
        payload: { nozzle_id: 'noz-1', reading: '251.000' },
      }),
    );
    expect(mismatched.kind).toBe('conflict');
  });

  it('opening 422 opening_below_expected → failed with the call-your-supervisor message', async () => {
    const api = makeApi({
      captureMeterReading: vi.fn().mockRejectedValue(
        new SdkError("opening reading is below the previous shift's approved closing", 422, {
          code: 'opening_below_expected',
          expected_opening_reading: '300.000',
        }),
      ),
    });
    const outcome = await replayAction(
      api,
      baseAction({
        action_type: 'opening_reading',
        payload: { nozzle_id: 'noz-1', reading: '100.000' },
      }),
    );
    expect(outcome.kind).toBe('failed');
    expect(outcome).toMatchObject({ message: expect.stringContaining('supervisor') });
  });

  it('reading 422 (meter scale) → failed with the server message, never retried as pending', async () => {
    const api = makeApi({
      captureMeterReading: vi
        .fn()
        .mockRejectedValue(
          new SdkError("reading has more decimals than the nozzle's meter precision", 422, null),
        ),
    });
    const outcome = await replayAction(
      api,
      baseAction({
        action_type: 'closing_reading',
        payload: { nozzle_id: 'noz-1', reading: '1.2345' },
      }),
    );
    expect(outcome).toEqual({
      kind: 'failed',
      message: "reading has more decimals than the nozzle's meter precision",
    });
  });

  it('verification refetch failing offline keeps the action pending (offline outcome)', async () => {
    const api = makeApi({
      captureMeterReading: vi
        .fn()
        .mockRejectedValue(new SdkError('dup', 409, { code: 'closing_already_submitted' })),
      attendantCurrentShift: vi.fn().mockRejectedValue(offlineError()),
    });
    const outcome = await replayAction(
      api,
      baseAction({
        action_type: 'closing_reading',
        payload: { nozzle_id: 'noz-1', reading: '1500.250' },
      }),
    );
    expect(outcome).toEqual({ kind: 'offline' });
  });

  const collectionPayload: CollectionPayload = {
    cash_amount: '250000.50',
    mobile_money_amount: '1000',
    card_amount: '0',
    credit_amount: '0',
    notes: 'till float included',
  };

  it('collection 409 + existing submission with MATCHING total → synced as duplicate', async () => {
    const api = makeApi({
      submitCash: vi
        .fn()
        .mockRejectedValue(
          new SdkError('cash has already been submitted for this shift', 409, null),
        ),
      attendantCurrentShift: vi.fn().mockResolvedValue(
        emptySnapshot({
          cash_submission: {
            submitted_total: '251000.50',
          } as AttendantCurrentShift['cash_submission'],
        }),
      ),
    });
    const outcome = await replayAction(
      api,
      baseAction({ action_type: 'collection', payload: collectionPayload }),
    );
    expect(outcome.kind).toBe('synced');
  });

  it('collection 409 + existing submission with DIFFERENT total → conflict', async () => {
    const api = makeApi({
      submitCash: vi
        .fn()
        .mockRejectedValue(
          new SdkError('cash has already been submitted for this shift', 409, null),
        ),
      attendantCurrentShift: vi.fn().mockResolvedValue(
        emptySnapshot({
          cash_submission: {
            submitted_total: '999.99',
          } as AttendantCurrentShift['cash_submission'],
        }),
      ),
    });
    const outcome = await replayAction(
      api,
      baseAction({ action_type: 'collection', payload: collectionPayload }),
    );
    expect(outcome.kind).toBe('conflict');
    expect(outcome).toMatchObject({ serverValue: '999.99' });
  });

  it('collection 409 with NO submission on the server (e.g. shift not closed) → failed', async () => {
    const api = makeApi({
      submitCash: vi
        .fn()
        .mockRejectedValue(
          new SdkError('shift must be closed before cash is submitted', 409, null),
        ),
      attendantCurrentShift: vi.fn().mockResolvedValue(emptySnapshot()),
    });
    const outcome = await replayAction(
      api,
      baseAction({ action_type: 'collection', payload: collectionPayload }),
    );
    expect(outcome).toEqual({
      kind: 'failed',
      message: 'shift must be closed before cash is submitted',
    });
  });

  it('collection 422 variance_reason_required → failed', async () => {
    const api = makeApi({
      submitCash: vi.fn().mockRejectedValue(
        new SdkError('your submitted total does not match the expected amount', 422, {
          code: 'variance_reason_required',
        }),
      ),
    });
    const outcome = await replayAction(
      api,
      baseAction({ action_type: 'collection', payload: collectionPayload }),
    );
    expect(outcome.kind).toBe('failed');
  });

  const issuePayload = {
    type: 'pump' as const,
    description: 'pump 1 will not dispense',
    severity: 'high',
    dedupe_key: 'dedupe-1',
  };

  it('issue report success (first create, 201) → synced and sends the dedupe_key, never a station', async () => {
    const reportIncident = vi.fn().mockResolvedValue({ id: 'inc-1', dedupe_key: 'dedupe-1' });
    const api = makeApi({ reportIncident });
    const outcome = await replayAction(
      api,
      baseAction({ action_type: 'report_issue', payload: issuePayload }),
    );
    expect(outcome.kind).toBe('synced');
    const sent = reportIncident.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(sent).toMatchObject({
      type: 'pump',
      description: 'pump 1 will not dispense',
      severity: 'high',
      dedupe_key: 'dedupe-1',
    });
    // Station is derived server-side — the queue NEVER asserts one.
    expect(sent).not.toHaveProperty('station_id');
  });

  it('issue report dedupe_key REPLAY (200, original incident) → synced/applied, NOT a duplicate conflict', async () => {
    // A 200 replay surfaces through the SDK as a normal resolved Incident — the
    // SAME success path as a 201. The honest mapping is synced/applied.
    const reportIncident = vi
      .fn()
      .mockResolvedValue({ id: 'inc-ORIGINAL', dedupe_key: 'dedupe-1' });
    const api = makeApi({ reportIncident });
    const outcome = await replayAction(
      api,
      baseAction({ action_type: 'report_issue', payload: issuePayload }),
    );
    expect(outcome.kind).toBe('synced');
    expect(outcome).not.toMatchObject({ kind: 'conflict' });
  });

  it('issue report 409 no_active_shift → failed with the code-mapped message (translated at render)', async () => {
    const api = makeApi({
      reportIncident: vi
        .fn()
        .mockRejectedValue(
          new SdkError('you have no active shift', 409, { code: 'no_active_shift' }),
        ),
    });
    const outcome = await replayAction(
      api,
      baseAction({ action_type: 'report_issue', payload: issuePayload }),
    );
    expect(outcome.kind).toBe('failed');
    expect(outcome).toMatchObject({ code: 'no_active_shift' });
  });

  it('issue report 422 validation → failed with issue_invalid (re-editable, never silently dropped)', async () => {
    const api = makeApi({
      reportIncident: vi
        .fn()
        .mockRejectedValue(new SdkError('description is required', 422, { error: 'bad request' })),
    });
    const outcome = await replayAction(
      api,
      baseAction({ action_type: 'report_issue', payload: issuePayload }),
    );
    expect(outcome.kind).toBe('failed');
    expect(outcome).toMatchObject({ code: 'issue_invalid' });
  });

  it('issue report offline transport → offline (stays pending, replays later)', async () => {
    const api = makeApi({ reportIncident: vi.fn().mockRejectedValue(offlineError()) });
    const outcome = await replayAction(
      api,
      baseAction({ action_type: 'report_issue', payload: issuePayload }),
    );
    expect(outcome).toEqual({ kind: 'offline' });
  });

  it('issue report 401 → auth (queue pauses, nothing discarded)', async () => {
    const api = makeApi({ reportIncident: vi.fn().mockRejectedValue(authError()) });
    const outcome = await replayAction(
      api,
      baseAction({ action_type: 'report_issue', payload: issuePayload }),
    );
    expect(outcome).toEqual({ kind: 'auth' });
  });

  it('collectionTotal sums tenders with exact decimal-string math', () => {
    expect(collectionTotal(collectionPayload)).toBe('251000.5');
    expect(
      collectionTotal({
        cash_amount: '0.1',
        mobile_money_amount: '0.2',
        card_amount: '',
        credit_amount: '0',
      }),
    ).toBe('0.3');
  });
});

// -----------------------------------------------------------------------------
// Engine: ordering, blocking, pause, no-data-loss, explicit discard
// -----------------------------------------------------------------------------

describe('SyncEngine', () => {
  it('replays strictly in enqueue order per shift', async () => {
    const calls: string[] = [];
    const api = makeApi({
      checkInToShift: vi.fn().mockImplementation(async () => {
        calls.push('check_in');
        return {};
      }),
      captureMeterReading: vi.fn().mockImplementation(async (_s, req) => {
        calls.push(`${req.reading_type}:${req.nozzle_id}`);
        return {};
      }),
      submitCash: vi.fn().mockImplementation(async () => {
        calls.push('collection');
        return {};
      }),
    });
    const { engine } = makeEngine(api);
    // Enqueue while "offline" so nothing replays mid-test.
    engine.setOnline(false);
    await engine.enqueue({ action_type: 'check_in', shift_id: 's1', payload: {}, label: 'in' });
    await engine.enqueue({
      action_type: 'opening_reading',
      shift_id: 's1',
      payload: { nozzle_id: 'n1', reading: '10.000' },
      label: 'open n1',
    });
    await engine.enqueue({
      action_type: 'closing_reading',
      shift_id: 's1',
      payload: { nozzle_id: 'n1', reading: '20.000' },
      label: 'close n1',
    });
    await engine.enqueue({
      action_type: 'collection',
      shift_id: 's1',
      payload: {
        cash_amount: '100',
        mobile_money_amount: '0',
        card_amount: '0',
        credit_amount: '0',
      },
      label: 'collect',
    });

    await engine.setOnline(true);

    expect(calls).toEqual(['check_in', 'opening:n1', 'closing:n1', 'collection']);
    expect(engine.getState().items.every((i) => i.sync_status === 'synced')).toBe(true);
  });

  it('stops the shift chain at the first conflict and keeps later actions pending', async () => {
    const api = makeApi({
      captureMeterReading: vi
        .fn()
        .mockRejectedValue(new SdkError('dup', 409, { code: 'closing_already_submitted' })),
      attendantCurrentShift: vi
        .fn()
        .mockResolvedValue(
          emptySnapshot({ readings: [{ nozzle_id: 'n1', closing_reading: '999.000' }] }),
        ),
    });
    const { engine } = makeEngine(api);
    engine.setOnline(false);
    await engine.enqueue({
      action_type: 'closing_reading',
      shift_id: 's1',
      payload: { nozzle_id: 'n1', reading: '20.000' },
      label: 'close n1',
    });
    await engine.enqueue({
      action_type: 'collection',
      shift_id: 's1',
      payload: {
        cash_amount: '100',
        mobile_money_amount: '0',
        card_amount: '0',
        credit_amount: '0',
      },
      label: 'collect',
    });

    await engine.setOnline(true);

    const closing = itemAt(engine, 0);
    const collection = itemAt(engine, 1);
    expect(closing.sync_status).toBe('conflict');
    // The dependent collection was NOT replayed over an unresolved conflict.
    expect(collection.sync_status).toBe('pending');
    expect(api.submitCash).not.toHaveBeenCalled();
    // The conflicting payload is fully preserved (no data loss).
    expect(closing.payload).toEqual({ nozzle_id: 'n1', reading: '20.000' });
    expect(closing.server_value).toBe('999.000');
  });

  it('a 401 pauses the queue (auth_required) without discarding anything', async () => {
    const api = makeApi({ checkInToShift: vi.fn().mockRejectedValue(authError()) });
    const { engine } = makeEngine(api);
    engine.setOnline(false);
    await engine.enqueue({ action_type: 'check_in', shift_id: 's1', payload: {}, label: 'in' });
    await engine.setOnline(true);

    expect(engine.getState().phase).toBe('auth_required');
    expect(itemAt(engine, 0).sync_status).toBe('pending');
  });

  it('an offline failure mid-run keeps the action pending and flips the engine offline', async () => {
    const api = makeApi({ checkInToShift: vi.fn().mockRejectedValue(offlineError()) });
    const { engine } = makeEngine(api);
    engine.setOnline(false);
    await engine.enqueue({ action_type: 'check_in', shift_id: 's1', payload: {}, label: 'in' });
    await engine.setOnline(true);

    const item = itemAt(engine, 0);
    expect(item.sync_status).toBe('pending');
    expect(item.retry_count).toBe(1);
    expect(engine.getState().online).toBe(false);
  });

  it('a failed action blocks its shift until retried, then syncs', async () => {
    const capture = vi
      .fn()
      .mockRejectedValueOnce(new SdkError('scale', 422, null))
      .mockResolvedValue({});
    const api = makeApi({ captureMeterReading: capture });
    const { engine } = makeEngine(api);
    engine.setOnline(false);
    await engine.enqueue({
      action_type: 'closing_reading',
      shift_id: 's1',
      payload: { nozzle_id: 'n1', reading: '20.0001' },
      label: 'close n1',
    });
    await engine.setOnline(true);
    expect(itemAt(engine, 0).sync_status).toBe('failed');
    expect(itemAt(engine, 0).error_message).toBe('scale');

    await engine.retry(itemAt(engine, 0).local_action_id);
    expect(itemAt(engine, 0).sync_status).toBe('synced');
  });

  it('discard removes ONLY failed/conflict rows and only via the explicit call', async () => {
    const api = makeApi({
      checkInToShift: vi.fn().mockRejectedValue(new SdkError('shift is not open', 409, null)),
    });
    const { engine, store } = makeEngine(api);
    engine.setOnline(false);
    await engine.enqueue({ action_type: 'check_in', shift_id: 's1', payload: {}, label: 'in' });
    await engine.enqueue({ action_type: 'check_out', shift_id: 's2', payload: {}, label: 'out' });

    const pendingIn = itemAt(engine, 0);
    const pendingOut = itemAt(engine, 1);
    // Discarding a pending action is refused — data is never silently dropped.
    await engine.discard(pendingIn.local_action_id);
    expect((await store.all()).length).toBe(2);

    await engine.setOnline(true);
    const failed = engine
      .getState()
      .items.find((i) => i.local_action_id === pendingIn.local_action_id);
    expect(failed?.sync_status).toBe('failed');

    await engine.discard(pendingIn.local_action_id);
    const remaining = await store.all();
    expect(remaining.map((r) => r.local_action_id)).not.toContain(pendingIn.local_action_id);
    // The other shift's action is untouched by the discard.
    expect(remaining.some((r) => r.local_action_id === pendingOut.local_action_id)).toBe(true);
  });

  it('preserves decimal strings verbatim through persist + reload (no float reserialization)', async () => {
    const api = makeApi();
    const { engine, store } = makeEngine(api);
    engine.setOnline(false);
    await engine.enqueue({
      action_type: 'closing_reading',
      shift_id: 's1',
      payload: { nozzle_id: 'n1', reading: '1500.250' },
      label: 'close',
    });
    await engine.enqueue({
      action_type: 'collection',
      shift_id: 's1',
      payload: {
        cash_amount: '250000.50',
        mobile_money_amount: '0.10',
        card_amount: '0',
        credit_amount: '0',
      },
      label: 'collect',
    });

    // A fresh engine over the same storage (app restart) sees identical strings.
    const reloaded = await store.all();
    const payloads = reloaded.map((r) => r.payload as Record<string, string>);
    expect(payloads).toHaveLength(2);
    expect(payloads[0]?.reading).toBe('1500.250');
    expect(payloads[1]?.cash_amount).toBe('250000.50');
    expect(payloads[1]?.mobile_money_amount).toBe('0.10');
  });

  it('init resumes interrupted (syncing) rows as pending and prunes synced rows', async () => {
    const api = makeApi();
    const store = new WebStorageQueueStore(localStorage);
    const stamp = new Date().toISOString();
    await store.put({
      local_action_id: 'interrupted',
      seq: 1,
      shift_id: 's1',
      label: 'in',
      created_at_local: stamp,
      retry_count: 0,
      sync_status: 'syncing',
      action_type: 'check_in',
      payload: {},
    });
    await store.put({
      local_action_id: 'done',
      seq: 2,
      shift_id: 's1',
      label: 'out',
      created_at_local: stamp,
      retry_count: 0,
      sync_status: 'synced',
      action_type: 'check_out',
      payload: {},
    });

    const engine = new SyncEngine(store, api, true);
    await engine.init();

    const ids = (await store.all()).map((r) => r.local_action_id);
    expect(ids).toContain('interrupted');
    expect(ids).not.toContain('done');
    // …and the resumed row replayed to synced on the init sync.
    expect(
      engine.getState().items.find((i) => i.local_action_id === 'interrupted')?.sync_status,
    ).toBe('synced');
  });
});
