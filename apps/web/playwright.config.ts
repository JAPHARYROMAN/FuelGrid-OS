import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright e2e config (QA-7 / CICD-3).
 *
 * The specs drive the real Next.js production build (build + `next start`)
 * but the BACKEND is never real: every spec intercepts `/api/**` with
 * Playwright route mocking, so CI needs no Postgres/Redis/API to run the
 * auth smoke. baseURL points at the locally-started server.
 */
const PORT = Number(process.env.PORT ?? 3100);
const baseURL = `http://127.0.0.1:${PORT}`;
const isCI = !!process.env.CI;

export default defineConfig({
  testDir: './e2e',
  // Fail the build if a `test.only` is accidentally committed.
  forbidOnly: isCI,
  retries: isCI ? 1 : 0,
  // Keep CI deterministic; allow local parallelism.
  workers: isCI ? 1 : undefined,
  reporter: isCI
    ? [['github'], ['html', { open: 'never' }], ['list']]
    : [['html', { open: 'never' }], ['list']],
  use: {
    baseURL,
    trace: 'on-first-retry',
    ignoreHTTPSErrors: true,
    // The production bundle ships a strict `script-src 'self'` CSP (see
    // next.config.ts) with no nonce, which blocks Next's inline hydration
    // bootstrap scripts in a headless browser, so the page never becomes
    // interactive. bypassCSP disables CSP ENFORCEMENT IN THE TEST BROWSER
    // ONLY — it does not touch the server headers or the shipped security
    // posture — so the specs can drive the real, hydrated app.
    bypassCSP: true,
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  // Build once, then serve the production bundle. NEXT_PUBLIC_API_URL points
  // at an origin that is never actually reached — all /api/** calls are mocked
  // per-spec — but it must be set so the SDK client builds a stable base URL.
  webServer: {
    command: `pnpm build && pnpm exec next start -p ${PORT}`,
    url: baseURL,
    timeout: 180_000,
    reuseExistingServer: !isCI,
    env: {
      NEXT_PUBLIC_API_URL: baseURL,
    },
  },
});
