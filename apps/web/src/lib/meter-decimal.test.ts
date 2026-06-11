import { describe, expect, it } from 'vitest';

import {
  addMeterDecimals,
  compareMeterDecimals,
  isMeterDecimal,
  meterFractionDigits,
  multiplyMeterDecimal,
  subtractMeterDecimals,
} from './meter-decimal';

describe('isMeterDecimal', () => {
  it('accepts plain non-negative decimals', () => {
    expect(isMeterDecimal('1500')).toBe(true);
    expect(isMeterDecimal('1500.250')).toBe(true);
    expect(isMeterDecimal('.5')).toBe(true);
    expect(isMeterDecimal(' 1500.25 ')).toBe(true);
  });

  it('rejects signs, grouping, exponents, and junk', () => {
    expect(isMeterDecimal('')).toBe(false);
    expect(isMeterDecimal('-5')).toBe(false);
    expect(isMeterDecimal('+5')).toBe(false);
    expect(isMeterDecimal('1,500')).toBe(false);
    expect(isMeterDecimal('1e3')).toBe(false);
    expect(isMeterDecimal('12.3.4')).toBe(false);
    expect(isMeterDecimal('abc')).toBe(false);
  });
});

describe('meterFractionDigits', () => {
  it('counts significant fraction digits, ignoring trailing zeros', () => {
    expect(meterFractionDigits('1500')).toBe(0);
    expect(meterFractionDigits('1500.0')).toBe(0);
    expect(meterFractionDigits('1500.50')).toBe(1);
    expect(meterFractionDigits('1500.255')).toBe(3);
  });
});

describe('compareMeterDecimals', () => {
  it('compares exactly across different scales', () => {
    expect(compareMeterDecimals('1500', '1500.000')).toBe(0);
    expect(compareMeterDecimals('1500.001', '1500')).toBe(1);
    expect(compareMeterDecimals('1499.999', '1500')).toBe(-1);
  });

  it('does not suffer binary-float drift on long figures', () => {
    // 9007199254740993 is not representable as a float64.
    expect(compareMeterDecimals('9007199254740993', '9007199254740992')).toBe(1);
    expect(compareMeterDecimals('0.1', '0.10')).toBe(0);
  });
});

describe('subtractMeterDecimals', () => {
  it('subtracts exactly and trims trailing fraction zeros', () => {
    expect(subtractMeterDecimals('1500.250', '1500')).toBe('0.25');
    expect(subtractMeterDecimals('1510', '1500.500')).toBe('9.5');
    expect(subtractMeterDecimals('1500.000', '1500')).toBe('0');
  });

  it('marks negative differences', () => {
    expect(subtractMeterDecimals('1499', '1500')).toBe('-1');
  });
});

describe('addMeterDecimals', () => {
  it('adds exactly across different scales and trims trailing zeros', () => {
    expect(addMeterDecimals('1500.250', '99.75')).toBe('1600');
    expect(addMeterDecimals('0.1', '0.2')).toBe('0.3'); // no binary-float 0.30000000000000004
    expect(addMeterDecimals('120.25', '0')).toBe('120.25');
  });
});

describe('multiplyMeterDecimal', () => {
  it('multiplies exactly by a small integer factor', () => {
    expect(multiplyMeterDecimal('120.25', 10)).toBe('1202.5');
    expect(multiplyMeterDecimal('0.1', 3)).toBe('0.3');
    expect(multiplyMeterDecimal('500', 0)).toBe('0');
  });

  it('rejects non-integer factors', () => {
    expect(() => multiplyMeterDecimal('1', 1.5)).toThrow();
  });
});
