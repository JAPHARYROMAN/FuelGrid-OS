import { afterEach, describe, expect, it, vi } from 'vitest';

import { Client, SdkError } from './client';

// A fetch stub returning a JSON Response, recording how it was called. 204/200
// with no body use a null body (the Response constructor rejects a non-null
// body for null-body statuses like 204).
function jsonFetch(status: number, body?: unknown) {
  return vi.fn((_input: RequestInfo | URL, _init?: RequestInit) =>
    Promise.resolve(new Response(body === undefined ? null : JSON.stringify(body), { status })),
  );
}

// Safely pull the URL + init of the i-th recorded fetch call (tsconfig has
// noUncheckedIndexedAccess, so mock.calls[i] is possibly undefined).
function callArgs(f: ReturnType<typeof jsonFetch>, i = 0): { url: string; init: RequestInit } {
  const call = f.mock.calls[i];
  if (!call) throw new Error(`fetch call #${i} was not made`);
  return { url: String(call[0]), init: (call[1] ?? {}) as RequestInit };
}

const originalFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = originalFetch;
  vi.restoreAllMocks();
});

describe('Client default fetch binding (blank-page regression)', () => {
  it('binds the global fetch to globalThis so it is not invoked as a method', async () => {
    // Real browsers throw "TypeError: Illegal invocation" when fetch is called
    // with `this` set to anything but the global object. Emulate that contract:
    // this stub throws unless `this === globalThis`. The client must bind the
    // default fetch (client.ts) or this login would throw — the exact bug that
    // left the app a blank page with a non-working Sign in.
    const seen: string[] = [];
    const strictFetch = function (this: unknown, input: RequestInfo | URL) {
      if (this !== globalThis) throw new TypeError('Illegal invocation');
      seen.push(String(input));
      return Promise.resolve(new Response(JSON.stringify({ token: 'tok' }), { status: 200 }));
    };
    globalThis.fetch = strictFetch as typeof fetch;

    const client = new Client({ baseURL: 'http://api.test' }); // no fetch override
    const res = await client.login({ tenant_slug: 'demo', email: 'a@b.c', password: 'pw' });

    expect(res).toEqual({ token: 'tok' });
    expect(seen).toEqual(['http://api.test/api/v1/auth/login']);
  });

  it('uses a caller-supplied fetch as-is, never the global', async () => {
    const f = jsonFetch(200, { token: 'tok' });
    globalThis.fetch = (() => {
      throw new Error('global fetch must not be used when an override is supplied');
    }) as typeof fetch;

    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await client.login({ tenant_slug: 'demo', email: 'a@b.c', password: 'pw' });

    expect(f).toHaveBeenCalledOnce();
  });
});

describe('Client.request', () => {
  it('strips a trailing slash from baseURL when building the URL', async () => {
    const f = jsonFetch(200, { ok: true });
    const client = new Client({ baseURL: 'http://api.test/', fetch: f as unknown as typeof fetch });
    await client.request('/api/v1/thing');
    expect(callArgs(f).url).toBe('http://api.test/api/v1/thing');
  });

  it('attaches the bearer token and a JSON content-type for a body', async () => {
    const f = jsonFetch(200, { ok: true });
    const client = new Client({
      baseURL: 'http://api.test',
      getToken: () => 'session-token',
      fetch: f as unknown as typeof fetch,
    });
    await client.request('/api/v1/thing', { method: 'POST', body: { a: 1 } });
    const { init } = callArgs(f);
    const headers = init.headers as Record<string, string>;
    expect(headers.Authorization).toBe('Bearer session-token');
    expect(headers['Content-Type']).toBe('application/json');
    expect(init.body).toBe(JSON.stringify({ a: 1 }));
  });

  it('omits Authorization on unauthenticated requests even when a token exists', async () => {
    const f = jsonFetch(200, { token: 'x' });
    const client = new Client({
      baseURL: 'http://api.test',
      getToken: () => 'session-token',
      fetch: f as unknown as typeof fetch,
    });
    await client.login({ tenant_slug: 'demo', email: 'a@b.c', password: 'pw' });
    const headers = callArgs(f).init.headers as Record<string, string>;
    expect(headers.Authorization).toBeUndefined();
  });

  it('returns undefined for a 204 No Content response', async () => {
    const f = jsonFetch(204);
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await expect(client.logout()).resolves.toBeUndefined();
  });

  it('throws an SdkError carrying the status and parsed body on a non-2xx response', async () => {
    const f = jsonFetch(422, { error: 'charge would exceed the credit limit' });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await expect(
      client.request('/api/v1/payments', { method: 'POST', body: {} }),
    ).rejects.toMatchObject({
      name: 'SdkError',
      status: 422,
      message: 'charge would exceed the credit limit',
    });
  });

  it('preserves the full parsed error body on the SdkError for branching', async () => {
    const f = jsonFetch(409, { error: 'conflict', detail: 'already sealed' });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    try {
      await client.request('/api/v1/reconciliations/seal', { method: 'POST' });
      expect.unreachable('expected the request to reject');
    } catch (err) {
      expect(err).toBeInstanceOf(SdkError);
      expect((err as SdkError).status).toBe(409);
      expect((err as SdkError).body).toEqual({ error: 'conflict', detail: 'already sealed' });
    }
  });
});
