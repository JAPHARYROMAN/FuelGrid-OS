/**
 * Last-known current-shift snapshot cache (Mobile Attendant Phase 6a).
 *
 * Persists the most recent successful GET /api/v1/attendant/current-shift
 * response so the attendant home (and the other attendant screens) can render
 * meaningfully when the app opens offline — clearly marked as stale by the
 * shell's offline strip. The payload is exactly what the attendant already
 * sees on screen (their own shift; no extra financial data), and no session
 * token is ever stored (it lives in the httpOnly cookie). localStorage is
 * sufficient: the snapshot is small and the queue (IndexedDB) carries the
 * durable writes.
 */

import type { AttendantCurrentShift } from '@fuelgrid/sdk';

const KEY = 'fg.attendant.snapshot';

export interface CachedSnapshot {
  data: AttendantCurrentShift;
  /** ISO timestamp (device clock) of the successful fetch. */
  saved_at: string;
}

export function saveSnapshot(data: AttendantCurrentShift): void {
  try {
    localStorage.setItem(KEY, JSON.stringify({ data, saved_at: new Date().toISOString() }));
  } catch {
    // Quota/private-mode failures degrade to "no offline snapshot" only.
  }
}

export function loadSnapshot(): CachedSnapshot | null {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<CachedSnapshot>;
    if (!parsed || typeof parsed !== 'object' || !parsed.data || !parsed.saved_at) return null;
    return parsed as CachedSnapshot;
  } catch {
    return null;
  }
}

export function clearSnapshot(): void {
  try {
    localStorage.removeItem(KEY);
  } catch {
    // ignore
  }
}
