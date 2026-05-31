import type { NextResponse } from 'next/server';

/**
 * httpOnly session-cookie contract (WEB-001 / Wave-10).
 *
 * The session token lives ONLY in this httpOnly, Secure, SameSite=Lax cookie.
 * It is never readable by client JS (no localStorage, no document.cookie), so
 * an XSS foothold cannot exfiltrate it. The browser never attaches the bearer
 * itself — the same-origin BFF proxy (app/api/bff/[...path]) reads this cookie
 * server-side and adds `Authorization: Bearer <token>` when forwarding to the
 * Go API. The Go API stays bearer-only; nothing changed server-side there.
 */
export const SESSION_COOKIE = 'fg_session';

/**
 * Secure is set in production only. On localhost dev (http) a Secure cookie
 * would never be stored, breaking login; Next runs dev over http, so gate it.
 */
const isProd = process.env.NODE_ENV === 'production';

/**
 * Shared attributes for the session cookie. httpOnly + SameSite=Lax is the
 * core hardening; Lax (not Strict) preserves the top-level /login?next=
 * redirect navigation. Path=/ so every route — including the BFF proxy and
 * middleware — sees it.
 */
const BASE_ATTRS = {
  httpOnly: true,
  secure: isProd,
  sameSite: 'lax',
  path: '/',
} as const;

/**
 * Set the session cookie on a response. expiresAt (RFC3339 from the API) is
 * translated to the cookie's Expires so the browser drops it when the session
 * would have lapsed anyway; absent/invalid expiry falls back to a session
 * cookie (cleared on browser close).
 */
export function setSessionCookie(res: NextResponse, token: string, expiresAt?: string): void {
  const expires = expiresAt ? new Date(expiresAt) : undefined;
  res.cookies.set({
    name: SESSION_COOKIE,
    value: token,
    ...BASE_ATTRS,
    ...(expires && !Number.isNaN(expires.getTime()) ? { expires } : {}),
  });
}

/** Clear the session cookie (logout / 401 backstop). */
export function clearSessionCookie(res: NextResponse): void {
  res.cookies.set({
    name: SESSION_COOKIE,
    value: '',
    ...BASE_ATTRS,
    maxAge: 0,
  });
}
