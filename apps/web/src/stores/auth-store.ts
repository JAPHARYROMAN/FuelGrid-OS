'use client';

import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

/**
 * Non-sensitive PRESENCE cookie name. It holds only "1" — never the token —
 * so the Next.js middleware (which cannot read localStorage) can server-side
 * gate protected routes. The real httpOnly-cookie session migration is a
 * tracked LATER item (WEB-001/W10); this is the interim guard (WEB-002).
 */
const PRESENCE_COOKIE = 'fg_authed';

/** Set the presence flag: Path=/, SameSite=Lax, NOT httpOnly (it's only a flag). */
function setPresenceCookie() {
  if (typeof document === 'undefined') return;
  document.cookie = `${PRESENCE_COOKIE}=1; Path=/; SameSite=Lax`;
}

/** Clear the presence flag by expiring it. */
function clearPresenceCookie() {
  if (typeof document === 'undefined') return;
  document.cookie = `${PRESENCE_COOKIE}=; Path=/; SameSite=Lax; Max-Age=0`;
}

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

      setSession: (token, expiresAt) => {
        setPresenceCookie();
        set({ token, expiresAt: expiresAt ?? null });
      },

      clearSession: () => {
        clearPresenceCookie();
        set({ token: null, expiresAt: null });
      },

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
        // Re-sync the presence cookie with the rehydrated token: a returning
        // user with a persisted token but no cookie (e.g. cookie expired, or
        // first load after this change shipped) should still pass middleware.
        if (state?.token) setPresenceCookie();
      },
    },
  ),
);
