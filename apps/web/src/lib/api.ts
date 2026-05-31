'use client';

import { Client } from '@fuelgrid/sdk';

import { useAuthStore } from '@/stores/auth-store';

/**
 * The browser talks to the same-origin BFF proxy, NOT the Go API directly
 * (WEB-001 / Wave-10). The proxy reads the httpOnly `fg_session` cookie
 * server-side and attaches the bearer, so no token is ever exposed to client
 * JS. A relative base URL keeps every call same-origin (and cookie-bearing).
 */
const baseURL = '/api/bff';

/**
 * Shared 401 handler: forget the local session hint and bounce to login,
 * preserving the current location as `?next=`. The httpOnly cookie is cleared
 * server-side — the BFF proxy strips it on any upstream 401 (and the logout
 * route clears it explicitly) — so here we only reset client state + navigate.
 * Guarded so we don't loop while already on /login. Used both as the SDK
 * transport backstop and by the React Query error caches (providers.tsx).
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
 * Singleton SDK client. No getToken: the bearer is injected server-side by the
 * BFF proxy from the httpOnly cookie, so the browser never holds the token.
 * onUnauthorized is the transport-level logout backstop: any 401, on any call,
 * clears the local session hint and redirects (SEC-3).
 */
export const api = new Client({
  baseURL,
  onUnauthorized: handleUnauthorized,
});
