'use client';

/**
 * The attendant offline sync engine (Mobile Attendant Phase 6a).
 *
 * One module-level engine owns the durable queue: pages enqueue actions when
 * a mutation fails for connectivity reasons, the engine replays them — in
 * strict capture order per shift — when the connection returns (online
 * event), when the app opens, or when the attendant taps "Sync now".
 *
 * Ordering invariant: a shift's actions form a chain (check-in → openings →
 * closings → collection). When an action does not reach `synced`, the rest of
 * that shift's chain is NOT attempted in that run: a conflict or failure
 * pauses the chain until the attendant resolves it (retry or explicit
 * discard) — replaying later actions over an unresolved earlier one could
 * submit figures on the wrong basis.
 *
 * Data-loss invariants (PRD §14.3):
 *   - failed/conflict actions are kept with their full local payload;
 *   - the ONLY removal paths are a successful sync or the attendant's
 *     explicit discard (which the UI double-confirms);
 *   - a 401 pauses the queue ("Sign in to finish syncing") — nothing is
 *     dropped on auth expiry.
 */

import { useSyncExternalStore } from 'react';

import { api } from '@/lib/api';

import { replayAction, type ReplayApi } from './replay';
import { createQueueStore, type QueueStore } from './store';
import type { EnqueueInput, QueuedAction } from './types';

export type EnginePhase = 'idle' | 'syncing' | 'auth_required';

export interface SyncEngineState {
  phase: EnginePhase;
  online: boolean;
  /** All known actions, in capture (seq) order — including synced ones from this session. */
  items: QueuedAction[];
  /** ISO timestamp of the last fully clean sync (everything synced). */
  lastSyncedAt: string | null;
}

/** The chip-level summary derived from the engine state (PRD §9.3 / §9.7). */
export type SyncStatusSummary =
  | { kind: 'offline'; waiting: number }
  | { kind: 'syncing'; waiting: number }
  | { kind: 'auth_required'; waiting: number }
  | { kind: 'conflict'; waiting: number }
  | { kind: 'failed'; waiting: number }
  | { kind: 'waiting'; waiting: number }
  | { kind: 'synced' }
  | { kind: 'online' };

export function deriveSyncSummary(state: SyncEngineState): SyncStatusSummary {
  const waiting = state.items.filter(
    (i) => i.sync_status === 'pending' || i.sync_status === 'syncing',
  ).length;
  const unresolved = state.items.filter((i) => i.sync_status !== 'synced').length;
  if (state.phase === 'syncing') return { kind: 'syncing', waiting };
  if (!state.online) return { kind: 'offline', waiting: unresolved };
  if (state.phase === 'auth_required') return { kind: 'auth_required', waiting: unresolved };
  if (state.items.some((i) => i.sync_status === 'conflict')) {
    return { kind: 'conflict', waiting: unresolved };
  }
  if (state.items.some((i) => i.sync_status === 'failed')) {
    return { kind: 'failed', waiting: unresolved };
  }
  if (waiting > 0) return { kind: 'waiting', waiting };
  if (state.items.some((i) => i.sync_status === 'synced')) return { kind: 'synced' };
  return { kind: 'online' };
}

