import { afterEach, describe, expect, it, vi } from 'vitest';

import { Client, SdkError } from './client';
import type { Paginated, Payment, Sale } from './types';

// SDK-level contract tests (C.5). These complement client.test.ts by pinning
// the request/response SHAPING of the main resource methods rather than the
// transport mechanics:
//
//   - decimal-string money/litre fields survive a round-trip as STRINGS (the
//     numeric->text contract): the SDK never coerces them to JS numbers, which
//     would re-introduce binary-float drift;
//   - the Paginated<T> envelope (items/count/has_more/limit/offset) is returned
//     verbatim and limit/offset are forwarded as query params;
//   - a 401 fires onUnauthorized exactly once and still rejects;
//   - non-2xx bodies map onto SdkError (status + parsed body + message).
//
// fetch is always mocked — no network, no DB.

// A fetch stub returning a JSON Response, recording how it was called.
function jsonFetch(status: number, body?: unknown, headers?: Record<string, string>) {
  return vi.fn((_input: RequestInfo | URL, _init?: RequestInit) =>
    Promise.resolve(
      new Response(body === undefined ? null : JSON.stringify(body), { status, headers }),
    ),
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

describe('SDK decimal-string money/litre contract', () => {
  it("preserves a sale row's decimal-string money fields as STRINGS (never numbers)", async () => {
    // The Go API emits these as exact decimal strings. The wire payload below
    // is what listStationSales receives; the SDK must hand them back unchanged.
    const sale: Sale = {
      id: 's1',
      shift_id: 'sh1',
      station_id: 'st1',
      operating_day_id: 'od1',
      nozzle_id: 'nz1',
      product_id: 'p1',
      tank_id: 'tk1',
      litres: 1234.567,
      // Trailing zeros that Number() would silently drop ("2950.00" -> 2950).
      unit_price: '2950.00',
      gross_amount: '3642372.60',
      tax_rate: '0.16',
      tax_amount: '502395.54',
      net_amount: '3139977.10',
      recorded_at: '2026-06-01T08:00:00Z',
    };
    const f = jsonFetch(200, { items: [sale], count: 1, has_more: false } as Paginated<Sale>);
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });

    const page = await client.listStationSales('st1', 'od1');
    const got = page.items[0]!;

    // Money/rate fields stay strings with their exact stored precision.
    expect(typeof got.unit_price).toBe('string');
    expect(got.unit_price).toBe('2950.00');
    expect(got.gross_amount).toBe('3642372.60');
    expect(got.tax_rate).toBe('0.16');
    expect(got.net_amount).toBe('3139977.10');
    // No precision loss / reformat: the trailing zeros that a number coercion
    // would drop are preserved byte-for-byte.
    expect(got.unit_price).not.toBe(String(Number(got.unit_price)));
    expect(got.net_amount).not.toBe(String(Number(got.net_amount)));
  });

  it("preserves a payment's amount as a decimal string through recordPayment", async () => {
    const payment: Payment = {
      id: 'pay1',
      station_id: 'st1',
      tender_type: 'cash',
      amount: '15000.00',
      received_by: 'u1',
      received_at: '2026-06-01T18:00:00Z',
      status: 'posted',
    };
    const f = jsonFetch(201, payment);
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });

    const got = await client.recordPayment('sh1', { tender_type: 'cash', amount: '15000.00' });
    expect(typeof got.amount).toBe('string');
    expect(got.amount).toBe('15000.00');

    // The request body carries the amount as a string too — not a coerced number.
    const sentBody = JSON.parse(String(callArgs(f).init.body)) as { amount: unknown };
    expect(typeof sentBody.amount).toBe('string');
    expect(sentBody.amount).toBe('15000.00');
  });

  it('does not strip trailing zeros from a high-precision litre string', async () => {
    // "25800.000" must survive verbatim — Number("25800.000") would drop them.
    const f = jsonFetch(200, {
      shift_id: 'sh1',
      tendered: '25800.000',
      recognized: '25800.000',
      variance: '0.000',
      over_threshold: false,
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const recon = await client.getShiftPaymentReconciliation('sh1');
    expect(recon.tendered).toBe('25800.000');
    expect(recon.variance).toBe('0.000');
  });
});

