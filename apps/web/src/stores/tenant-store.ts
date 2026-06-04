'use client';

import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

/** The kind of enterprise node the active scope points at (Feature 13.1). */
export type ScopeType = 'tenant' | 'company' | 'region' | 'group' | 'station';

/**
 * The active enterprise reporting scope chosen via the scope-switcher. null
 * means tenant-wide ("All stations"). It is a read-time lens only: it narrows
 * which scope the chain view is filtered to, never widening access (scoped
 * reads still enforce station access server-side).
 */
export interface ActiveScope {
  type: ScopeType;
  /** null for a tenant scope (covers everything). */
  id: string | null;
  /** Human label for the switcher trigger. */
  label: string;
}

interface TenantState {
  /** The currently selected station. null = "all stations in tenant". */
  activeStationID: string | null;
  /** The currently selected region. null = "all regions". */
  activeRegionID: string | null;
  /** The currently selected company. null = "all companies in tenant". */
  activeCompanyID: string | null;
  /** The active enterprise scope chosen via the scope-switcher. null = tenant-wide. */
  activeScope: ActiveScope | null;

  setActiveStation: (id: string | null) => void;
  setActiveRegion: (id: string | null) => void;
  setActiveCompany: (id: string | null) => void;
  setActiveScope: (scope: ActiveScope | null) => void;
  reset: () => void;
}

export const useTenantStore = create<TenantState>()(
  persist(
    (set) => ({
      activeStationID: null,
      activeRegionID: null,
      activeCompanyID: null,
      activeScope: null,

      setActiveStation: (id) => set({ activeStationID: id }),
      setActiveRegion: (id) => set({ activeRegionID: id }),
      setActiveCompany: (id) => set({ activeCompanyID: id }),
      setActiveScope: (scope) => set({ activeScope: scope }),

      reset: () =>
        set({
          activeStationID: null,
          activeRegionID: null,
          activeCompanyID: null,
          activeScope: null,
        }),
    }),
    {
      name: 'fuelgrid.tenant-context',
      storage: createJSONStorage(() => localStorage),
    },
  ),
);
