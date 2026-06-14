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

describe('Client setup and tank inventory methods', () => {
  it('GETs and PATCHes the persisted setup checklist', async () => {
    const checklist = {
      steps: [],
      required_total: 0,
      required_ready: 0,
      required_completed: 0,
      operationally_ready: false,
      blocked: [],
    };
    const f = jsonFetch(200, checklist);
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });

    await expect(client.getSetupChecklist()).resolves.toEqual(checklist);
    expect(callArgs(f).url).toBe('http://api.test/api/v1/setup/checklist');

    await expect(client.getSetupChecklist({ stationID: 'station-1' })).resolves.toEqual(checklist);
    expect(callArgs(f, 1).url).toBe('http://api.test/api/v1/setup/checklist?station_id=station-1');

    await client.updateSetupStep(
      { step_code: 'opening_stock', status: 'completed' },
      { stationID: 'station-1' },
    );
    const second = callArgs(f, 2);
    expect(second.url).toBe('http://api.test/api/v1/setup/checklist?station_id=station-1');
    expect(second.init.method).toBe('PATCH');
    expect(second.init.body).toBe(
      JSON.stringify({ step_code: 'opening_stock', status: 'completed' }),
    );
  });

  it('uses the real tank ledger, book-balance, and opening-balance paths', async () => {
    const f = jsonFetch(200, { items: [], count: 0, has_more: false });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });

    await client.listTankLedger('tank-1', { limit: 25, offset: 50 });
    expect(callArgs(f).url).toBe('http://api.test/api/v1/tanks/tank-1/ledger?limit=25&offset=50');

    await client.getTankBookBalance('tank-1');
    expect(callArgs(f, 1).url).toBe('http://api.test/api/v1/tanks/tank-1/book-balance');

    await client.setTankOpeningBalance('tank-1', { litres: '1200.000', notes: 'counted' });
    const third = callArgs(f, 2);
    expect(third.url).toBe('http://api.test/api/v1/tanks/tank-1/opening-balance');
    expect(third.init.method).toBe('POST');
    expect(third.init.body).toBe(JSON.stringify({ litres: '1200.000', notes: 'counted' }));
  });
});

