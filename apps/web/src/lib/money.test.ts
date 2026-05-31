import { describe, expect, it } from 'vitest';

import { formatLitres, formatMoney, parseDecimal, sumMoney } from './money';

// The formatter internals are exhaustively tested in packages/ui. This suite
// only guards the web-app re-export path (@/lib/money) so a broken/renamed
// re-export is caught here, and confirms decimal STRINGS render without NaN.
describe('money re-export (@/lib/money)', () => {
  it('formats decimal-string money with grouping + 2dp', () => {
    expect(formatMoney('2950.00')).toBe('2,950.00');
    expect(formatMoney('1234567.5')).toBe('1,234,567.50');
  });

  it('formats decimal-string litres as whole numbers by default', () => {
    expect(formatLitres('25800.000')).toBe('25,800');
  });

  it('never renders NaN — unparseable input uses the fallback', () => {
    expect(formatMoney('abc')).toBe('—');
    expect(formatMoney(null)).toBe('—');
    expect(formatMoney('')).toBe('—');
  });

  it('exposes parseDecimal + sumMoney through the re-export', () => {
    expect(parseDecimal('3.14')).toBeCloseTo(3.14);
    expect(sumMoney(['0.10', '0.20'])).toBe('0.30');
  });
});
