'use client';

import { Client } from '@fuelgrid/sdk';

import { useAuthStore } from '@/stores/auth-store';

const baseURL = process.env.NEXT_PUBLIC_API_URL?.replace(/\/$/, '') ?? 'http://localhost:8080';

/**
 * Shared 401 handler: clear the (now-invalid) session and bounce to login,
 * preserving the current location as `?next=`. Guarded so we don't loop while
 * already on /login. Used both as the SDK transport backstop and by the
 * React Query error caches (providers.tsx).
 */
export function handleUnauthorized() {
  useAuthStore.getState().clearSession();
  if (typeof window === 'undefined') return;
  const { pathname, search } = window.location;
  if (pathname === '/login') return;
  const next = encodeURIComponent(`${pathname}${search}`);
  window.location.assign(`/login?next=${next}`);
}

/**
 * Singleton SDK client. The getToken callback reads from the auth store
 * on every request so a token refresh / logout propagates without
 * rebuilding the client. onUnauthorized is the transport-level logout
 * backstop: any 401, on any call, clears the session and redirects (SEC-3).
 */
export const api = new Client({
  baseURL,
  getToken: () => useAuthStore.getState().token,
  onUnauthorized: handleUnauthorized,
});