describe('Client nozzle meter methods', () => {
  it('POSTs nozzle initial-meter adjustments to the documented endpoint', async () => {
    const f = jsonFetch(200, { id: 'nz-1', initial_meter_reading: '501' });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });

    await client.setNozzleInitialMeter('nz-1', {
      reading: '501.00',
      note: 'meter serviced',
    });

    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/nozzles/nz-1/initial-meter');
    expect(init.method).toBe('POST');
    expect(init.body).toBe(JSON.stringify({ reading: '501.00', note: 'meter serviced' }));
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

describe('Client audit log endpoints', () => {
  it('listAuditLogs forwards filters and paging as query params', async () => {
    const f = jsonFetch(200, { items: [], count: 0, has_more: false });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await client.listAuditLogs({
      action: 'expense.approved',
      entityType: 'expense',
      entityID: 'e1',
      actorID: 'a1',
      since: '2026-01-01T00:00:00Z',
      until: '2026-02-01T00:00:00Z',
      limit: 25,
      offset: 50,
    });
    const { url, init } = callArgs(f);
    expect(init.method ?? 'GET').toBe('GET');
    const u = new URL(url);
    expect(u.pathname).toBe('/api/v1/audit-logs');
    expect(u.searchParams.get('action')).toBe('expense.approved');
    expect(u.searchParams.get('entity_type')).toBe('expense');
    expect(u.searchParams.get('entity_id')).toBe('e1');
    expect(u.searchParams.get('actor_id')).toBe('a1');
    expect(u.searchParams.get('since')).toBe('2026-01-01T00:00:00Z');
    expect(u.searchParams.get('until')).toBe('2026-02-01T00:00:00Z');
    expect(u.searchParams.get('limit')).toBe('25');
    expect(u.searchParams.get('offset')).toBe('50');
  });

  it('exportAuditLogs POSTs with from/to and returns the export result', async () => {
    const f = jsonFetch(201, {
      export_id: 'x1',
      export_type: 'audit_logs',
      format: 'csv',
      from: '2026-01-01',
      to: '2026-01-31',
      row_count: 3,
      checksum: 'abc',
      csv: 'occurred_at,actor_id\n',
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.exportAuditLogs({ from: '2026-01-01', to: '2026-01-31' });
    const { url, init } = callArgs(f);
    expect(init.method).toBe('POST');
    const u = new URL(url);
    expect(u.pathname).toBe('/api/v1/audit-logs/export');
    expect(u.searchParams.get('from')).toBe('2026-01-01');
    expect(u.searchParams.get('to')).toBe('2026-01-31');
    expect(res.csv).toContain('occurred_at');
    expect(res.row_count).toBe(3);
  });
});

describe('Client report catalog endpoint', () => {
  it('getReportCatalog GETs /reports/catalog and returns the typed catalog', async () => {
    const f = jsonFetch(200, {
      generated_at: '2026-06-13T00:00:00Z',
      categories: [
        {
          key: 'sales',
          name: 'Sales',
          description: 'Revenue and litres.',
          icon: 'trending-up',
          sort_order: 20,
          required_permission: 'revenue.read',
          availability: 'live',
          target_route: '/reports/sales',
          metric: { label: 'Gross revenue (30d)', value: '1234567.89', unit: 'TZS' },
          alert_count: 0,
          reports: [],
        },
        {
          key: 'tank',
          name: 'Tank',
          description: 'Live tank telemetry.',
          icon: 'database',
          sort_order: 40,
          required_permission: 'inventory.read',
          availability: 'placeholder',
          target_route: '/reports/tank',
          metric: {
            label: 'Live tank telemetry',
            value: null,
            reason: 'No ATG / sensor feed connected — live tank telemetry is not available.',
          },
          alert_count: 0,
          reports: [],
        },
      ],
      data_quality: [{ category_key: 'tank', level: 'info', message: 'Tank: not available yet.' }],
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.getReportCatalog();
    const { url, init } = callArgs(f);
    expect(init.method ?? 'GET').toBe('GET');
    expect(new URL(url).pathname).toBe('/api/v1/reports/catalog');
    // Monetary metric is carried as an exact decimal string, never a number.
    expect(res.categories[0]?.metric.value).toBe('1234567.89');
    expect(typeof res.categories[0]?.metric.value).toBe('string');
    // A placeholder category carries a null metric and an honest reason.
    expect(res.categories[1]?.availability).toBe('placeholder');
    expect(res.categories[1]?.metric.value).toBeNull();
    expect(res.categories[1]?.metric.reason).toContain('sensor feed');
    expect(res.data_quality[0]?.category_key).toBe('tank');
  });
});

describe('Client mobile attendant phase-0 methods', () => {
  it('checkInToShift POSTs the check-in path with optional device info', async () => {
    const f = jsonFetch(201, { id: 'a1', status: 'checked_in' });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });

    await client.checkInToShift('shift-1', { device_info: { model: 'TestPhone' } });
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/shifts/shift-1/check-in');
    expect(init.method).toBe('POST');
    expect(init.body).toBe(JSON.stringify({ device_info: { model: 'TestPhone' } }));

    await client.checkInToShift('shift-1');
    expect(callArgs(f, 1).init.body).toBe(JSON.stringify({}));
  });

  it('checkOutOfShift POSTs the check-out path', async () => {
    const f = jsonFetch(200, { id: 'a1', status: 'checked_out' });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.checkOutOfShift('shift-1');
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/shifts/shift-1/check-out');
    expect(init.method).toBe('POST');
    expect(res.status).toBe('checked_out');
  });

  it('listShiftAttendance GETs the attendance list', async () => {
    const f = jsonFetch(200, { items: [], count: 0 });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await client.listShiftAttendance('shift-1');
    expect(callArgs(f).url).toBe('http://api.test/api/v1/shifts/shift-1/attendance');
  });

  it('confirmNozzleAssignment POSTs the confirm path', async () => {
    const f = jsonFetch(200, { id: 'as1', confirmed_at: '2026-06-11T08:00:00Z' });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.confirmNozzleAssignment('shift-1', 'as1');
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/shifts/shift-1/nozzle-assignments/as1/confirm');
    expect(init.method).toBe('POST');
    expect(res.confirmed_at).toBe('2026-06-11T08:00:00Z');
  });

  it('verifyShiftReadings POSTs the batch-verify path', async () => {
    const f = jsonFetch(200, { items: [], count: 0, newly_verified: 0 });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.verifyShiftReadings('shift-1');
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/shifts/shift-1/readings/verify');
    expect(init.method).toBe('POST');
    expect(res.newly_verified).toBe(0);
  });

  it('verifyCorrectReading POSTs both decimal-string values and the reason', async () => {
    const f = jsonFetch(201, {
      id: 'v1',
      attendant_submitted_reading: '1500.000',
      supervisor_verified_reading: '1490.000',
      final_approved_reading: '1490.000',
      status: 'corrected',
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.verifyCorrectReading('shift-1', 'r1', {
      verified_reading: '1490.000',
      reason: 'pump display misread',
    });
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/shifts/shift-1/readings/r1/verify-correct');
    expect(init.method).toBe('POST');
    expect(init.body).toBe(
      JSON.stringify({ verified_reading: '1490.000', reason: 'pump display misread' }),
    );
    expect(res.final_approved_reading).toBe('1490.000');
    expect(res.attendant_submitted_reading).toBe('1500.000');
  });

  it('rejectReading POSTs the reject path with the mandatory reason', async () => {
    const f = jsonFetch(201, {
      id: 'v1',
      attendant_submitted_reading: '1500.000',
      final_approved_reading: '1500.000',
      status: 'rejected',
      reason: 'meter photo unreadable',
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.rejectReading('shift-1', 'r1', { reason: 'meter photo unreadable' });
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/shifts/shift-1/readings/r1/reject');
    expect(init.method).toBe('POST');
    expect(init.body).toBe(JSON.stringify({ reason: 'meter photo unreadable' }));
    expect(res.status).toBe('rejected');
    expect(res.final_approved_reading).toBe('1500.000');
  });

  it('flagReading POSTs the flag path with the mandatory reason', async () => {
    const f = jsonFetch(201, {
      id: 'v2',
      attendant_submitted_reading: '1500.000',
      final_approved_reading: '1500.000',
      status: 'flagged',
      reason: 'inconsistent with dip',
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.flagReading('shift-1', 'r1', { reason: 'inconsistent with dip' });
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/shifts/shift-1/readings/r1/flag');
    expect(init.method).toBe('POST');
    expect(init.body).toBe(JSON.stringify({ reason: 'inconsistent with dip' }));
    expect(res.status).toBe('flagged');
  });

  it('approveReading POSTs the per-reading approve path (clears a hold)', async () => {
    const f = jsonFetch(201, {
      id: 'v3',
      attendant_submitted_reading: '1500.000',
      final_approved_reading: '1500.000',
      status: 'approved',
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.approveReading('shift-1', 'r1');
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/shifts/shift-1/readings/r1/approve');
    expect(init.method).toBe('POST');
    expect(res.status).toBe('approved');
    expect(res.final_approved_reading).toBe('1500.000');
  });

  it('listReadingVerifications GETs the verification set with decimal strings intact', async () => {
    const f = jsonFetch(200, {
      items: [
        {
          id: 'v1',
          attendant_submitted_reading: '1500.000',
          supervisor_verified_reading: '1490.000',
          final_approved_reading: '1490.000',
          status: 'corrected',
          reason: 'misread',
        },
      ],
      count: 1,
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.listReadingVerifications('shift-1');
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/shifts/shift-1/reading-verifications');
    expect(init.method ?? 'GET').toBe('GET');
    expect(res.count).toBe(1);
    expect(res.items[0]!.final_approved_reading).toBe('1490.000');
    expect(res.items[0]!.attendant_submitted_reading).toBe('1500.000');
  });
});

describe('Client mobile attendant phase-0 handover methods', () => {
  it('confirmCashSubmission POSTs the confirm path with decimal-string money', async () => {
    const f = jsonFetch(201, {
      id: 'cr1',
      expected_amount: '1475000.00',
      attendant_submitted_total: '1475000.00',
      supervisor_received_total: '1470000.00',
      difference: '-5000.00',
      status: 'approved_with_difference',
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.confirmCashSubmission('shift-1', {
      received_total: '1470000.00',
      reason: 'short by 5,000',
      supervisor_comment: 'counted twice',
    });
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/shifts/shift-1/cash-submission/confirm');
    expect(init.method).toBe('POST');
    expect(init.body).toBe(
      JSON.stringify({
        received_total: '1470000.00',
        reason: 'short by 5,000',
        supervisor_comment: 'counted twice',
      }),
    );
    expect(res.difference).toBe('-5000.00');
    expect(res.status).toBe('approved_with_difference');
  });

  it('confirmCashSubmission flags the handover (PRD §9.6 hold) with a reason', async () => {
    const f = jsonFetch(201, {
      id: 'cr2',
      expected_amount: '1475000.00',
      attendant_submitted_total: '1475000.00',
      supervisor_received_total: '1475000.00',
      difference: '0.00',
      status: 'flagged',
      reason: 'cash count disputed',
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.confirmCashSubmission('shift-1', {
      received_total: '1475000.00',
      status: 'flagged',
      reason: 'cash count disputed',
    });
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/shifts/shift-1/cash-submission/confirm');
    expect(init.method).toBe('POST');
    expect(init.body).toBe(
      JSON.stringify({
        received_total: '1475000.00',
        status: 'flagged',
        reason: 'cash count disputed',
      }),
    );
    expect(res.status).toBe('flagged');
  });

  it('getCollectionReceipt GETs the collection-receipt path with decimal strings intact', async () => {
    const f = jsonFetch(200, {
      id: 'cr1',
      expected_amount: '1475000.00',
      attendant_submitted_total: '1475000.00',
      supervisor_received_total: '1470000.00',
      difference: '-5000.00',
      status: 'approved_with_difference',
      reason: 'short by 5,000',
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.getCollectionReceipt('shift-1');
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/shifts/shift-1/collection-receipt');
    expect(init.method ?? 'GET').toBe('GET');
    expect(res.difference).toBe('-5000.00');
    expect(res.status).toBe('approved_with_difference');
  });

  it('listExpectedOpeningReadings GETs the expected-opening path', async () => {
    const f = jsonFetch(200, {
      items: [
        {
          assignment_id: 'as1',
          nozzle_id: 'noz-1',
          attendant_id: 'u-2',
          expected_opening_reading: '1490.000',
          source: 'verified',
        },
      ],
      count: 1,
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.listExpectedOpeningReadings('shift-1');
    expect(callArgs(f).url).toBe('http://api.test/api/v1/shifts/shift-1/expected-opening-readings');
    expect(res.items[0]?.expected_opening_reading).toBe('1490.000');
    expect(res.items[0]?.source).toBe('verified');
  });

  it('openShift forwards the handover override reason', async () => {
    const f = jsonFetch(201, { id: 'shift-2', status: 'open' });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await client.openShift('st-1', {
      operating_day_id: 'day-1',
      name: 'Evening',
      slot: 'evening',
      handover_override_reason: 'outgoing supervisor unreachable',
    });
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/stations/st-1/shifts');
    expect(init.body).toBe(
      JSON.stringify({
        operating_day_id: 'day-1',
        name: 'Evening',
        slot: 'evening',
        handover_override_reason: 'outgoing supervisor unreachable',
      }),
    );
  });
});

describe('Client mobile attendant phase-1 methods', () => {
  it('attendantCurrentShift GETs the workflow snapshot', async () => {
    const f = jsonFetch(200, {
      status: 'on_shift',
      next_action: 'check_in',
      user_message: 'Your shift is open. Check in to start working.',
      station: { id: 'st-1', name: 'Mikocheni' },
      shift: { id: 'shift-1', status: 'open' },
      attendance: { status: 'not_checked_in' },
      assignments: [],
      readings: [],
      expected_openings_available: false,
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.attendantCurrentShift();
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/attendant/current-shift');
    expect(init.method ?? 'GET').toBe('GET');
    expect(res.next_action).toBe('check_in');
    expect(res.station?.name).toBe('Mikocheni');
    expect(res.attendance.status).toBe('not_checked_in');
  });

  it('attendantCurrentShift surfaces the blocked state with its blocking code', async () => {
    const f = jsonFetch(200, {
      status: 'on_shift',
      next_action: 'blocked',
      blocking_code: 'awaiting_nozzle_assignment',
      user_message: 'You are checked in. Wait for your nozzle assignment.',
      attendance: { status: 'checked_in', check_in_at: '2026-06-11T05:00:00Z' },
      assignments: [],
      readings: [],
      expected_openings_available: false,
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.attendantCurrentShift();
    expect(res.next_action).toBe('blocked');
    expect(res.blocking_code).toBe('awaiting_nozzle_assignment');
  });
});

describe('Client mobile attendant phase-7 methods', () => {
  it('reportIncident POSTs the self-service path with the dedupe key', async () => {
    const f = jsonFetch(201, {
      id: 'inc-1',
      station_id: 'st-1',
      type: 'pump',
      severity: 'medium',
      status: 'open',
      opened_by: 'att-1',
      dedupe_key: 'queue-key-1',
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.reportIncident({
      type: 'pump',
      description: 'Pump 1 display flickers',
      dedupe_key: 'queue-key-1',
    });
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/incidents/report');
    expect(init.method).toBe('POST');
    expect(init.body).toBe(
      JSON.stringify({
        type: 'pump',
        description: 'Pump 1 display flickers',
        dedupe_key: 'queue-key-1',
      }),
    );
    expect(res.dedupe_key).toBe('queue-key-1');
    expect(res.status).toBe('open');
  });

  it('reportIncident replay resolves with the existing incident (200 body)', async () => {
    // The server answers a replayed dedupe_key with 200 + the ORIGINAL row;
    // the client treats it like any other success.
    const f = jsonFetch(200, { id: 'inc-1', status: 'open', dedupe_key: 'queue-key-1' });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.reportIncident({
      type: 'meter',
      description: 'Meter sticks at 9s',
      dedupe_key: 'queue-key-1',
    });
    expect(res.id).toBe('inc-1');
  });

  it('reportIncident surfaces the no_active_shift 409 for offline-queue branching', async () => {
    const f = jsonFetch(409, {
      error: 'you are not on an active shift; issues are reported from your current shift',
      code: 'no_active_shift',
      status: 409,
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    try {
      await client.reportIncident({ type: 'other', description: 'x' });
      expect.unreachable('expected SdkError');
    } catch (err) {
      const e = err as SdkError;
      expect(e.status).toBe(409);
      expect((e.body as { code?: string }).code).toBe('no_active_shift');
    }
  });

  it('getAttendanceReport GETs the station/date-range envelope', async () => {
    const f = jsonFetch(200, {
      metadata: { report_key: 'attendance' },
      summary: [{ label: 'Present', value: '3', unit: 'count' }],
      table: { columns: [], rows: [] },
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.getAttendanceReport('st-1', { from: '2026-06-01', to: '2026-06-12' });
    const { url, init } = callArgs(f);
    expect(url).toBe(
      'http://api.test/api/v1/reports/attendance?station_id=st-1&from=2026-06-01&to=2026-06-12',
    );
    expect(init.method ?? 'GET').toBe('GET');
    expect(res.metadata.report_key).toBe('attendance');
  });

  it('getCorrectionsVariancesReport keeps decimal-string figures intact', async () => {
    const f = jsonFetch(200, {
      metadata: { report_key: 'corrections-variances' },
      summary: [{ label: 'Total shortage', value: '5000.00', unit: 'TZS' }],
      chart_data: {
        corrections: [
          { submitted_reading: '1500.000', final_reading: '1490.000', delta_litres: '-10.000' },
        ],
        collections: [
          { expected_amount: '1445500.00', received_total: '1440500.00', difference: '-5000.00' },
        ],
      },
      table: { columns: [], rows: [] },
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const res = await client.getCorrectionsVariancesReport('st-1');
    const { url } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/reports/corrections-variances?station_id=st-1');
    const chart = res.chart_data as {
      corrections: Array<{ delta_litres: string }>;
      collections: Array<{ difference: string }>;
    };
    expect(chart.corrections[0]!.delta_litres).toBe('-10.000');
    expect(chart.collections[0]!.difference).toBe('-5000.00');
    expect(res.summary[0]!.value).toBe('5000.00');
  });
});

describe('Client async export-job methods (Export Center)', () => {
  it('enqueues an export job (POST /exports) and returns the queued job', async () => {
    const f = jsonFetch(202, {
      id: 'job-1',
      report_key: 'financials',
      format: 'csv',
      filters: { period: 'this-month' },
      status: 'queued',
      file_url: null,
      file_name: null,
      file_size: null,
      error: null,
      requested_by: 'user-1',
      created_at: '2026-06-14T09:30:00Z',
      download_url: null,
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const job = await client.createExportJob({
      report_key: 'financials',
      format: 'csv',
      filters: { period: 'this-month' },
    });
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/exports');
    expect(init.method).toBe('POST');
    expect(job.status).toBe('queued');
    expect(job.id).toBe('job-1');
  });

  it('reads a completed job with a download_url + checksum (GET /exports/{id})', async () => {
    const f = jsonFetch(200, {
      id: 'job-1',
      report_key: 'financials',
      format: 'pdf',
      filters: {},
      status: 'completed',
      file_url: null,
      file_name: 'financials-20260614.pdf',
      file_size: 4096,
      error: null,
      requested_by: 'user-1',
      created_at: '2026-06-14T09:30:00Z',
      started_at: '2026-06-14T09:30:02Z',
      completed_at: '2026-06-14T09:30:03Z',
      checksum: 'abc123',
      download_url: '/api/v1/exports/job-1/download',
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const job = await client.getExportJob('job-1');
    const { url } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/exports/job-1');
    expect(job.status).toBe('completed');
    expect(job.download_url).toBe('/api/v1/exports/job-1/download');
    expect(job.checksum).toBe('abc123');
  });

  it('downloads a completed job as a Blob (GET /exports/{id}/download)', async () => {
    const f = vi.fn(() =>
      Promise.resolve(
        new Response('%PDF-1.4 stub', {
          status: 200,
          headers: { 'Content-Type': 'application/pdf' },
        }),
      ),
    );
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const blob = await client.downloadExportJob('job-1');
    const { url, init } = callArgs(f);
    expect(url).toBe('http://api.test/api/v1/exports/job-1/download');
    expect(init.method).toBe('GET');
    expect(blob).toBeInstanceOf(Blob);
    expect(await blob.text()).toContain('%PDF-');
  });

  it('surfaces a 409 (not ready) as an SdkError on download', async () => {
    const f = jsonFetch(409, { error: 'export is not ready for download' });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await expect(client.downloadExportJob('job-1')).rejects.toBeInstanceOf(SdkError);
  });
});
