import { beforeEach, describe, expect, it } from 'vitest';

import { useAuthStore } from './auth-store';

/**
 * The store no longer holds a token (WEB-001 / Wave-10) — that lives only in
 * the httpOnly `fg_session` cookie. These tests cover the non-sensitive UI
 * hints the store still owns: the `authed` flag and `expiresAt`.
 */
describe('auth-store', () => {
  beforeEach(() => {
    useAuthStore.setState({ authed: false, expiresAt: null });
  });

  it('setAuthed records the session hint + expiry and never stores a token', () => {
    useAuthStore.getState().setAuthed('2030-01-01T00:00:00Z');

    const state = useAuthStore.getState();
    expect(state.authed).toBe(true);
    expect(state.expiresAt).toBe('2030-01-01T00:00:00Z');
    // There is no token field at all — nothing client-readable to steal.
    expect((state as unknown as Record<string, unknown>).token).toBeUndefined();
  });

  it('setAuthed defaults expiresAt to null when omitted', () => {
    useAuthStore.getState().setAuthed();

    expect(useAuthStore.getState().authed).toBe(true);
    expect(useAuthStore.getState().expiresAt).toBeNull();
  });

  it('clearSession resets the hint and expiry', () => {
    useAuthStore.getState().setAuthed('2030-01-01T00:00:00Z');
    expect(useAuthStore.getState().authed).toBe(true);

    useAuthStore.getState().clearSession();

    const state = useAuthStore.getState();
    expect(state.authed).toBe(false);
    expect(state.expiresAt).toBeNull();
  });
});
