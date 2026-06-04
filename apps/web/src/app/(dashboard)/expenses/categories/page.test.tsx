import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError, type ExpenseCategory } from '@fuelgrid/sdk';

const listExpenseCategories = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listExpenseCategories: (...args: unknown[]) => listExpenseCategories(...args),
    createExpenseCategory: vi.fn(),
    updateExpenseCategory: vi.fn(),
    setExpenseCategoryStatus: vi.fn(),
  },
}));

vi.mock('@/lib/toast', () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import ExpenseCategoriesPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ExpenseCategoriesPage />
    </QueryClientProvider>,
  );
}

const category: ExpenseCategory = {
  id: 'cat-1',
  name: 'Utilities',
  account_key: 'operating_expense',
  approval_threshold: '5000.00',
  status: 'active',
};

describe('ExpenseCategoriesPage', () => {
  beforeEach(() => {
    permitted = true;
    listExpenseCategories.mockReset();
  });

  afterEach(() => vi.clearAllMocks());

  it('renders categories with their account mapping and approval threshold', async () => {
    listExpenseCategories.mockResolvedValue({ items: [category], count: 1, has_more: false });
    renderPage();

    expect(await screen.findByText('Utilities')).toBeInTheDocument();
    expect(screen.getByText('operating_expense')).toBeInTheDocument();
    expect(screen.getByText(/5,?000/)).toBeInTheDocument();
    expect(screen.getByText('active')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /deactivate/i })).toBeEnabled();
  });

  it('shows the empty state when there are no categories', async () => {
    listExpenseCategories.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No categories')).toBeInTheDocument();
  });

  it('disables management controls when the user lacks expense.manage', async () => {
    permitted = false;
    listExpenseCategories.mockResolvedValue({ items: [category], count: 1, has_more: false });
    renderPage();

    await screen.findByText('Utilities');
    expect(screen.getByRole('button', { name: /new category/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /^edit$/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /deactivate/i })).toBeDisabled();
  });

  it('shows a no-access error when the list 403s', async () => {
    listExpenseCategories.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access')).toBeInTheDocument();
  });
});
