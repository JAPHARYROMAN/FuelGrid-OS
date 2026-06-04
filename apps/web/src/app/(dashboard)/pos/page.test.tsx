import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { type OperationsOverview, type Station } from '@fuelgrid/sdk';

// The POS reads stations / operations-overview / products / customers, the
// shift reconciliation + payments, and posts tenders — all via @/lib/api. The
// page-level write gate goes through usePermission. Mock both.
const listStations = vi.fn();
const getOperationsOverview = vi.fn();
const listProducts = vi.fn();
const listCustomers = vi.fn();
const getShiftPaymentReconciliation = vi.fn();
const listShiftPayments = vi.fn();
const recordPayment = vi.fn();

vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...a: unknown[]) => listStations(...a),
    getOperationsOverview: (...a: unknown[]) => getOperationsOverview(...a),
    listProducts: (...a: unknown[]) => listProducts(...a),
    listCustomers: (...a: unknown[]) => listCustomers(...a),
    getShiftPaymentReconciliation: (...a: unknown[]) => getShiftPaymentReconciliation(...a),
    listShiftPayments: (...a: unknown[]) => listShiftPayments(...a),
    recordPayment: (...a: unknown[]) => recordPayment(...a),
  },
}));

vi.mock('@/lib/toast', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({ usePermission: () => permitted }));

