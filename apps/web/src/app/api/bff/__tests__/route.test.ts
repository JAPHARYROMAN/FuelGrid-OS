// @vitest-environment node
//
// Server-side coverage for the same-origin BFF proxy (WEB-001 / Wave-10).
//
// The Playwright e2e suite mocks this proxy at the *browser* level, so the
// forward-and-strip logic that runs on the server (read the httpOnly
// fg_session cookie -> attach Authorization: Bearer to the Go API; on login
// move the token into the cookie + strip it from the JSON; on logout/401 clear
// the cookie) was previously verified only by build + reasoning. These tests
// exercise the exported route handlers directly against a mocked upstream
// fetch, with no live Go API.
//
// This file pins the Node test environment (route handlers run in Node, not
// jsdom) via the docblock above so it does not perturb the existing jsdom RTL
// suites.

import { NextRequest } from 'next/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { GET, POST } from '../[...path]/route';
import { SESSION_COOKIE } from '@/lib/server/session-cookie';

const TOKEN = 'secret-bearer-token-xyz';
const UPSTREAM_ORIGIN = 'http://localhost:8080';

// ctx.params is a Promise in Next 15 route handlers.
function ctx(...path: string[]) {
  return { params: Promise.resolve({ path }) };
}

function request(
  method: string,
  pathAndQuery: string,
  opts: { cookieToken?: string; body?: unknown; headers?: Record<string, string> } = {},
): NextRequest {
  const url = `http://localhost:3000/api/bff/${pathAndQuery}`;
  const headers = new Headers(opts.headers);
  if (opts.cookieToken !== undefined) {
    headers.set('cookie', `${SESSION_COOKIE}=${opts.cookieToken}`);
  }
  let body: string | undefined;
  if (opts.body !== undefined) {
    headers.set('content-type', 'application/json');
    body = JSON.stringify(opts.body);
  }
  return new NextRequest(url, { method, headers, body });
}

// A typed handle on the mocked global fetch so we can read the captured call.
let fetchMock: ReturnType<typeof vi.fn>;

function mockUpstream(response: Response): void {
  fetchMock = vi.fn(async () => response);
  vi.stubGlobal('fetch', fetchMock);
}

function lastFetchCall(): { url: string; init: RequestInit & { headers: Headers } } {
  expect(fetchMock).toHaveBeenCalledTimes(1);
  const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit & { headers: Headers }];
  return { url, init };
}

beforeEach(() => {
  // api-origin.ts falls back to localhost:8080, but pin it so the test is not
  // hostage to ambient env.
  process.env.API_ORIGIN = UPSTREAM_ORIGIN;
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  delete process.env.API_ORIGIN;
});

describe('BFF proxy — authenticated forwarding', () => {
  it('reads fg_session and sends Authorization: Bearer to the upstream origin', async () => {
    mockUpstream(
      new Response(JSON.stringify({ id: 'station-1' }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }),
    );

    const req = request('GET', 'api/v1/stations?page=2', { cookieToken: TOKEN });
    const res = await GET(req, ctx('api', 'v1', 'stations'));

    const { url, init } = lastFetchCall();
    // Forwarded to the Go API origin, preserving path + query string.
    expect(url).toBe(`${UPSTREAM_ORIGIN}/api/v1/stations?page=2`);
    expect(init.headers.get('authorization')).toBe(`Bearer ${TOKEN}`);
    // The browser's raw cookie header is never forwarded to the upstream.
    expect(init.headers.get('cookie')).toBeNull();

    expect(res.status).toBe(200);
    await expect(res.json()).resolves.toEqual({ id: 'station-1' });
  });

  it('never leaks the bearer token to the browser response', async () => {
    mockUpstream(
      new Response(JSON.stringify({ id: 'station-1' }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }),
    );

    const req = request('GET', 'api/v1/stations', { cookieToken: TOKEN });
    const res = await GET(req, ctx('api', 'v1', 'stations'));

    // Token must not appear in any response header (no Authorization echo,
    // no Set-Cookie carrying it) nor the JSON body.
    for (const [, value] of res.headers) {
      expect(value).not.toContain(TOKEN);
    }
    const bodyText = await res.text();
    expect(bodyText).not.toContain(TOKEN);
  });

  it('forwards the request body for non-GET methods', async () => {
    mockUpstream(new Response(JSON.stringify({ ok: true }), { status: 200 }));

    const req = request('POST', 'api/v1/orders', { cookieToken: TOKEN, body: { qty: 3 } });
    await POST(req, ctx('api', 'v1', 'orders'));

    const { init } = lastFetchCall();
    expect(init.headers.get('authorization')).toBe(`Bearer ${TOKEN}`);
    const sentBody = Buffer.from(init.body as ArrayBuffer).toString('utf8');
    expect(JSON.parse(sentBody)).toEqual({ qty: 3 });
  });

  it('does not attach Authorization when there is no session cookie', async () => {
    mockUpstream(new Response(JSON.stringify({ ok: true }), { status: 200 }));

    const req = request('GET', 'api/v1/public/health');
    await GET(req, ctx('api', 'v1', 'public', 'health'));

    const { init } = lastFetchCall();
    expect(init.headers.get('authorization')).toBeNull();
  });
});

