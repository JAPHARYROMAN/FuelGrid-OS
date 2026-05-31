import { defineConfig } from 'vitest/config';

// jsdom gives component tests a DOM (document/window) without a real browser.
export default defineConfig({
  test: {
    environment: 'jsdom',
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
  },
});
