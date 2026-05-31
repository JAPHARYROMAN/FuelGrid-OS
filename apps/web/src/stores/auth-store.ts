'use client';

import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

/**
 * Client-side auth state (WEB-001 / Wave-10 — httpOnly cookie migration).
 *
 * The session TOKEN no longer lives anywhere client JS can read it. It is held
 * only in an httpOnly `fg_session` cookie set by the BFF login route handler
 * and attached to API calls server-side by the same-origin proxy
 * (app/api/bff/[...path]). This store keeps ONLY non-sensitive UI state:
 *   - `authed`: a boolean hint (was the `fg_authed` presence flag) used by the
 *     client guards to avoid a flash-of-redirect before the real server-side
 *     middleware check runs. It is NOT a security boundary — middleware reads
 *     the httpOnly cookie, and the API independently enforces the bearer.
 *   - `expiresAt`: the session expiry the API reported, for display only.
 *
 * No token is ever persisted, so an XSS foothold has nothing to steal here.
 */
interface AuthState {
  /** Non-sensitive UI hint: a session was established (token is in the cookie). */
  authed: boolean;
  expiresAt: string | null;
  /**
   * False until the zustand persist middleware has rehydrated from
   * localStorage. Guards prevent flash-of-redirect on first paint.
   */
  hydrated: boolean;

  /** Record that a session was established (called after the BFF set the cookie). */
  setAuthed: (expiresAt?: string) => void;
  /** Forget the local session hint (the BFF/route handler clears the cookie). */
  clearSession: () => void;
  setHydrated: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      authed: false,
      expiresAt: null,
      hydrated: false,

      setAuthed: (expiresAt) => set({ authed: true, expiresAt: expiresAt ?? null }),

      clearSession: () => set({ authed: false, expiresAt: null }),

      setHydrated: () => set({ hydrated: true }),
    }),
    {
      name: 'fuelgrid.auth',
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        // Persist ONLY non-sensitive UI hints — never a token.
        authed: state.authed,
        expiresAt: state.expiresAt,
      }),
      onRehydrateStorage: () => (state) => {
        state?.setHydrated();
      },
    },
  ),
);
