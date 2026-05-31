'use client';

import { useQuery } from '@tanstack/react-query';

import type { MePermissions } from '@fuelgrid/sdk';

import { api } from '@/lib/api';
import { useAuthStore } from '@/stores/auth-store';

/**
 * usePermissions fetches the actor's permission set from the API and
 * keeps it cached for 60 seconds. Underpins usePermission(code).
 */
export function usePermissions() {
  const authed = useAuthStore((s) => s.authed);

  return useQuery<MePermissions>({
    queryKey: ['me', 'permissions'],
    queryFn: ({ signal }) => api.mePermissions(signal),
    staleTime: 60 * 1000,
    enabled: authed,
  });
}

interface UsePermissionOptions {
  /**
   * When the permission is station_scoped, supply the station the
   * action targets. Without it, station-scoped permissions return
   * false unless the actor has tenant-wide reach.
   */
  stationID?: string | null;
}

/**
 * usePermission mirrors the backend's policy.Can() logic in the
 * browser so the UI can hide or disable controls a user can't use.
 * **Frontend permission checks are UX hints only** — the backend is
 * authoritative (per CONTRIBUTING.md).
 *
 * Returns null while the query is loading so callers can render a
 * disabled / skeleton state instead of flicker-toggling visibility.
 */
export function usePermission(code: string, opts: UsePermissionOptions = {}): boolean | null {
  const { data, isLoading } = usePermissions();

  if (isLoading || !data) return null;

  const perm = data.permissions.find((p) => p.code === code);
  if (!perm) return false;

  if (!perm.station_scoped) return true;

  // Station-scoped permission — caller MUST supply a station id for a
  // meaningful answer. Treat missing station as "no" the same way the
  // backend does.
  if (!opts.stationID) return false;

  if (data.tenant_wide) return true;

  return Boolean(data.station_ids?.includes(opts.stationID));
}