// PermissionGate renders children when not explicitly denied; mock it to its
// children so the gated submit button is present in the test tree.
vi.mock('@/components/permission-gate', () => ({
  PermissionGate: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

import PosPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <PosPage />
    </QueryClientProvider>,
  );
}

const station: Station = { id: 'st-1', name: 'Westlands', code: 'WL' } as Station;

function overview(shiftStatus = 'open'): OperationsOverview {
  return {
    station,
    day: { id: 'day-1', station_id: 'st-1', business_date: '2026-06-01', status: 'open' },
    shifts: [
      {
        id: 'sh-1',
        name: 'Morning',
        station_id: 'st-1',
        status: shiftStatus,
        attendants: [],
        nozzle_assignments: [],
        expected_cash: '0.00',
        litres_sold: '0',
        exceptions: [],
        open_exception_count: 0,
      },
    ],
  } as unknown as OperationsOverview;
}

describe('PosPage', () => {
  beforeEach(() => {
    permitted = true;
    listStations.mockResolvedValue({ items: [station], count: 1, has_more: false });
    getOperationsOverview.mockResolvedValue(overview());
    listProducts.mockResolvedValue({
      items: [{ id: 'prod-1', code: 'PMS', name: 'Petrol', default_price: '180.00' }],
      count: 1,
      has_more: false,
    });
    listCustomers.mockResolvedValue({
      items: [{ id: 'cust-1', code: 'AC', name: 'Acme' }],
      count: 1,
      has_more: false,
    });
    getShiftPaymentReconciliation.mockResolvedValue({
      shift_id: 'sh-1',
      tendered: '0.00',
      recognized: '7200.00',
      variance: '-7200.00',
      over_threshold: true,
    });
    listShiftPayments.mockResolvedValue({ items: [], count: 0, has_more: false });
    recordPayment.mockImplementation(
      (_shiftID: string, body: { tender_type: string; amount: string }) =>
        Promise.resolve({
          id: `pay-${Math.random()}`,
          station_id: 'st-1',
          shift_id: 'sh-1',
          tender_type: body.tender_type,
          amount: body.amount,
          received_by: 'u-1',
          received_at: '2026-06-01T09:00:00Z',
          status: 'recorded',
        }),
    );
  });

  afterEach(() => vi.clearAllMocks());

  it('blocks when there is no active operating day', async () => {
    getOperationsOverview.mockResolvedValue({ station, day: null, shifts: [] });
    renderPage();
    expect(await screen.findByText('No active operating day')).toBeInTheDocument();
  });

  it('blocks when there is no open shift', async () => {
    getOperationsOverview.mockResolvedValue(overview('closed'));
    renderPage();
    expect(await screen.findByText('No open shift')).toBeInTheDocument();
  });

  it('shows a no-access state when payment.record is denied', async () => {
    permitted = false;
    renderPage();
    expect(await screen.findByText('No access to the till')).toBeInTheDocument();
  });

  it('surfaces server-computed reconciliation (recognized revenue)', async () => {
    renderPage();
    expect(await screen.findByText('Shift settlement')).toBeInTheDocument();
    // Recognized 7,200 is shown from the reconciliation query (also appears in
    // the variance, so assert at least one match).
    await waitFor(() => expect(screen.getAllByText(/7,?200/).length).toBeGreaterThan(0));
  });

  it('keeps the Record button disabled until tenders sum exactly to the total', async () => {
    renderPage();
    const total = await screen.findByLabelText('Sale total');
    fireEvent.change(total, { target: { value: '100.00' } });

    const record = screen.getByRole('button', { name: /record sale/i });
    expect(record).toBeDisabled();

    // Under-tender keeps it disabled.
    fireEvent.change(screen.getByLabelText('Tender 1 amount'), { target: { value: '60.00' } });
    expect(record).toBeDisabled();
    expect(screen.getByTestId('pos-remaining')).toHaveTextContent('40');

    // Exact balance enables it.
    fireEvent.change(screen.getByLabelText('Tender 1 amount'), { target: { value: '100.00' } });
    await waitFor(() => expect(record).toBeEnabled());
  });

  it('validates a split that sums to the total across multiple tenders', async () => {
    renderPage();
    fireEvent.change(await screen.findByLabelText('Sale total'), { target: { value: '100.00' } });
    fireEvent.change(screen.getByLabelText('Tender 1 amount'), { target: { value: '60.00' } });

    fireEvent.click(screen.getByRole('button', { name: /add tender/i }));
    const lines = await screen.findAllByTestId('tender-line');
    expect(lines).toHaveLength(2);
    fireEvent.change(screen.getByLabelText('Tender 2 amount'), { target: { value: '40.00' } });

    const record = screen.getByRole('button', { name: /record sale/i });
    await waitFor(() => expect(record).toBeEnabled());

    fireEvent.click(record);
    await waitFor(() => expect(recordPayment).toHaveBeenCalledTimes(2));
    expect(recordPayment).toHaveBeenNthCalledWith(
      1,
      'sh-1',
      expect.objectContaining({ tender_type: 'cash', amount: '60.00' }),
    );
    expect(recordPayment).toHaveBeenNthCalledWith(
      2,
      'sh-1',
      expect.objectContaining({ tender_type: 'cash', amount: '40.00' }),
    );
  });

  it('prevents a double-submit (one click → one batch)', async () => {
    // Make the post hang so a second click could race in if unguarded.
    let resolve!: (v: unknown) => void;
    recordPayment.mockImplementation(() => new Promise((r) => (resolve = r)));
    renderPage();
    fireEvent.change(await screen.findByLabelText('Sale total'), { target: { value: '50.00' } });
    fireEvent.change(screen.getByLabelText('Tender 1 amount'), { target: { value: '50.00' } });

    await waitFor(() => expect(screen.getByRole('button', { name: /record sale/i })).toBeEnabled());

    // Re-query before each click so we act on the live node (a re-render can
    // swap the disabled/label), then fire three rapid clicks.
    fireEvent.click(screen.getByRole('button', { name: /record sale/i }));
    fireEvent.click(screen.getByRole('button', { name: /recording/i }));
    fireEvent.click(screen.getByRole('button', { name: /recording/i }));

    // Exactly one tender post was issued despite three clicks (the in-flight
    // submit is guarded; the button is also disabled while pending).
    await waitFor(() => expect(recordPayment).toHaveBeenCalledTimes(1));
    resolve({
      id: 'pay-1',
      station_id: 'st-1',
      shift_id: 'sh-1',
      tender_type: 'cash',
      amount: '50.00',
      received_by: 'u-1',
      received_at: '2026-06-01T09:00:00Z',
      status: 'recorded',
    });
    await waitFor(() => expect(screen.getByText(/Receipt/)).toBeInTheDocument());
  });

  it('requires a customer for a credit tender', async () => {
    renderPage();
    fireEvent.change(await screen.findByLabelText('Sale total'), { target: { value: '100.00' } });
    fireEvent.change(screen.getByLabelText('Tender 1 method'), { target: { value: 'credit' } });
    fireEvent.change(screen.getByLabelText('Tender 1 amount'), { target: { value: '100.00' } });

    const record = screen.getByRole('button', { name: /record sale/i });
    await waitFor(() => expect(record).toBeEnabled());
    fireEvent.click(record);

    // Posting is blocked until a customer is chosen.
    expect(await screen.findByText(/select a customer for the credit tender/i)).toBeInTheDocument();
    expect(recordPayment).not.toHaveBeenCalled();
  });

  it('renders a downloadable receipt after recording', async () => {
    renderPage();
    fireEvent.change(await screen.findByLabelText('Sale total'), { target: { value: '50.00' } });
    fireEvent.change(screen.getByLabelText('Tender 1 amount'), { target: { value: '50.00' } });
    fireEvent.click(screen.getByRole('button', { name: /record sale/i }));

    const receipt = await screen.findByRole('dialog', { name: /sale receipt/i });
    expect(within(receipt).getByText(/SALE RECEIPT/)).toBeInTheDocument();
    expect(within(receipt).getByRole('button', { name: /download/i })).toBeInTheDocument();
  });

  it('lists recorded tenders with their payment status', async () => {
    listShiftPayments.mockResolvedValue({
      items: [
        {
          id: 'pay-1',
          station_id: 'st-1',
          shift_id: 'sh-1',
          tender_type: 'mobile_money',
          amount: '120.00',
          received_by: 'u-1',
          received_at: '2026-06-01T09:00:00Z',
          status: 'recorded',
        },
      ],
      count: 1,
      has_more: false,
    });
    renderPage();
    // "Mobile money" also exists as a tender-method <option>; scope to the
    // recorded-tenders list (a <ul> of recorded payments) to find the badge
    // and its amount in the same row.
    const recordedRow = await screen.findByText(
      (_content, el) => el?.tagName.toLowerCase() === 'span' && el.textContent === 'Mobile money',
    );
    const row = recordedRow.closest('li')!;
    expect(within(row).getByText(/120/)).toBeInTheDocument();
  });
});
