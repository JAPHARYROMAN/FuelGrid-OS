import { describe, expect, it } from 'vitest';

import manifest from './manifest';

describe('attendant PWA manifest', () => {
  it('installs and launches into the attendant workspace', () => {
    const value = manifest();

    expect(value.id).toBe('/attendant');
    expect(value.start_url).toBe('/attendant');
    expect(value.display).toBe('standalone');
    expect(value.orientation).toBe('portrait');
    expect(value.icons).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ sizes: '192x192' }),
        expect.objectContaining({ sizes: '512x512' }),
      ]),
    );
  });
});
