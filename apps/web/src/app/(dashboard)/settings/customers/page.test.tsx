import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { Customer } from '@fuelgrid/sdk';

const listCustomers = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listCustomers: (...args: unknown[]) => listCustomers(...args),
    customersPdf: vi.fn(),
    createCustomer: vi.fn(),
    updateCustomer: vi.fn(),
    setCustomerStatus: vi.fn(),
  },
}));

// Per-code permission map so the test can assert the page wires the correct
// codes: New/Edit ride credit.manage, the status toggle rides customer.manage.
let permissions: Record<string, boolean> = {};
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: (code: string) => permissions[code] ?? false,
}));

import CustomersSettingsPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <CustomersSettingsPage />
    </QueryClientProvider>,
  );
}

const sampleCustomer: Customer = {
  id: 'c1',
  code: 'FLEET',
  name: 'Fleet Co',
  contact_name: 'Sam',
  contact_email: null,
  contact_phone: null,
  credit_limit: '100000',
  status: 'active',
} as unknown as Customer;

describe('CustomersSettingsPage', () => {
  beforeEach(() => {
    permissions = {
      'credit.manage': true,
      'customer.manage': true,
      'customer.read': true,
    };
    listCustomers.mockReset();
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders the customer list with mocked data', async () => {
    listCustomers.mockResolvedValue({ items: [sampleCustomer], count: 1 });
    renderPage();

    expect(await screen.findByText('Fleet Co')).toBeInTheDocument();
    expect(screen.getByText('FLEET')).toBeInTheDocument();
  });

  it('shows the empty state when there are no customers', async () => {
    listCustomers.mockResolvedValue({ items: [], count: 0 });
    renderPage();

    expect(await screen.findByText('No customers yet')).toBeInTheDocument();
  });

  it('disables New/Edit when the user lacks credit.manage', async () => {
    permissions = { 'credit.manage': false, 'customer.manage': true };
    listCustomers.mockResolvedValue({ items: [sampleCustomer], count: 1 });
    renderPage();

    await screen.findByText('Fleet Co');
    expect(screen.getByRole('button', { name: /new customer/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /^edit$/i })).toBeDisabled();
    // The status toggle (customer.manage) remains enabled.
    expect(screen.getByRole('button', { name: /suspend/i })).toBeEnabled();
  });

  it('disables the status toggle when the user lacks customer.manage', async () => {
    permissions = { 'credit.manage': true, 'customer.manage': false };
    listCustomers.mockResolvedValue({ items: [sampleCustomer], count: 1 });
    renderPage();

    await screen.findByText('Fleet Co');
    expect(screen.getByRole('button', { name: /suspend/i })).toBeDisabled();
    // New/Edit (credit.manage) remain enabled.
    expect(screen.getByRole('button', { name: /new customer/i })).toBeEnabled();
  });
});
