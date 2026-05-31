import { describe, expect, it } from 'vitest';

import { formatLitres, formatMoney, parseDecimal, sumMoney } from '../lib/money';

describe('money formatters', () => {
  it('formats a money decimal string with grouping + 2dp', () => {
    expect(formatMoney('2950.00')).toBe('2,950.00');
    expect(formatMoney('1234567.5')).toBe('1,234,567.50');
  });

  it('formats litres as whole numbers by default', () => {
    expect(formatLitres('25800.000')).toBe('25,800');
    expect(formatLitres('25800.5', { maximumFractionDigits: 1 })).toBe('25,800.5');
  });

  it('renders a fallback for null/empty/unparseable input', () => {
    expect(formatMoney(null)).toBe('—');
    expect(formatMoney('')).toBe('—');
    expect(formatMoney('abc', { fallback: 'n/a' })).toBe('n/a');
    expect(formatLitres(undefined, { fallback: '0' })).toBe('0');
  });

  it('accepts a number as well as a string', () => {
    expect(formatMoney(10)).toBe('10.00');
    expect(parseDecimal('3.14')).toBeCloseTo(3.14);
  });

  it('sums money strings decimal-safely without float drift', () => {
    // 0.1 + 0.2 in float is 0.30000000000000004; integer-cents avoids that.
    expect(sumMoney(['0.10', '0.20'])).toBe('0.30');
    expect(sumMoney(['100.25', '200.75', '0.00'])).toBe('301.00');
    expect(sumMoney(['10.00', null, '', 'abc', '5.50'])).toBe('15.50');
    expect(sumMoney([])).toBe('0.00');
    expect(sumMoney(['-5.00', '2.50'])).toBe('-2.50');
  });
});
