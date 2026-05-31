/**
 * The upstream Go API origin the BFF proxy forwards to (WEB-001 / Wave-10).
 *
 * This runs SERVER-SIDE only, so it prefers server-only env vars (API_ORIGIN /
 * API_URL) and falls back to the existing NEXT_PUBLIC_API_URL the app already
 * sets, then to the local dev default. NEXT_PUBLIC_API_URL no longer needs to
 * be the browser's API base (the browser now talks to the same-origin /api/bff
 * proxy) — it is reused here only as a convenient existing fallback so no new
 * env wiring is required to keep dev/CI working.
 */
export function apiOrigin(): string {
  const raw =
    process.env.API_ORIGIN ??
    process.env.API_URL ??
    process.env.NEXT_PUBLIC_API_URL ??
    'http://localhost:8080';
  return raw.replace(/\/$/, '');
}