describe('BFF proxy — login token capture', () => {
  it('moves the token into an httpOnly fg_session cookie and strips it from the JSON', async () => {
    const expiresAt = '2099-01-01T00:00:00Z';
    mockUpstream(
      new Response(JSON.stringify({ token: TOKEN, expires_at: expiresAt, mfa_required: false }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }),
    );

    const req = request('POST', 'api/v1/auth/login', {
      body: { email: 'a@b.com', password: 'pw' },
    });
    const res = await POST(req, ctx('api', 'v1', 'auth', 'login'));

    // Login is forwarded UNauthenticated — no stale cookie token attached.
    const { init } = lastFetchCall();
    expect(init.headers.get('authorization')).toBeNull();

    // Token moved into a Set-Cookie fg_session, marked httpOnly.
    const setCookie = res.headers.get('set-cookie') ?? '';
    expect(setCookie).toContain(`${SESSION_COOKIE}=${TOKEN}`);
    expect(setCookie.toLowerCase()).toContain('httponly');

    // The set cookie is also visible on the parsed cookie jar.
    expect(res.cookies.get(SESSION_COOKIE)?.value).toBe(TOKEN);

    // Token stripped from the JSON the browser receives.
    const body = (await res.json()) as Record<string, unknown>;
    expect(body.token).toBeUndefined();
    expect(body.mfa_required).toBe(false);
    expect(body.expires_at).toBe(expiresAt);

    // And definitely not lurking anywhere in the raw body.
    const raw = JSON.stringify(body);
    expect(raw).not.toContain(TOKEN);
  });

  it('passes an mfa_required login response through without setting the cookie', async () => {
    mockUpstream(
      new Response(JSON.stringify({ mfa_required: true }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }),
    );

    const req = request('POST', 'api/v1/auth/login', {
      body: { email: 'a@b.com', password: 'pw' },
    });
    const res = await POST(req, ctx('api', 'v1', 'auth', 'login'));

    expect(res.headers.get('set-cookie')).toBeNull();
    const body = (await res.json()) as Record<string, unknown>;
    expect(body.mfa_required).toBe(true);
  });
});

describe('BFF proxy — cookie clearing', () => {
  it('clears the fg_session cookie on an upstream 401', async () => {
    mockUpstream(
      new Response(JSON.stringify({ error: 'unauthorized' }), {
        status: 401,
        headers: { 'content-type': 'application/json' },
      }),
    );

    const req = request('GET', 'api/v1/stations', { cookieToken: TOKEN });
    const res = await GET(req, ctx('api', 'v1', 'stations'));

    expect(res.status).toBe(401);
    const cleared = res.cookies.get(SESSION_COOKIE);
    expect(cleared?.value).toBe('');
    expect(cleared?.maxAge).toBe(0);
    const setCookie = res.headers.get('set-cookie') ?? '';
    expect(setCookie).toContain(`${SESSION_COOKIE}=`);
    expect(setCookie.toLowerCase()).toMatch(/max-age=0/);
  });

  it('clears the fg_session cookie on logout regardless of upstream outcome', async () => {
    mockUpstream(new Response(null, { status: 204 }));

    const req = request('POST', 'api/v1/auth/logout', { cookieToken: TOKEN });
    const res = await POST(req, ctx('api', 'v1', 'auth', 'logout'));

    // Logout is forwarded with the bearer (best-effort revoke) but the cookie
    // is always cleared afterwards.
    const { init } = lastFetchCall();
    expect(init.headers.get('authorization')).toBe(`Bearer ${TOKEN}`);

    const cleared = res.cookies.get(SESSION_COOKIE);
    expect(cleared?.value).toBe('');
    expect(cleared?.maxAge).toBe(0);
  });
});
