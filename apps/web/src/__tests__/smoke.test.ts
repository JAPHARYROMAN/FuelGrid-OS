import { describe, expect, it } from 'vitest';

// Smoke test: proves the vitest + jsdom harness is wired for the web app.
// Kept intentionally trivial — real route/component coverage is a later wave.
describe('web harness smoke', () => {
  it('runs in a jsdom environment (window/document are available)', () => {
    expect(typeof window).toBe('object');
    expect(typeof document).toBe('object');
  });

  it('normalizes an API base URL by stripping a single trailing slash', () => {
    // Mirrors the baseURL derivation in src/lib/api.ts without importing the
    // client singleton (which pulls in the auth store / 'use client').
    const normalize = (raw: string | undefined) =>
      raw?.replace(/\/$/, '') ?? 'http://localhost:8080';

    expect(normalize('http://api.test/')).toBe('http://api.test');
    expect(normalize('http://api.test')).toBe('http://api.test');
    expect(normalize(undefined)).toBe('http://localhost:8080');
  });
});
