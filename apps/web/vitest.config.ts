import { fileURLToPath } from 'node:url';

import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

// jsdom gives client-component/util tests a DOM without a real browser.
// The `@` alias mirrors tsconfig paths so test imports match app imports.
// The React plugin transforms JSX/TSX (tsconfig keeps jsx: "preserve" for
// Next.js, so component tests need their own transform here).
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    // Playwright e2e specs live under e2e/ and run with their own runner —
    // keep Vitest from picking them up.
    exclude: ['e2e/**', 'node_modules/**', 'dist/**', '.next/**'],
    // jest-dom matchers + RTL cleanup between tests.
    setupFiles: ['./test/setup.ts'],
    coverage: {
      provider: 'v8',
      // text for local visibility; json-summary so CI / dashboards can read
      // the totals as a machine-readable artifact.
      reporter: ['text', 'json-summary'],
      // RATCHET SCOPE: gate only the modules that have a unit/RTL suite today
      // (the QA-2/QA-3 harness — auth flow, redirect guard, money + redirect
      // utils, session presence store, error reporting). Pulling the 30+
      // not-yet-tested route pages into the denominator would make any floor a
      // fantasy number; scoping keeps the gate a real regression guard for the
      // covered surface. Widen this include as more modules gain tests.
      include: [
        'src/middleware.ts',
        'src/lib/safe-redirect.ts',
        'src/lib/money.ts',
        'src/lib/sentry.ts',
        'src/stores/auth-store.ts',
        'src/components/auth/**/*.{ts,tsx}',
      ],
      // Floors sit a few points under the current measurement
      // (S 71.5 / B 51.2 / F 84.6 / L 76.2 as of this commit) so they catch a
      // regression without failing today. Ratchet upward as coverage grows.
      thresholds: {
        statements: 65,
        branches: 45,
        functions: 78,
        lines: 70,
      },
    },
  },
});
