import { NextResponse } from 'next/server';
import type { NextRequest } from 'next/server';

/**
 * Server-side route guard (WEB-002 / FE-MW).
 *
 * PRESENCE-FLAG GUARD — NOT a real session check. The session token lives in
 * localStorage today, which middleware cannot read. So auth-store sets a
 * non-sensitive presence cookie (`fg_authed=1`, SameSite=Lax, NOT httpOnly)
 * on login and clears it on logout. This middleware redirects requests for
 * protected routes to /login when that flag is absent.
 *
 * Because the flag is forgeable, this is defense-in-depth only: it stops the
 * dashboard HTML from streaming to a logged-out visitor and removes the
 * client-side flash-of-redirect. The API still independently enforces the
 * bearer token on every call, and the client-side ProtectedRoute remains in
 * place. Migrating the token itself to an httpOnly cookie (so this becomes a
 * real session check) is a tracked LATER item: WEB-001 / Wave-10.
 */

const PRESENCE_COOKIE = 'fg_authed';

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

  const authed = req.cookies.get(PRESENCE_COOKIE)?.value === '1';
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
