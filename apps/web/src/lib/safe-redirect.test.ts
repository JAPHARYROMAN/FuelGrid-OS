import { describe, expect, it } from 'vitest';

import { safeRedirect } from './safe-redirect';

describe('safeRedirect (open-redirect guard)', () => {
  it('allows an internal absolute path', () => {
    expect(safeRedirect('/finance')).toBe('/finance');
    expect(safeRedirect('/stations/abc/pumps/1')).toBe('/stations/abc/pumps/1');
    expect(safeRedirect('/finance?tab=close#section')).toBe('/finance?tab=close#section');
  });

  it('falls back to the command center for empty / missing input', () => {
    expect(safeRedirect(null)).toBe('/command-center');
    expect(safeRedirect(undefined)).toBe('/command-center');
    expect(safeRedirect('')).toBe('/command-center');
  });

  it('rejects absolute external URLs', () => {
    expect(safeRedirect('https://evil.com')).toBe('/command-center');
    expect(safeRedirect('http://evil.com/path')).toBe('/command-center');
  });

  it('rejects protocol-relative URLs', () => {
    expect(safeRedirect('//evil.com')).toBe('/command-center');
    expect(safeRedirect('//evil.com/finance')).toBe('/command-center');
  });

  it('rejects backslash-tricked protocol-relative URLs', () => {
    // Some browsers normalise "/\" to "//" and resolve it off-origin.
    expect(safeRedirect('/\\evil.com')).toBe('/command-center');
  });

  it('rejects scheme/javascript payloads that are not leading-slash paths', () => {
    expect(safeRedirect('javascript:alert(1)')).toBe('/command-center');
    expect(safeRedirect('mailto:a@b.c')).toBe('/command-center');
    expect(safeRedirect('finance')).toBe('/command-center');
  });
});
