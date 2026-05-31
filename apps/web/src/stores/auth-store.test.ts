import { beforeEach, describe, expect, it } from 'vitest';

import { useAuthStore } from './auth-store';

const PRESENCE_COOKIE = 'fg_authed';

function presenceCookieValue(): string | null {
  const match = document.cookie
    .split('; ')
    .find((c) => c.startsWith(`${PRESENCE_COOKIE}=`));
  if (!match) return null;
  return match.slice(PRESENCE_COOKIE.length + 1);
}

describe('auth-store', () => {
  beforeEach(() => {
    // Reset the singleton store and clear any presence cookie between tests.
    useAuthStore.setState({ token: null, expiresAt: null });
    document.cookie = `${PRESENCE_COOKIE}=; Path=/; SameSite=Lax; Max-Age=0`;
  });

  it('setSession stores the token + expiry and sets the presence cookie', () => {
    useAuthStore.getState().setSession('tok-123', '2030-01-01T00:00:00Z');

    const state = useAuthStore.getState();
    expect(state.token).toBe('tok-123');
    expect(state.expiresAt).toBe('2030-01-01T00:00:00Z');
    expect(presenceCookieValue()).toBe('1');
  });

  it('setSession defaults expiresAt to null when omitted', () => {
    useAuthStore.getState().setSession('tok-456');

    expect(useAuthStore.getState().token).toBe('tok-456');
    expect(useAuthStore.getState().expiresAt).toBeNull();
    expect(presenceCookieValue()).toBe('1');
  });

  it('clearSession clears the token, expiry, and the presence cookie', () => {
    useAuthStore.getState().setSession('tok-789', '2030-01-01T00:00:00Z');
    expect(presenceCookieValue()).toBe('1');

    useAuthStore.getState().clearSession();

    const state = useAuthStore.getState();
    expect(state.token).toBeNull();
    expect(state.expiresAt).toBeNull();
    // Cookie expired via Max-Age=0 -> jsdom drops it entirely.
    expect(presenceCookieValue()).toBeNull();
  });
});