function newLocalActionID(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  // Older WebViews: random fallback — uniqueness only matters device-locally.
  return `local-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

export class SyncEngine {
  private state: SyncEngineState;
  private readonly listeners = new Set<() => void>();
  private initialized = false;
  private syncQueued = false;

  constructor(
    private readonly store: QueueStore,
    private readonly replayApi: ReplayApi,
    online: boolean = typeof navigator === 'undefined' ? true : navigator.onLine,
  ) {
    this.state = { phase: 'idle', online, items: [], lastSyncedAt: null };
  }

  // ----- subscription (useSyncExternalStore contract) -----

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getState = (): SyncEngineState => this.state;

  private setState(patch: Partial<SyncEngineState>): void {
    this.state = { ...this.state, ...patch };
    for (const l of this.listeners) l();
  }

  private async refreshItems(): Promise<void> {
    this.setState({ items: await this.store.all() });
  }

  // ----- lifecycle -----

  /**
   * Load persisted actions (pruning previous sessions' synced rows) and kick
   * a sync. Safe to call more than once.
   */
  async init(): Promise<void> {
    if (this.initialized) return;
    this.initialized = true;
    const items = await this.store.all();
    for (const item of items) {
      // A replay interrupted mid-flight (app killed) resumes as pending.
      if (item.sync_status === 'syncing') {
        await this.store.put({ ...item, sync_status: 'pending' });
      }
      // Synced rows from an earlier session are done — drop them.
      if (item.sync_status === 'synced') {
        await this.store.remove(item.local_action_id);
      }
    }
    await this.refreshItems();
    await this.sync();
  }

  /** Returns the triggered sync's promise so callers (and tests) can await it. */
  setOnline(online: boolean): Promise<void> {
    const wasOffline = !this.state.online;
    this.setState({ online });
    if (online && wasOffline) return this.sync();
    return Promise.resolve();
  }

  // ----- queue operations -----

  async enqueue(input: EnqueueInput): Promise<QueuedAction> {
    const maxSeq = this.state.items.reduce((m, i) => Math.max(m, i.seq), 0);
    const action = {
      ...input,
      local_action_id: newLocalActionID(),
      seq: maxSeq + 1,
      created_at_local: new Date().toISOString(),
      retry_count: 0,
      sync_status: 'pending',
    } as QueuedAction;
    await this.store.put(action);
    await this.refreshItems();
    // If we're actually online (the failure was a blip), try immediately.
    if (this.state.online) void this.sync();
    return action;
  }

  /** Re-arm a failed/conflict action and run the queue again. */
  async retry(localActionID: string): Promise<void> {
    const item = this.state.items.find((i) => i.local_action_id === localActionID);
    if (!item || item.sync_status === 'synced' || item.sync_status === 'syncing') return;
    await this.store.put({
      ...item,
      sync_status: 'pending',
      error_message: undefined,
      error_code: undefined,
      error_params: undefined,
    });
    if (this.state.phase === 'auth_required') this.setState({ phase: 'idle' });
    await this.refreshItems();
    await this.sync();
  }

  /**
   * Explicitly drop one action — the ONLY way queued data is ever discarded,
   * and only for rows the attendant can see failed/conflicted. The UI
   * confirms before calling this.
   */
  async discard(localActionID: string): Promise<void> {
    const item = this.state.items.find((i) => i.local_action_id === localActionID);
    if (!item) return;
    if (item.sync_status !== 'failed' && item.sync_status !== 'conflict') return;
    await this.store.remove(localActionID);
    await this.refreshItems();
    // The discarded row may have been blocking the rest of its shift's chain.
    await this.sync();
  }

  /** Manual "Sync now". */
  async syncNow(): Promise<void> {
    if (this.state.phase === 'auth_required') this.setState({ phase: 'idle' });
    await this.sync();
  }

  // ----- replay loop -----

  async sync(): Promise<void> {
    if (this.state.phase === 'syncing') {
      // Coalesce: run once more after the active pass for actions enqueued
      // mid-flight.
      this.syncQueued = true;
      return;
    }
    if (!this.state.online || this.state.phase === 'auth_required') return;

    const pending = this.state.items.some((i) => i.sync_status === 'pending');
    if (!pending) return;

    this.setState({ phase: 'syncing' });
    let nextPhase: EnginePhase = 'idle';

    try {
      // Strict capture order; an unresolved (failed/conflict) action blocks
      // the REST of its shift's chain.
      const items = await this.store.all();
      const blockedShifts = new Set<string>();
      for (const item of items) {
        if (item.sync_status === 'failed' || item.sync_status === 'conflict') {
          blockedShifts.add(item.shift_id);
        }
      }

      for (const item of items) {
        if (item.sync_status !== 'pending') continue;
        if (blockedShifts.has(item.shift_id)) continue;

        await this.store.put({ ...item, sync_status: 'syncing' });
        await this.refreshItems();

        const outcome = await replayAction(this.replayApi, item);

        if (outcome.kind === 'synced') {
          await this.store.put({
            ...item,
            sync_status: 'synced',
            error_message: undefined,
            error_code: undefined,
            error_params: undefined,
          });
          continue;
        }
        if (outcome.kind === 'conflict') {
          // The CODE (+ params) is what the sync sheet renders — translated
          // at display time; the message is the untranslated fallback.
          await this.store.put({
            ...item,
            sync_status: 'conflict',
            error_message: outcome.message,
            error_code: outcome.code,
            error_params: outcome.params,
            server_value: outcome.serverValue,
          });
          blockedShifts.add(item.shift_id);
          continue;
        }
        if (outcome.kind === 'failed') {
          await this.store.put({
            ...item,
            sync_status: 'failed',
            error_message: outcome.message,
            error_code: outcome.code,
            error_params: outcome.params,
          });
          blockedShifts.add(item.shift_id);
          continue;
        }
        // offline / auth / transient: stay pending and stop the whole run.
        await this.store.put({
          ...item,
          sync_status: 'pending',
          retry_count: item.retry_count + 1,
          error_message: outcome.kind === 'transient' ? outcome.message : undefined,
          error_code: outcome.kind === 'transient' ? outcome.code : undefined,
          error_params: undefined,
        });
        if (outcome.kind === 'auth') nextPhase = 'auth_required';
        if (outcome.kind === 'offline') this.setState({ online: false });
        break;
      }
    } finally {
      await this.refreshItems();
      const allDone =
        this.state.items.length > 0 && this.state.items.every((i) => i.sync_status === 'synced');
      this.setState({
        phase: nextPhase,
        lastSyncedAt: allDone ? new Date().toISOString() : this.state.lastSyncedAt,
      });
    }

    if (this.syncQueued) {
      this.syncQueued = false;
      void this.sync();
    }
  }
}

// ----------------------------------------------------------------------------
// Singleton + React binding
// ----------------------------------------------------------------------------

let engine: SyncEngine | null = null;

export function getSyncEngine(): SyncEngine {
  engine ??= new SyncEngine(createQueueStore(), api);
  return engine;
}

/** Test hook: drop the singleton so each test starts clean. */
export function resetSyncEngineForTests(): void {
  engine = null;
}

const SERVER_STATE: SyncEngineState = {
  phase: 'idle',
  online: true,
  items: [],
  lastSyncedAt: null,
};

/** Subscribe a component to the engine state. */
export function useSyncEngineState(): SyncEngineState {
  return useSyncExternalStore(
    (cb) => getSyncEngine().subscribe(cb),
    () => getSyncEngine().getState(),
    () => SERVER_STATE,
  );
}
