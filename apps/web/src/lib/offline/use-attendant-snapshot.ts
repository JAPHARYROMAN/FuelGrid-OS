'use client';

/**
 * The shared current-shift snapshot query for every attendant screen
 * (Mobile Attendant Phase 6a).
 *
 * Same query key + fetcher the screens already used, plus offline support:
 *   - every successful response is persisted (snapshot-cache) so the app
 *     renders the last-known state when it opens offline;
 *   - the cached snapshot seeds the query as ALREADY-STALE initial data, so
 *     React Query refetches immediately whenever the network is available —
 *     on a shared device a different attendant's stale snapshot is replaced
 *     by their own data on first paint-after-fetch;
 *   - screens render data when they have it even if the LAST refetch failed
 *     (use `showStale` for the honest "Showing last synced info" hint).
 */

import { useQuery } from '@tanstack/react-query';

import type { AttendantCurrentShift } from '@fuelgrid/sdk';

import { api } from '@/lib/api';

import { loadSnapshot, saveSnapshot } from './snapshot-cache';

export const ATTENDANT_SHIFT_QUERY_KEY = ['attendant-current-shift'];

export function useAttendantSnapshot(options: { refetchInterval?: number } = {}) {
  const query = useQuery<AttendantCurrentShift>({
    queryKey: ATTENDANT_SHIFT_QUERY_KEY,
    queryFn: async ({ signal }) => {
      const data = await api.attendantCurrentShift(signal);
      saveSnapshot(data);
      return data;
    },
    refetchInterval: options.refetchInterval,
    initialData: () => loadSnapshot()?.data,
    // Date.parse of the cached saved_at marks the seed stale enough to
    // refetch immediately; 0 (epoch) when the timestamp is unreadable.
    initialDataUpdatedAt: () => {
      const cached = loadSnapshot();
      if (!cached) return undefined;
      const t = Date.parse(cached.saved_at);
      return Number.isNaN(t) ? 0 : t;
    },
  });

  return {
    ...query,
    /**
     * Data is on screen but the latest refetch did not succeed — the screen
     * should show the stale-data hint.
     */
    showStale: query.data !== undefined && query.isError,
    /** No data at all AND the fetch failed — the screen shows its error state. */
    showError: query.data === undefined && query.isError,
  };
}
