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
    // jest-dom matchers + RTL cleanup between tests.
    setupFiles: ['./test/setup.ts'],
  },
});
