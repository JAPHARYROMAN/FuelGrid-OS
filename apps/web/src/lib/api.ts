'use client';

import { Client } from '@fuelgrid/sdk';

import { useAuthStore } from '@/stores/auth-store';

const baseURL = process.env.NEXT_PUBLIC_API_URL?.replace(/\/$/, '') ?? 'http://localhost:8080';

/**
 * Singleton SDK client. The getToken callback reads from the auth store
 * on every request so a token refresh / logout propagates without
 * rebuilding the client.
 */
export const api = new Client({
  baseURL,
  getToken: () => useAuthStore.getState().token,
});
