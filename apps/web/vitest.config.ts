import { fileURLToPath } from 'node:url';

import { defineConfig } from 'vitest/config';

// jsdom gives client-component/util tests a DOM without a real browser.
// The `@` alias mirrors tsconfig paths so test imports match app imports.
export default defineConfig({
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
  },
});
