'use client';

import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

interface TenantState {
  /** The currently selected station. null = "all stations in tenant". */
  activeStationID: string | null;
  /** The currently selected region. null = "all regions". */
  activeRegionID: string | null;
  /** The currently selected company. null = "all companies in tenant". */
  activeCompanyID: string | null;

  setActiveStation: (id: string | null) => void;
  setActiveRegion: (id: string | null) => void;
  setActiveCompany: (id: string | null) => void;
  reset: () => void;
}

export const useTenantStore = create<TenantState>()(
  persist(
    (set) => ({
      activeStationID: null,
      activeRegionID: null,
      activeCompanyID: null,

      setActiveStation: (id) => set({ activeStationID: id }),
      setActiveRegion: (id) => set({ activeRegionID: id }),
      setActiveCompany: (id) => set({ activeCompanyID: id }),

      reset: () => set({ activeStationID: null, activeRegionID: null, activeCompanyID: null }),
    }),
    {
      name: 'fuelgrid.tenant-context',
      storage: createJSONStorage(() => localStorage),
    },
  ),
);
