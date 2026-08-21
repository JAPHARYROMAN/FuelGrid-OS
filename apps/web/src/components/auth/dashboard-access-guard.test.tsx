import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';

const replace = vi.fn();
const refetch = vi.fn();
const usePermissions = vi.fn();

vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace }),
}));

vi.mock('@/hooks/use-permissions', () => ({
  usePermissions: () => usePermissions(),
}));

import { DashboardAccessGuard } from './dashboard-access-guard';

describe('DashboardAccessGuard', () => {
  beforeEach(() => {
    replace.mockReset();
    refetch.mockReset();
  });

  it('does not render the desktop surface while access is loading', () => {
    usePermissions.mockReturnValue({ isLoading: true, isError: false, refetch });
    render(
      <DashboardAccessGuard>
        <div>desktop surface</div>
      </DashboardAccessGuard>,
    );
    expect(screen.queryByText('desktop surface')).not.toBeInTheDocument();
    expect(screen.getByText('Checking access…')).toBeInTheDocument();
  });

  it('redirects attendant-only users without rendering the desktop surface', () => {
    usePermissions.mockReturnValue({
      data: {
        permissions: [],
        roles: ['attendant'],
        tenant_wide: false,
        is_attendant_only: true,
      },
      isLoading: false,
      isError: false,
      refetch,
    });
    render(
      <DashboardAccessGuard>
        <div>desktop surface</div>
      </DashboardAccessGuard>,
    );
    expect(replace).toHaveBeenCalledWith('/attendant');
    expect(screen.queryByText('desktop surface')).not.toBeInTheDocument();
  });

  it('renders the desktop surface for broader roles', () => {
    usePermissions.mockReturnValue({
      data: {
        permissions: [{ code: 'company.read', station_scoped: false }],
        roles: ['station_manager'],
        tenant_wide: true,
        is_attendant_only: false,
      },
      isLoading: false,
      isError: false,
      refetch,
    });
    render(
      <DashboardAccessGuard>
        <div>desktop surface</div>
      </DashboardAccessGuard>,
    );
    expect(screen.getByText('desktop surface')).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
  });
});
