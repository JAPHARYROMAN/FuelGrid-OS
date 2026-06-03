import '@testing-library/jest-dom/vitest';

import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

// jsdom lacks ResizeObserver, which recharts' ResponsiveContainer constructs on
// mount. Provide a no-op so chart-bearing pages (e.g. the aging dashboards) can
// be rendered under RTL without an unhandled error.
if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
}

// Unmount React trees and reset jsdom between tests so component tests don't
// leak DOM/state into one another.
afterEach(() => {
  cleanup();
});
