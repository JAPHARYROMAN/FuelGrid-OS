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

export type PermissionCheckMode = 'target' | 'held';

export interface UsePermissionOptions {
  /**
   * When the permission is station_scoped, supply the station the
   * action targets. In "target" mode, missing station-scoped resources
   * return false, matching policy.Can.
   */
  stationID?: string | null;
  /**
   * "target" mirrors policy.Can for an action against a specific resource.
   * "held" mirrors requirePermissionHeld for tenant-scoped list/document
   * endpoints that only need the actor to hold the permission somewhere.
   */
  mode?: PermissionCheckMode;
}

export function canUsePermission(
  data: MePermissions,
  code: string,
  opts: UsePermissionOptions = {},
): boolean {
  const perm = data.permissions.find((p) => p.code === code);
  if (!perm) return false;

  if (!perm.station_scoped) return true;

  if (opts.mode === 'held') return true;

  // Station-scoped permission — caller MUST supply a station id for a
  // meaningful answer. Treat missing station as "no" the same way the
  // backend's policy.Can does.
  if (!opts.stationID) return false;

  if (data.tenant_wide) return true;

  return Boolean(data.station_ids?.includes(opts.stationID));
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

  return canUsePermission(data, code, opts);
}
