/**
 * Exact decimal-string helpers for meter readings (Mobile Attendant Phase 2).
 *
 * Meter readings travel as exact decimal STRINGS end-to-end (numeric(14,3) ->
 * text); the server compares them in SQL numeric. These helpers give the
 * mobile capture screens the same exactness client-side — BigInt arithmetic
 * on the scaled integer forms, never parseFloat — so "Matched / Higher /
 * Lower than expected" can be decided without binary-float drift.
 */

/** A non-negative plain decimal: digits with an optional fraction ("1500", "1500.25", ".5"). */
const DECIMAL_RE = /^(\d+(\.\d*)?|\.\d+)$/;

/** Whether the string is a plain non-negative decimal a meter can read. */
export function isMeterDecimal(value: string): boolean {
  return DECIMAL_RE.test(value.trim());
}

function parts(value: string): { int: string; frac: string } {
  const [int = '', frac = ''] = value.trim().split('.');
  return { int: int || '0', frac };
}

/**
 * Significant fraction digits, trailing zeros ignored — matching the server's
 * scale check, which compares the numeric VALUE against the meter precision:
 * "1.50" has one significant fraction digit so it fits a 1dp meter, while
 * "1.55" has two and would be rejected with a 422.
 */
export function meterFractionDigits(value: string): number {
  return parts(value).frac.replace(/0+$/, '').length;
}

/** Scale both decimals to a common integer form for exact comparison. */
function scaled(a: string, b: string): { ia: bigint; ib: bigint; scale: number } {
  const pa = parts(a);
  const pb = parts(b);
  const scale = Math.max(pa.frac.length, pb.frac.length);
  return {
    ia: BigInt(pa.int + pa.frac.padEnd(scale, '0')),
    ib: BigInt(pb.int + pb.frac.padEnd(scale, '0')),
    scale,
  };
}

/**
 * Exact comparison of two non-negative decimal strings:
 * -1 when a < b, 0 when equal (numerically — "1500" == "1500.000"), 1 when a > b.
 */
export function compareMeterDecimals(a: string, b: string): -1 | 0 | 1 {
  const { ia, ib } = scaled(a, b);
  return ia < ib ? -1 : ia > ib ? 1 : 0;
}

/**
 * Exact a - b as a decimal string (trailing fraction zeros trimmed, "-"
 * prefix when negative). Both inputs must satisfy isMeterDecimal.
 */
export function subtractMeterDecimals(a: string, b: string): string {
  const { ia, ib, scale } = scaled(a, b);
  let diff = ia - ib;
  const negative = diff < 0n;
  if (negative) diff = -diff;
  const digits = diff.toString().padStart(scale + 1, '0');
  const int = digits.slice(0, digits.length - scale) || '0';
  const frac = scale > 0 ? digits.slice(digits.length - scale).replace(/0+$/, '') : '';
  return `${negative ? '-' : ''}${int}${frac ? `.${frac}` : ''}`;
}

/** Render a non-negative scaled BigInt back to a trimmed decimal string. */
function fromScaled(value: bigint, scale: number): string {
  const digits = value.toString().padStart(scale + 1, '0');
  const int = digits.slice(0, digits.length - scale) || '0';
  const frac = scale > 0 ? digits.slice(digits.length - scale).replace(/0+$/, '') : '';
  return `${int}${frac ? `.${frac}` : ''}`;
}

/**
 * Exact a + b as a decimal string (trailing fraction zeros trimmed). Both
 * inputs must satisfy isMeterDecimal — used to total litres across nozzles
 * on the closing confirmation step (Phase 3) without binary-float drift.
 */
export function addMeterDecimals(a: string, b: string): string {
  const { ia, ib, scale } = scaled(a, b);
  return fromScaled(ia + ib, scale);
}

/**
 * Exact value × factor (a small non-negative INTEGER factor) as a decimal
 * string. Used by the closing screen's high-delta heuristic ("more than 10×
 * the median of the other nozzles") — the multiply happens on the scaled
 * BigInt, never a JS number.
 */
export function multiplyMeterDecimal(value: string, factor: number): string {
  if (!Number.isInteger(factor) || factor < 0) {
    throw new Error('multiplyMeterDecimal: factor must be a non-negative integer');
  }
  const { int, frac } = parts(value);
  return fromScaled(BigInt(int + frac) * BigInt(factor), frac.length);
}
