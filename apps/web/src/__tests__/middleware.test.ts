import { describe, expect, it } from 'vitest';
import { NextRequest } from 'next/server';

import { middleware } from '@/middleware';

function req(path: string, opts: { authed?: boolean } = {}): NextRequest {
  const url = `http://localhost:3000${path}`;
  const headers = new Headers();
  if (opts.authed) headers.set('cookie', 'fg_authed=1');
  return new NextRequest(url, { headers });
}

describe('route-guard middleware (WEB-002, presence-flag)', () => {
  it('lets public routes through without the presence flag', () => {
    for (const path of [
      '/login',
      '/login?next=/finance',
      '/forgot-password',
      '/reset-password',
      '/',
    ]) {
      const res = middleware(req(path));
      // NextResponse.next() carries no redirect Location header.
      expect(res.headers.get('location')).toBeNull();
    }
  });

  it('redirects a protected route to /login with ?next when the flag is absent', () => {
    const res = middleware(req('/finance'));
    const location = res.headers.get('location');
    expect(location).not.toBeNull();
    const loc = new URL(location as string);
    expect(loc.pathname).toBe('/login');
    expect(loc.searchParams.get('next')).toBe('/finance');
  });

  it('allows a protected route through when the presence flag is set', () => {
    const res = middleware(req('/finance', { authed: true }));
    expect(res.headers.get('location')).toBeNull();
  });
});
