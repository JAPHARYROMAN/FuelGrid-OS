/**
 * Money/litre formatting helpers for the decimal-string contract.
 *
 * The Go backend emits every money/litre/rate field as an exact decimal
 * STRING (e.g. "2950.00", "25800.000"). The frontend must never run
 * Number()/.toFixed() arithmetic on these for display — that re-introduces the
 * binary-float drift the string contract exists to avoid. These helpers parse
 * a value for *display only* via Intl.NumberFormat and never feed the parsed
 * float back into business logic.
 */

/** Parse a decimal string (or number) for display. Returns NaN when unparseable. */
export function parseDecimal(value: string | number | null | undefined): number {
  if (value == null || value === '') return NaN;
  return typeof value === 'number' ? value : Number(value);
}

export interface FormatOptions {
  /** Minimum fraction digits (default 2 for money). */
  minimumFractionDigits?: number;
  /** Maximum fraction digits (default = minimum). */
  maximumFractionDigits?: number;
  /** Rendered when the value is null/empty/unparseable. */
  fallback?: string;
}

/**
 * Format a money decimal string for display, e.g. "2950.00" -> "2,950.00".
 * Currency-symbol-free by design — the tenant's currency is shown alongside
 * by the caller, not baked into every figure.
 */
export function formatMoney(
  value: string | number | null | undefined,
  opts: FormatOptions = {},
): string {
  const n = parseDecimal(value);
  if (!Number.isFinite(n)) return opts.fallback ?? '—';
  const min = opts.minimumFractionDigits ?? 2;
  return new Intl.NumberFormat(undefined, {
    minimumFractionDigits: min,
    maximumFractionDigits: opts.maximumFractionDigits ?? min,
  }).format(n);
}

/**
 * Format a litre decimal string for display, e.g. "25800.000" -> "25,800".
 * Litres default to whole numbers; pass fraction digits for finer figures.
 */
export function formatLitres(
  value: string | number | null | undefined,
  opts: FormatOptions = {},
): string {
  const n = parseDecimal(value);
  if (!Number.isFinite(n)) return opts.fallback ?? '—';
  return new Intl.NumberFormat(undefined, {
    minimumFractionDigits: opts.minimumFractionDigits ?? 0,
    maximumFractionDigits: opts.maximumFractionDigits ?? opts.minimumFractionDigits ?? 0,
  }).format(n);
}

/**
 * Decimal-safe sum of money strings, returned as a 2dp decimal string.
 *
 * Sums in integer cents (rounding each input to the nearest cent) so a long
 * column of "x.xx" figures cannot drift the way Number()+reduce does. Inputs
 * that are null/empty/unparseable contribute 0. Use this only when the server
 * has not already provided the aggregate; prefer a server-provided total.
 */
export function sumMoney(values: Array<string | number | null | undefined>): string {
  let cents = 0;
  for (const v of values) {
    const n = parseDecimal(v);
    if (Number.isFinite(n)) cents += Math.round(n * 100);
  }
  const sign = cents < 0 ? '-' : '';
  const abs = Math.abs(cents);
  return `${sign}${Math.floor(abs / 100)}.${String(abs % 100).padStart(2, '0')}`;
}
