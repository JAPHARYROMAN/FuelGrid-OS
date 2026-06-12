/**
 * Durable storage for the offline action queue (Mobile Attendant Phase 6a).
 *
 * Primary backend is IndexedDB (survives reloads and PWA restarts; no extra
 * dependency — the wrapper below is the handful of promisified calls we
 * need). When IndexedDB is unavailable (some private modes, jsdom tests) we
 * fall back to localStorage so the queue still works — capacity is smaller
 * but the action records are tiny JSON.
 *
 * No session token is ever stored here (it lives in the httpOnly cookie);
 * payloads contain only what the attendant themselves typed.
 */

import type { QueuedAction } from './types';

export interface QueueStore {
  all(): Promise<QueuedAction[]>;
  put(action: QueuedAction): Promise<void>;
  remove(localActionID: string): Promise<void>;
}

// ----------------------------------------------------------------------------
// IndexedDB implementation
// ----------------------------------------------------------------------------

const DB_NAME = 'fg-attendant-offline';
const DB_VERSION = 1;
const STORE_NAME = 'actions';

function requestToPromise<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error ?? new Error('IndexedDB request failed'));
  });
}

function openDatabase(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(STORE_NAME)) {
        db.createObjectStore(STORE_NAME, { keyPath: 'local_action_id' });
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error ?? new Error('IndexedDB open failed'));
    req.onblocked = () => reject(new Error('IndexedDB open blocked'));
  });
}

export class IdbQueueStore implements QueueStore {
  private db: Promise<IDBDatabase> | null = null;

  private database(): Promise<IDBDatabase> {
    this.db ??= openDatabase();
    return this.db;
  }

  async all(): Promise<QueuedAction[]> {
    const db = await this.database();
    const tx = db.transaction(STORE_NAME, 'readonly');
    const rows = await requestToPromise(tx.objectStore(STORE_NAME).getAll());
    return (rows as QueuedAction[]).sort((a, b) => a.seq - b.seq);
  }

  async put(action: QueuedAction): Promise<void> {
    const db = await this.database();
    const tx = db.transaction(STORE_NAME, 'readwrite');
    await requestToPromise(tx.objectStore(STORE_NAME).put(action));
  }

  async remove(localActionID: string): Promise<void> {
    const db = await this.database();
    const tx = db.transaction(STORE_NAME, 'readwrite');
    await requestToPromise(tx.objectStore(STORE_NAME).delete(localActionID));
  }
}

// ----------------------------------------------------------------------------
// localStorage fallback
// ----------------------------------------------------------------------------

const LS_KEY = 'fg.attendant.offline-queue';

export class WebStorageQueueStore implements QueueStore {
  constructor(private readonly storage: Storage) {}

  private read(): QueuedAction[] {
    try {
      const raw = this.storage.getItem(LS_KEY);
      if (!raw) return [];
      const parsed = JSON.parse(raw) as unknown;
      return Array.isArray(parsed) ? (parsed as QueuedAction[]) : [];
    } catch {
      return [];
    }
  }

  private write(actions: QueuedAction[]): void {
    this.storage.setItem(LS_KEY, JSON.stringify(actions));
  }

  async all(): Promise<QueuedAction[]> {
    return [...this.read()].sort((a, b) => a.seq - b.seq);
  }

  async put(action: QueuedAction): Promise<void> {
    const rows = this.read();
    const i = rows.findIndex((r) => r.local_action_id === action.local_action_id);
    if (i === -1) rows.push(action);
    else rows[i] = action;
    this.write(rows);
  }

  async remove(localActionID: string): Promise<void> {
    this.write(this.read().filter((r) => r.local_action_id !== localActionID));
  }
}

/** The best available durable store for this environment. */
export function createQueueStore(): QueueStore {
  if (typeof indexedDB !== 'undefined') return new IdbQueueStore();
  return new WebStorageQueueStore(localStorage);
}
