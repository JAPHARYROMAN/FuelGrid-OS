import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';

import { useAuthStore } from '@/stores/auth-store';

const replace = vi.fn();

// ProtectedRoute reads the current path and replaces the URL via next/navigation.
vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace }),
  usePathname: () => '/finance',
}));

import { ProtectedRoute } from './protected-route';

describe('ProtectedRoute', () => {
  beforeEach(() => {
    replace.mockClear();
    useAuthStore.setState({ authed: false, expiresAt: null, hydrated: false });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('shows the session check placeholder until the store has hydrated', () => {
    render(
      <ProtectedRoute>
        <div>secret dashboard</div>
      </ProtectedRoute>,
    );

    expect(screen.queryByText('secret dashboard')).not.toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
  });

  it('redirects to /login with the current path as ?next when unauthenticated', () => {
    useAuthStore.setState({ hydrated: true, authed: false });

    render(
      <ProtectedRoute>
        <div>secret dashboard</div>
      </ProtectedRoute>,
    );

    expect(replace).toHaveBeenCalledWith('/login?next=%2Ffinance');
    expect(screen.queryByText('secret dashboard')).not.toBeInTheDocument();
  });

  it('renders children when hydrated and authenticated', () => {
    useAuthStore.setState({ hydrated: true, authed: true });

    render(
      <ProtectedRoute>
        <div>secret dashboard</div>
      </ProtectedRoute>,
    );

    expect(screen.getByText('secret dashboard')).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
  });
});
