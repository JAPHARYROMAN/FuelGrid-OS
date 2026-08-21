import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { SdkError } from '@fuelgrid/sdk';

import { useAuthStore } from '@/stores/auth-store';

const login = vi.fn();
const mePermissions = vi.fn();
const navigateAfterLogin = vi.fn();
const loginDestination = vi.fn();

vi.mock('next/navigation', () => ({
  // No ?next= in these tests -> safeRedirect falls back to /command-center.
  useSearchParams: () => new URLSearchParams(),
}));

vi.mock('@/lib/api', () => ({
  api: {
    login: (...args: unknown[]) => login(...args),
    mePermissions: (...args: unknown[]) => mePermissions(...args),
  },
}));

vi.mock('@/lib/login-destination', () => ({
  loginDestination: (...args: unknown[]) => loginDestination(...args),
  navigateAfterLogin: (...args: unknown[]) => navigateAfterLogin(...args),
}));

import { LoginForm } from './login-form';

async function fillCredentials(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('Tenant'), 'demo');
  await user.type(screen.getByLabelText('Email'), 'ops@demo.test');
  await user.type(screen.getByLabelText('Password'), 'sup3rsecret');
}

describe('LoginForm', () => {
  beforeEach(() => {
    login.mockReset();
    mePermissions.mockReset();
    navigateAfterLogin.mockReset();
    loginDestination.mockReset();
    loginDestination.mockImplementation((_next, access: { permissions: { code: string }[] }) =>
      access.permissions.some((permission) => permission.code === 'company.read')
        ? '/command-center'
        : '/attendant',
    );
    useAuthStore.setState({ authed: false, expiresAt: null });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('submits credentials, records the session hint, and redirects on success', async () => {
    // The BFF strips the token from the login response — the client only sees
    // { mfa_required, expires_at }. Success is "not mfa_required".
    login.mockResolvedValue({ mfa_required: false, expires_at: '2030-01-01T00:00:00Z' });
    mePermissions.mockResolvedValue({
      permissions: [{ code: 'company.read', station_scoped: false }],
      tenant_wide: true,
    });
    const user = userEvent.setup();

    render(<LoginForm />);
    await fillCredentials(user);
    await user.click(screen.getByRole('button', { name: 'Sign in' }));

    await waitFor(() => {
      expect(login).toHaveBeenCalledWith(
        expect.objectContaining({
          email: 'ops@demo.test',
          password: 'sup3rsecret',
          tenant_slug: 'demo',
        }),
      );
    });
    expect(useAuthStore.getState().authed).toBe(true);
    expect(useAuthStore.getState().expiresAt).toBe('2030-01-01T00:00:00Z');
    expect(mePermissions).toHaveBeenCalledOnce();
    expect(navigateAfterLogin).toHaveBeenCalledWith('/command-center');
  });

  it('surfaces a friendly error and does not set a session on a 401', async () => {
    login.mockRejectedValue(new SdkError('unauthorized', 401, { error: 'bad creds' }));
    const user = userEvent.setup();

    render(<LoginForm />);
    await fillCredentials(user);
    await user.click(screen.getByRole('button', { name: 'Sign in' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Invalid tenant, email, or password.');
    expect(useAuthStore.getState().authed).toBe(false);
    expect(mePermissions).not.toHaveBeenCalled();
    expect(navigateAfterLogin).not.toHaveBeenCalled();
  });

  it('routes an attendant-only account into the mobile attendant workspace', async () => {
    login.mockResolvedValue({ mfa_required: false, expires_at: '2030-01-01T00:00:00Z' });
    mePermissions.mockResolvedValue({
      permissions: [
        { code: 'cash.submit', station_scoped: true },
        { code: 'incidents.report', station_scoped: true },
        { code: 'payment.record', station_scoped: true },
        { code: 'pricing.read', station_scoped: true },
        { code: 'reading.edit', station_scoped: true },
        { code: 'shift.open', station_scoped: true },
      ],
      station_ids: ['station-1'],
      tenant_wide: false,
    });
    const user = userEvent.setup();

    render(<LoginForm />);
    await fillCredentials(user);
    await user.click(screen.getByRole('button', { name: 'Sign in' }));

    await waitFor(() => expect(navigateAfterLogin).toHaveBeenCalledWith('/attendant'));
    expect(useAuthStore.getState().authed).toBe(true);
  });
});
