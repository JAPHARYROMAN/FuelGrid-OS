import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError, type Expense } from '@fuelgrid/sdk';

const listExpenses = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listExpenses: (...args: unknown[]) => listExpenses(...args),
    expensesPdf: vi.fn(),
    createExpense: vi.fn(),
    submitExpense: vi.fn(),
    approveExpense: vi.fn(),
    postExpense: vi.fn(),
  },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import ExpensesPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ExpensesPage />
    </QueryClientProvider>,
  );
}

const submittedExpense: Expense = {
  id: 'exp-1',
  expense_date: '2026-02-01',
  amount: '250.00',
  account_key: 'operating_expense',
  payment_mode: 'cash',
  payee: 'City Power',
  status: 'submitted',
} as Expense;

describe('ExpensesPage', () => {
  beforeEach(() => {
    permitted = true;
    listExpenses.mockReset();
  });

  afterEach(() => vi.clearAllMocks());

  it('renders the expense list with status and amount', async () => {
    listExpenses.mockResolvedValue({ items: [submittedExpense], count: 1, has_more: false });
    renderPage();

    expect(await screen.findByText('City Power')).toBeInTheDocument();
    expect(screen.getByText('submitted')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^approve$/i })).toBeEnabled();
  });

  it('shows the empty state when there are no expenses', async () => {
    listExpenses.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No expenses')).toBeInTheDocument();
  });

  it('disables create + approve controls when the user lacks finance permissions', async () => {
    permitted = false;
    listExpenses.mockResolvedValue({ items: [submittedExpense], count: 1, has_more: false });
    renderPage();

    await screen.findByText('City Power');
    expect(screen.getByRole('button', { name: /new expense/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /^approve$/i })).toBeDisabled();
  });

  it('shows a no-access error when the list 403s', async () => {
    listExpenses.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access')).toBeInTheDocument();
  });
});
