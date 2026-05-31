import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { SdkError } from '@fuelgrid/sdk';

import { useAuthStore } from '@/stores/auth-store';

const replace = vi.fn();
const login = vi.fn();

vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace }),
  // No ?next= in these tests -> safeRedirect falls back to /command-center.
  useSearchParams: () => new URLSearchParams(),
}));

vi.mock('@/lib/api', () => ({
  api: { login: (...args: unknown[]) => login(...args) },
}));

import { LoginForm } from './login-form';

async function fillCredentials(user: ReturnType<typeof userEvent.setup>) {
  // tenant_slug defaults to "demo"; only email + password are required.
  await user.type(screen.getByLabelText('Email'), 'ops@demo.test');
  await user.type(screen.getByLabelText('Password'), 'sup3rsecret');
}

describe('LoginForm', () => {
  beforeEach(() => {
    replace.mockClear();
    login.mockReset();
    useAuthStore.setState({ token: null, expiresAt: null });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('submits credentials, stores the session, and redirects on success', async () => {
    login.mockResolvedValue({ token: 'tok-abc', expires_at: '2030-01-01T00:00:00Z' });
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
    expect(useAuthStore.getState().token).toBe('tok-abc');
    expect(replace).toHaveBeenCalledWith('/command-center');
  });

  it('surfaces a friendly error and does not set a session on a 401', async () => {
    login.mockRejectedValue(new SdkError('unauthorized', 401, { error: 'bad creds' }));
    const user = userEvent.setup();

    render(<LoginForm />);
    await fillCredentials(user);
    await user.click(screen.getByRole('button', { name: 'Sign in' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Invalid tenant, email, or password.');
    expect(useAuthStore.getState().token).toBeNull();
    expect(replace).not.toHaveBeenCalled();
  });
});
