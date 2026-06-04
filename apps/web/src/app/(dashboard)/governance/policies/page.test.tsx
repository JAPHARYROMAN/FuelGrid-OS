import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError, type ApprovalPolicy, type ApprovalSimulation } from '@fuelgrid/sdk';

const listApprovalPolicies = vi.fn();
const simulateApprovalPolicy = vi.fn();
const createApprovalPolicy = vi.fn();
const updateApprovalPolicy = vi.fn();
const setApprovalPolicyStatus = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listApprovalPolicies: (...args: unknown[]) => listApprovalPolicies(...args),
    simulateApprovalPolicy: (...args: unknown[]) => simulateApprovalPolicy(...args),
    createApprovalPolicy: (...args: unknown[]) => createApprovalPolicy(...args),
    updateApprovalPolicy: (...args: unknown[]) => updateApprovalPolicy(...args),
    setApprovalPolicyStatus: (...args: unknown[]) => setApprovalPolicyStatus(...args),
  },
}));

// usePermission returns this value; null mimics the still-loading state.
let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import GovernancePoliciesPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <GovernancePoliciesPage />
    </QueryClientProvider>,
  );
}

const policy: ApprovalPolicy = {
  id: 'pol-1',
  workflow_type: 'central_price',
  min_amount: '1000.00',
  required_approvals: 2,
  required_role: 'finance_manager',
  status: 'active',
};

describe('GovernancePoliciesPage', () => {
  beforeEach(() => {
    permitted = true;
    listApprovalPolicies.mockReset();
    simulateApprovalPolicy.mockReset();
    createApprovalPolicy.mockReset();
    updateApprovalPolicy.mockReset();
    setApprovalPolicyStatus.mockReset();
  });

  afterEach(() => vi.clearAllMocks());

  it('renders the policy list with its workflow, approvals and role', async () => {
    listApprovalPolicies.mockResolvedValue({ items: [policy], count: 1, has_more: false });
    renderPage();

    expect(await screen.findByText('central_price')).toBeInTheDocument();
    expect(screen.getByText('finance_manager')).toBeInTheDocument();
    // required_approvals rendered.
    expect(screen.getByText('2')).toBeInTheDocument();
  });

  it('shows the empty state when there are no policies', async () => {
    listApprovalPolicies.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No policies yet')).toBeInTheDocument();
  });

  it('shows a no-access error when the list 403s', async () => {
    listApprovalPolicies.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access')).toBeInTheDocument();
  });

  it('simulates a workflow and shows an approval-required outcome', async () => {
    listApprovalPolicies.mockResolvedValue({ items: [policy], count: 1, has_more: false });
    const sim: ApprovalSimulation = {
      workflow_type: 'central_price',
      approval_required: true,
      matched: true,
      required_approvals: 2,
      required_role: 'finance_manager',
      policy_id: 'pol-1',
    };
    simulateApprovalPolicy.mockResolvedValue(sim);
    renderPage();

    await screen.findByText('central_price');
    fireEvent.change(screen.getByLabelText('Workflow type'), {
      target: { value: 'central_price' },
    });
    fireEvent.change(screen.getByLabelText('Amount (optional)'), { target: { value: '1500' } });
    fireEvent.click(screen.getByRole('button', { name: /^simulate$/i }));

    expect(await screen.findByText('Approval required')).toBeInTheDocument();
    await waitFor(() =>
      expect(simulateApprovalPolicy).toHaveBeenCalledWith({
        workflow_type: 'central_price',
        amount: '1500',
      }),
    );
  });

  it('simulates a workflow with no matching policy and shows no-approval outcome', async () => {
    listApprovalPolicies.mockResolvedValue({ items: [], count: 0, has_more: false });
    const sim: ApprovalSimulation = {
      workflow_type: 'unknown_flow',
      approval_required: false,
      matched: false,
      required_approvals: 1,
      required_role: null,
      policy_id: null,
    };
    simulateApprovalPolicy.mockResolvedValue(sim);
    renderPage();

    await screen.findByText('No policies yet');
    fireEvent.change(screen.getByLabelText('Workflow type'), { target: { value: 'unknown_flow' } });
    fireEvent.click(screen.getByRole('button', { name: /^simulate$/i }));

    expect(await screen.findByText('No approval required')).toBeInTheDocument();
  });

  it('disables manage controls when the user lacks the permission', async () => {
    permitted = false;
    listApprovalPolicies.mockResolvedValue({ items: [policy], count: 1, has_more: false });
    renderPage();

    await screen.findByText('central_price');
    // Simulate button is disabled without approval_policy.manage.
    expect(screen.getByRole('button', { name: /^simulate$/i })).toBeDisabled();
  });

  it('edits a policy through the edit dialog', async () => {
    listApprovalPolicies.mockResolvedValue({ items: [policy], count: 1, has_more: false });
    updateApprovalPolicy.mockResolvedValue({ ...policy, required_approvals: 3 });
    renderPage();

    await screen.findByText('central_price');
    fireEvent.click(screen.getByRole('button', { name: /^edit$/i }));

    // The dialog is seeded from the policy.
    const approvals = await screen.findByLabelText('Required approvals');
    expect((approvals as HTMLInputElement).value).toBe('2');
    fireEvent.change(approvals, { target: { value: '3' } });
    fireEvent.click(screen.getByRole('button', { name: /save changes/i }));

    await waitFor(() =>
      expect(updateApprovalPolicy).toHaveBeenCalledWith('pol-1', {
        workflow_type: 'central_price',
        min_amount: '1000.00',
        required_approvals: 3,
        required_role: 'finance_manager',
      }),
    );
  });

  it('disables an active policy from the list', async () => {
    listApprovalPolicies.mockResolvedValue({ items: [policy], count: 1, has_more: false });
    setApprovalPolicyStatus.mockResolvedValue({ ...policy, status: 'archived' });
    renderPage();

    await screen.findByText('central_price');
    fireEvent.click(screen.getByRole('button', { name: /^disable$/i }));

    await waitFor(() => expect(setApprovalPolicyStatus).toHaveBeenCalledWith('pol-1', 'archived'));
  });

  it('enables a disabled policy from the list', async () => {
    const archived: ApprovalPolicy = { ...policy, status: 'archived' };
    listApprovalPolicies.mockResolvedValue({ items: [archived], count: 1, has_more: false });
    setApprovalPolicyStatus.mockResolvedValue({ ...archived, status: 'active' });
    renderPage();

    await screen.findByText('central_price');
    fireEvent.click(screen.getByRole('button', { name: /^enable$/i }));

    await waitFor(() => expect(setApprovalPolicyStatus).toHaveBeenCalledWith('pol-1', 'active'));
  });
});
