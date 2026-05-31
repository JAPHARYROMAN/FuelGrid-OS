import { NextResponse } from 'next/server';
import type { NextRequest } from 'next/server';

import { apiOrigin } from '@/lib/server/api-origin';
import { SESSION_COOKIE, clearSessionCookie, setSessionCookie } from '@/lib/server/session-cookie';

/**
 * Same-origin BFF proxy (WEB-001 / Wave-10).
 *
 * The browser SDK points at `/api/bff` instead of the Go API directly, so the
 * session token never lives in client-readable storage. This handler runs
 * server-side: it reads the httpOnly `fg_session` cookie, attaches
 * `Authorization: Bearer <token>`, and forwards the request to the Go API
 * (which stays bearer-only — no API change). The token is therefore invisible
 * to client JS at every step, closing the XSS-token-theft hole the old
 * localStorage + non-httpOnly presence cookie left open.
 *
 * Two paths are special:
 *   - LOGIN (POST .../auth/login): forwarded unauthenticated; on a 200 that
 *     carries a token, the token is moved into the httpOnly cookie and STRIPPED
 *     from the JSON returned to the browser, so the client only learns
 *     { mfa_required, expires_at }.
 *   - LOGOUT (POST .../auth/logout): forwarded (best-effort) then the cookie is
 *     cleared regardless of the upstream result.
 *
 * Any upstream 401 also clears the cookie so a dead session cannot linger.
 */

// This route must run per-request (it reads cookies + proxies live calls).
export const dynamic = 'force-dynamic';

// Hop-by-hop and identity headers we must not blindly forward in either
// direction. The Authorization header is set by us, never passed through from
// the browser; Host/content-length are recomputed by fetch.
const STRIP_REQUEST_HEADERS = new Set([
  'host',
  'connection',
  'content-length',
  'authorization',
  'cookie',
]);

const STRIP_RESPONSE_HEADERS = new Set([
  'content-length',
  'content-encoding',
  'transfer-encoding',
  'connection',
  // Never let the upstream set cookies on our origin via the proxy.
  'set-cookie',
]);

function isLoginPath(path: string): boolean {
  return path === 'api/v1/auth/login';
}

function isLogoutPath(path: string): boolean {
  return path === 'api/v1/auth/logout';
}

function buildUpstreamHeaders(req: NextRequest, token: string | undefined): Headers {
  const headers = new Headers();
  req.headers.forEach((value, key) => {
    if (!STRIP_REQUEST_HEADERS.has(key.toLowerCase())) {
      headers.set(key, value);
    }
  });
  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }
  return headers;
}

function copyResponseHeaders(upstream: Response): Headers {
  const headers = new Headers();
  upstream.headers.forEach((value, key) => {
    if (!STRIP_RESPONSE_HEADERS.has(key.toLowerCase())) {
      headers.set(key, value);
    }
  });
  return headers;
}

async function proxy(req: NextRequest, segments: string[]): Promise<NextResponse> {
  const path = segments.join('/');
  const search = req.nextUrl.search;
  const url = `${apiOrigin()}/${path}${search}`;

  const token = req.cookies.get(SESSION_COOKIE)?.value || undefined;
  const login = isLoginPath(path);
  const logout = isLogoutPath(path);

  // Login is the bootstrap call — there is no session yet, so never attach a
  // (stale) cookie token to it.
  const headers = buildUpstreamHeaders(req, login ? undefined : token);

  // Read the body once. GET/HEAD have none.
  const method = req.method.toUpperCase();
  const hasBody = method !== 'GET' && method !== 'HEAD';
  const body = hasBody ? await req.arrayBuffer() : undefined;

  let upstream: Response;
  try {
    upstream = await fetch(url, {
      method,
      headers,
      body: body && body.byteLength > 0 ? body : undefined,
      redirect: 'manual',
      cache: 'no-store',
    });
  } catch {
    return NextResponse.json({ error: 'upstream request failed' }, { status: 502 });
  }

  const respHeaders = copyResponseHeaders(upstream);

  // ---- Login: move the token into the httpOnly cookie, strip it from JSON ----
  if (login && upstream.ok) {
    const text = await upstream.text();
    let parsed: Record<string, unknown> | null = null;
    try {
      parsed = text ? (JSON.parse(text) as Record<string, unknown>) : null;
    } catch {
      parsed = null;
    }
    const tok = parsed && typeof parsed.token === 'string' ? parsed.token : null;

    if (tok) {
      const expiresAt = typeof parsed?.expires_at === 'string' ? parsed.expires_at : undefined;
      // Never return the raw token to the browser.
      const safeBody = { ...parsed };
      delete safeBody.token;
      const res = NextResponse.json(safeBody, { status: upstream.status });
      setSessionCookie(res, tok, expiresAt);
      return res;
    }
    // No token (e.g. mfa_required) — pass the body through unchanged.
    return new NextResponse(text, { status: upstream.status, headers: respHeaders });
  }

  // ---- Logout: clear the cookie regardless of the upstream outcome ----
  if (logout) {
    const buf = await upstream.arrayBuffer();
    const res = new NextResponse(buf.byteLength > 0 ? buf : null, {
      status: upstream.status,
      headers: respHeaders,
    });
    clearSessionCookie(res);
    return res;
  }

  // ---- 401 backstop: a dead/invalid session must not linger in the cookie ----
  if (upstream.status === 401) {
    const buf = await upstream.arrayBuffer();
    const res = new NextResponse(buf.byteLength > 0 ? buf : null, {
      status: 401,
      headers: respHeaders,
    });
    clearSessionCookie(res);
    return res;
  }

  // ---- Everything else: stream the upstream response through unchanged ----
  const buf = await upstream.arrayBuffer();
  return new NextResponse(buf.byteLength > 0 ? buf : null, {
    status: upstream.status,
    headers: respHeaders,
  });
}

type Ctx = { params: Promise<{ path: string[] }> };

async function handle(req: NextRequest, ctx: Ctx): Promise<NextResponse> {
  const { path } = await ctx.params;
  return proxy(req, path);
}

export const GET = handle;
export const POST = handle;
export const PUT = handle;
export const PATCH = handle;
export const DELETE = handle;
