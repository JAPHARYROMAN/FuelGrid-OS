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

describe('Client 401 handling + error context (SEC-3, SDK-03/04)', () => {
  it('invokes onUnauthorized before throwing on a 401', async () => {
    const onUnauthorized = vi.fn();
    const f = jsonFetch(401, { error: 'token expired' });
    const client = new Client({
      baseURL: 'http://api.test',
      fetch: f as unknown as typeof fetch,
      onUnauthorized,
    });
    await expect(client.request('/api/v1/me')).rejects.toMatchObject({
      name: 'SdkError',
      status: 401,
    });
    expect(onUnauthorized).toHaveBeenCalledOnce();
  });

  it('does not invoke onUnauthorized for non-401 errors', async () => {
    const onUnauthorized = vi.fn();
    const f = jsonFetch(403, { error: 'forbidden' });
    const client = new Client({
      baseURL: 'http://api.test',
      fetch: f as unknown as typeof fetch,
      onUnauthorized,
    });
    await expect(client.request('/api/v1/me')).rejects.toMatchObject({ status: 403 });
    expect(onUnauthorized).not.toHaveBeenCalled();
  });

  it('carries the X-Request-Id header on the SdkError for correlation', async () => {
    const f = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ error: 'boom' }), {
          status: 500,
          headers: { 'X-Request-Id': 'req-abc-123' },
        }),
      ),
    );
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    try {
      await client.request('/api/v1/thing');
      expect.unreachable('expected the request to reject');
    } catch (err) {
      expect(err).toBeInstanceOf(SdkError);
      expect((err as SdkError).requestId).toBe('req-abc-123');
    }
  });

  it('leaves requestId null when the server sent no X-Request-Id', async () => {
    const f = jsonFetch(500, { error: 'boom' });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await expect(client.request('/api/v1/thing')).rejects.toMatchObject({ requestId: null });
  });

  it('returns null for an empty 200 body without throwing', async () => {
    const f = jsonFetch(200); // no body
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await expect(client.request('/api/v1/thing')).resolves.toBeNull();
  });
});

describe('Client scoped runtime validation (SDK-01)', () => {
  it('passes a valid critical (login) payload through unchanged', async () => {
    const f = jsonFetch(200, { token: 'tok', expires_at: '2026-01-01T00:00:00Z' });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await expect(
      client.login({ tenant_slug: 'demo', email: 'a@b.c', password: 'pw' }),
    ).resolves.toEqual({ token: 'tok', expires_at: '2026-01-01T00:00:00Z' });
  });

  it('rejects a malformed critical (/me) payload with an SdkError', async () => {
    // user_id missing — the meSchema requires it.
    const f = jsonFetch(200, { tenant_id: 't1', session_id: 's1', mfa_satisfied: true });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await expect(client.me()).rejects.toMatchObject({ name: 'SdkError', status: 0 });
  });

  it('validates only opted-in calls — a plain request is not schema-checked', async () => {
    // A shape that would fail meSchema, but listStations does not validate.
    const f = jsonFetch(200, { anything: 'goes' });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await expect(client.request('/api/v1/stations')).resolves.toEqual({ anything: 'goes' });
  });
});

describe('Client.request transport errors (SDK-03)', () => {
  it('wraps a network failure as an SdkError with status 0, not a raw TypeError', async () => {
    const f = vi.fn(() => Promise.reject(new TypeError('Failed to fetch')));
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await expect(client.request('/api/v1/thing')).rejects.toMatchObject({
      name: 'SdkError',
      status: 0,
    });
  });

  it('rethrows an AbortError unchanged so a deliberate cancellation is detectable', async () => {
    const abort = new DOMException('aborted', 'AbortError');
    const f = vi.fn(() => Promise.reject(abort));
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await expect(client.request('/api/v1/thing')).rejects.toBe(abort);
  });
});

describe('Client list-document PDF exports (DOC-PDF)', () => {
  // A fetch stub returning a PDF Blob response, recording how it was called.
  function pdfFetch(status: number) {
    return vi.fn((_input: RequestInfo | URL, _init?: RequestInit) =>
      Promise.resolve(
        new Response(new Blob([new Uint8Array([0x25, 0x50, 0x44, 0x46])]), {
          status,
          headers: { 'Content-Type': 'application/pdf' },
        }),
      ),
    );
  }

  const cases = [
    { name: 'customersPdf', path: '/api/v1/customers.pdf' },
    { name: 'suppliersPdf', path: '/api/v1/suppliers.pdf' },
    { name: 'productsPdf', path: '/api/v1/products.pdf' },
  ] as const;

  for (const c of cases) {
    it(`${c.name} GETs ${c.path} with an application/pdf Accept and returns a Blob`, async () => {
      const f = pdfFetch(200);
      const client = new Client({
        baseURL: 'http://api.test',
        fetch: f as unknown as typeof fetch,
      });
      const blob = await client[c.name]();
      expect(blob).toBeInstanceOf(Blob);
      const { url, init } = callArgs(f);
      expect(url).toBe(`http://api.test${c.path}`);
      expect(init.method).toBe('GET');
      expect((init.headers as Record<string, string>).Accept).toBe('application/pdf');
      expect(init.credentials).toBe('same-origin');
    });

    it(`${c.name} throws an SdkError on a 403`, async () => {
      const f = jsonFetch(403, { error: 'forbidden' });
      const client = new Client({
        baseURL: 'http://api.test',
        fetch: f as unknown as typeof fetch,
      });
      await expect(client[c.name]()).rejects.toMatchObject({ name: 'SdkError', status: 403 });
    });
  }

  it('exposes matching same-origin URL helpers', () => {
    const client = new Client({ baseURL: 'http://api.test' });
    expect(client.customersPdfUrl()).toBe('http://api.test/api/v1/customers.pdf');
    expect(client.suppliersPdfUrl()).toBe('http://api.test/api/v1/suppliers.pdf');
    expect(client.productsPdfUrl()).toBe('http://api.test/api/v1/products.pdf');
  });
});