describe('SDK Paginated<T> envelope contract', () => {
  it('returns the items/count/has_more envelope verbatim', async () => {
    const envelope: Paginated<Payment> = {
      items: [],
      count: 0,
      limit: 25,
      offset: 50,
      has_more: true,
    };
    const f = jsonFetch(200, envelope);
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });

    const page = await client.listShiftPayments('sh1', { limit: 25, offset: 50 });
    expect(page.items).toEqual([]);
    expect(page.count).toBe(0);
    expect(page.has_more).toBe(true);
    expect(page.limit).toBe(25);
    expect(page.offset).toBe(50);
  });

  it('forwards limit and offset as query params on a paged list', async () => {
    const f = jsonFetch(200, { items: [], count: 0, has_more: false });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });

    await client.listShiftPayments('sh1', { limit: 10, offset: 20 });
    const u = new URL(callArgs(f).url);
    expect(u.pathname).toBe('/api/v1/shifts/sh1/payments');
    expect(u.searchParams.get('limit')).toBe('10');
    expect(u.searchParams.get('offset')).toBe('20');
  });

  it('omits limit/offset query params when neither is supplied', async () => {
    const f = jsonFetch(200, { items: [], count: 0, has_more: false });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });

    await client.listShiftPayments('sh1');
    const u = new URL(callArgs(f).url);
    expect(u.search).toBe('');
  });

  it('preserves has_more:false so a caller stops paging at the tail', async () => {
    const f = jsonFetch(200, { items: [{}], count: 1, has_more: false });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    const page = await client.listShiftPayments('sh1');
    expect(page.has_more).toBe(false);
  });
});

describe('SDK 401 + error mapping contract', () => {
  it('fires onUnauthorized exactly once on a 401 and still rejects with SdkError', async () => {
    const onUnauthorized = vi.fn();
    const f = jsonFetch(401, { error: 'token expired' });
    const client = new Client({
      baseURL: 'http://api.test',
      fetch: f as unknown as typeof fetch,
      onUnauthorized,
    });
    await expect(client.listShiftPayments('sh1')).rejects.toBeInstanceOf(SdkError);
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });

  it('maps a 422 business error body onto the SdkError message + body + status', async () => {
    const f = jsonFetch(422, {
      error: 'charge would exceed the credit limit',
      code: 'credit_limit',
    });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    try {
      await client.recordPayment('sh1', { tender_type: 'credit', amount: '999999.00' });
      expect.unreachable('expected the request to reject');
    } catch (err) {
      expect(err).toBeInstanceOf(SdkError);
      const e = err as SdkError;
      expect(e.status).toBe(422);
      expect(e.message).toBe('charge would exceed the credit limit');
      expect(e.body).toEqual({
        error: 'charge would exceed the credit limit',
        code: 'credit_limit',
      });
    }
  });

  it('falls back to "HTTP <status>" when the error body has no error field', async () => {
    const f = jsonFetch(500, { unexpected: true });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await expect(client.listShiftPayments('sh1')).rejects.toMatchObject({
      status: 500,
      message: 'HTTP 500',
    });
  });

  it('does not fire onUnauthorized on a 403 (forbidden, not unauthenticated)', async () => {
    const onUnauthorized = vi.fn();
    const f = jsonFetch(403, { error: 'forbidden' });
    const client = new Client({
      baseURL: 'http://api.test',
      fetch: f as unknown as typeof fetch,
      onUnauthorized,
    });
    await expect(client.listShiftPayments('sh1')).rejects.toMatchObject({ status: 403 });
    expect(onUnauthorized).not.toHaveBeenCalled();
  });

  it('carries X-Request-Id onto the SdkError for log correlation', async () => {
    const f = jsonFetch(409, { error: 'conflict' }, { 'X-Request-Id': 'req-xyz-1' });
    const client = new Client({ baseURL: 'http://api.test', fetch: f as unknown as typeof fetch });
    await expect(client.listShiftPayments('sh1')).rejects.toMatchObject({
      status: 409,
      requestId: 'req-xyz-1',
    });
  });
});
