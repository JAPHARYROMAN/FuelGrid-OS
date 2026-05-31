import { NextResponse } from 'next/server';
import type { NextRequest } from 'next/server';

/**
 * Server-side route guard (WEB-001 / Wave-10 — httpOnly cookie migration) PLUS
 * per-request nonce-based Content-Security-Policy (SEC — nonce CSP).
 *
 * --- Auth gate ---
 * The session token lives in the httpOnly `fg_session` cookie (set by the BFF
 * login route, attached to API calls server-side by the /api/bff proxy).
 * Middleware runs server-side, so it reads that real session cookie directly
 * and redirects protected-route requests to /login when it is absent — a
 * genuine presence check on the actual session credential, no longer the old
 * forgeable `fg_authed` flag. The cookie's mere presence (not its validity) is
 * what's checked here; the Go API still authoritatively validates the bearer on
 * every call, and any 401 routes through the SDK backstop, which clears the
 * cookie + redirects.
 *
 * --- Nonce CSP ---
 * The static `script-src 'self'` CSP previously shipped from next.config.ts had
 * NO nonce, which blocks Next.js's inline hydration bootstrap scripts in a
 * strict browser — so production browsers could fail to hydrate. We now mint a
 * fresh random nonce per request here and build the CSP with
 * `script-src 'nonce-<nonce>' 'strict-dynamic'`. When Next.js sees a nonce in
 * the CSP on the middleware response it automatically stamps that nonce onto
 * the framework's inline/bootstrap scripts, and `strict-dynamic` lets those
 * trusted scripts load the rest of the bundle. The nonce is also forwarded as
 * the `x-nonce` request header so Server Components can read it via headers()
 * for any inline <script>/<style> the app itself controls.
 */

const SESSION_COOKIE = 'fg_session';

/**
 * Public paths that never require the presence flag. Everything else under
 * the matcher is treated as protected. Kept as exact/prefix matches so a
 * future `/login-help` style page must be added explicitly.
 */
const PUBLIC_PREFIXES = ['/login', '/forgot-password', '/reset-password', '/mfa'];

function isPublic(pathname: string): boolean {
  if (pathname === '/') return true; // root is a thin client redirector
  return PUBLIC_PREFIXES.some((prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`));
}

/**
 * The browser talks to the API directly (fetch from the SDK), so the API
 * origin must be allowed in connect-src. Read it from the public env vars
 * the app already uses; fall back to 'self' when neither is defined.
 */
const apiOrigin = process.env.NEXT_PUBLIC_API_BASE_URL ?? process.env.NEXT_PUBLIC_API_URL ?? '';

/**
 * When Sentry is configured, its browser SDK POSTs events to the ingest host
 * encoded in the DSN (e.g. https://<key>@o123.ingest.sentry.io/456). Add only
 * that origin to connect-src — conditionally, so CSP is unchanged (and not
 * weakened) when no DSN is set. A malformed DSN simply contributes nothing.
 */
function sentryConnectSrc(): string {
  const dsn = process.env.NEXT_PUBLIC_SENTRY_DSN;
  if (!dsn) return '';
  try {
    return new URL(dsn).origin;
  } catch {
    return '';
  }
}

const connectSrc = ["'self'", apiOrigin, sentryConnectSrc()].filter(Boolean).join(' ');

/**
 * Generate a fresh base64 nonce using the Web Crypto API, which is available in
 * the edge/middleware runtime. 16 random bytes is the commonly recommended
 * size for a CSP nonce.
 */
function generateNonce(): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  // btoa is available in the edge runtime.
  let binary = '';
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary);
}

/**
 * Build the per-request CSP. `script-src` carries the nonce plus
 * `strict-dynamic` (so Next's nonced bootstrap can load the rest of the bundle)
 * and keeps `'self'` for non-strict-dynamic-aware browsers as a fallback.
 * `style-src` keeps `'unsafe-inline'` — required by Tailwind / next-themes
 * injected style tags. The remaining directives mirror the prior static policy.
 */
function buildCsp(nonce: string): string {
  return [
    "default-src 'self'",
    `connect-src ${connectSrc}`,
    // PWA: the web app manifest and its icon PNGs are served same-origin.
    "manifest-src 'self'",
    "img-src 'self' data:",
    "style-src 'self' 'unsafe-inline'",
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic'`,
    "font-src 'self' data:",
    "object-src 'none'",
    "frame-ancestors 'none'",
    "base-uri 'self'",
    "form-action 'self'",
  ].join('; ');
}

export function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;

  // Mint a per-request nonce and the matching CSP for every response, whether
  // the request is public, authed, or redirected.
  const nonce = generateNonce();
  const csp = buildCsp(nonce);

  // Auth gate: unauthenticated request for a protected route -> redirect to
  // /login, preserving where they were headed. The CSP still applies to the
  // redirect response.
  if (!isPublic(pathname) && !req.cookies.get(SESSION_COOKIE)?.value) {
    const loginUrl = req.nextUrl.clone();
    loginUrl.pathname = '/login';
    loginUrl.search = '';
    loginUrl.searchParams.set('next', pathname);
    const redirect = NextResponse.redirect(loginUrl);
    redirect.headers.set('Content-Security-Policy', csp);
    return redirect;
  }

  // Forward both the nonce (x-nonce, for Server Components via headers()) AND
  // the CSP on the REQUEST headers. Next.js reads the nonce off the incoming
  // `content-security-policy` request header (see app-render's
  // getScriptNonceFromHeader) to stamp it onto its own framework/bootstrap
  // scripts — without the request-header CSP the scripts ship un-nonced and the
  // app fails to hydrate under the policy. The CSP is also set on the response
  // so the browser actually enforces it.
  const requestHeaders = new Headers(req.headers);
  requestHeaders.set('x-nonce', nonce);
  requestHeaders.set('Content-Security-Policy', csp);

  const res = NextResponse.next({ request: { headers: requestHeaders } });
  res.headers.set('Content-Security-Policy', csp);
  return res;
}

/**
 * Run on everything EXCEPT Next.js internals, the API proxy path, and static
 * assets (files with an extension). This keeps the guard off `_next/*`,
 * `/api/*`, favicon, images, etc. — only real page navigations are gated and
 * carry the nonce CSP. Static assets do not need the per-request nonce; the
 * non-script hardening headers from next.config.ts still apply to them.
 */
export const config = {
  matcher: ['/((?!api|_next/static|_next/image|favicon.ico|.*\\.[\\w]+$).*)'],
};
