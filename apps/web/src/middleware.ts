import { NextResponse } from 'next/server';
import type { NextRequest } from 'next/server';

/**
 * Server-side route guard (WEB-001 / Wave-10 — httpOnly cookie migration).
 *
 * The session token now lives in the httpOnly `fg_session` cookie (set by the
 * BFF login route, attached to API calls server-side by the /api/bff proxy).
 * Middleware runs server-side, so it reads that real session cookie directly
 * and redirects protected-route requests to /login when it is absent — this is
 * a genuine presence check on the actual session credential, no longer the old
 * forgeable `fg_authed` flag.
 *
 * The cookie's mere presence (not its validity) is what's checked here; the Go
 * API still authoritatively validates the bearer on every call, and any 401
 * routes through the SDK backstop, which clears the cookie + redirects.
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

export function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;

  if (isPublic(pathname)) {
    return NextResponse.next();
  }

  const authed = Boolean(req.cookies.get(SESSION_COOKIE)?.value);
  if (authed) {
    return NextResponse.next();
  }

  // Unauthenticated request for a protected route: redirect to login,
  // preserving where they were headed so post-login lands them back.
  const loginUrl = req.nextUrl.clone();
  loginUrl.pathname = '/login';
  loginUrl.search = '';
  loginUrl.searchParams.set('next', pathname);
  return NextResponse.redirect(loginUrl);
}

/**
 * Run on everything EXCEPT Next.js internals, the API proxy path, and static
 * assets (files with an extension). This keeps the guard off `_next/*`,
 * `/api/*`, favicon, images, etc. — only real page navigations are gated.
 */
export const config = {
  matcher: ['/((?!api|_next/static|_next/image|favicon.ico|.*\\.[\\w]+$).*)'],
};
