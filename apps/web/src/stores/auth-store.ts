'use client';

import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

interface AuthState {
  token: string | null;
  expiresAt: string | null;
  /**
   * False until the zustand persist middleware has rehydrated from
   * localStorage. Guards prevent flash-of-redirect on first paint.
   */
  hydrated: boolean;

  setSession: (token: string, expiresAt?: string) => void;
  clearSession: () => void;
  setHydrated: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: null,
      expiresAt: null,
      hydrated: false,

      setSession: (token, expiresAt) => set({ token, expiresAt: expiresAt ?? null }),

      clearSession: () => set({ token: null, expiresAt: null }),

      setHydrated: () => set({ hydrated: true }),
    }),
    {
      name: 'fuelgrid.auth',
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        token: state.token,
        expiresAt: state.expiresAt,
      }),
      onRehydrateStorage: () => (state) => {
        state?.setHydrated();
      },
    },
  ),
);
