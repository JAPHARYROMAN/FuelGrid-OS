import { describe, expect, it, vi } from 'vitest';

import { reportError } from '@/lib/sentry';

// reportError is the entry point the React error boundaries call. It MUST be a
// no-op when Sentry was never initialized (no DSN) so dev/CI/build are unaffected
// and a render error never throws a second time from inside the boundary.
describe('reportError (render-error capture)', () => {
  it('is a no-op and never throws when Sentry is not initialized (no DSN)', () => {
    const error = Object.assign(new Error('boom'), { digest: 'abc123' });
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});

    expect(() => reportError(error, { boundary: 'dashboard' })).not.toThrow();

    spy.mockRestore();
  });
});
