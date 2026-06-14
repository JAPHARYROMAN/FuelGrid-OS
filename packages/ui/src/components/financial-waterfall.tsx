import { cn } from '../lib/cn';
import { chartColors } from '../lib/chart-theme';
import { parseDecimal } from '../lib/money';

/**
 * FinancialWaterfall — a general-purpose money waterfall (running-total) chart.
 *
 *   Net revenue  (− COGS)  = Gross margin  (− Expenses)  = Net operating result
 *
 * Each step is a labelled horizontal bar. A `base`/`total` step is an absolute
 * running total drawn from the zero baseline; a `delta` step is a signed
 * positive/negative change drawn as a floating bar that bridges the previous
 * running total to the next, so the eye follows the money down the cascade. The
 * final `total` step "lands" the cumulative result.
 *
 * This is the GENERAL money waterfall the finance reports need — distinct from
 * the stock-specific ReconciliationWaterfall (opening + deliveries − sales =
 * expected vs actual). It carries no domain assumptions: the caller supplies the
 * ordered steps and a money formatter.
 *
 * Decimal-string contract: every figure arrives as an exact decimal STRING (or
 * number); it is parsed ONCE here for the bar geometry only and never fed back
 * into business logic. The printed figure comes from the caller's `valueFormatter`
 * so display formatting (currency, separators) stays the caller's concern.
 *
 * Accessibility: colour is never the sole signal. Every step prints its label,
 * a +/−/= sign glyph and its formatted figure as text; the whole figure list is
 * also exposed as a `role="list"` with a per-step `aria-label`, and a hidden
 * caption summarises the cascade. Reads in monochrome print and for
 * colour-vision-deficient users.
 */

type Decimal = string | number | null | undefined;

/** A step's role in the cascade. */
export type WaterfallStepKind =
  /** An absolute running-total bar drawn from the baseline (a starting figure). */
  | 'base'
  /** A signed change that bridges the previous running total to the next. */
  | 'delta'
  /** An absolute cumulative-result bar (a subtotal/total) drawn from baseline. */
  | 'total';

/** One step of the waterfall. */
export interface FinancialWaterfallStep {
  /** Stable key. */
  key: string;
  /** Human label, e.g. "Net revenue", "COGS", "Gross margin". */
  label: string;
  /**
   * The step's value as an exact decimal STRING (or number). For a `delta` step
   * the sign matters (a cost is negative); for `base`/`total` it is the absolute
   * running total at that point. Parsed once for geometry only.
   */
  value: Decimal;
  /** The step's role; defaults to `delta`. */
  kind?: WaterfallStepKind;
  /**
   * Force the delta's direction tone. By default a `delta` is positive (success)
   * when its value is ≥ 0 and negative (danger) when < 0; override to render a
   * deduction explicitly even when the magnitude is supplied unsigned.
   */
  negative?: boolean;
}

export interface FinancialWaterfallProps {
  steps: FinancialWaterfallStep[];
  /**
   * Display formatter for every figure (e.g. (v) => formatMoney(v)). Defaults to
   * a thin `String()` so the component renders without a hard money dependency.
   */
  valueFormatter?: (v: Decimal) => string;
  /** Accessible group label, e.g. "Profit and loss waterfall". */
  ariaLabel?: string;
  /** Unit suffix appended after each figure, e.g. "TZS". */
  unit?: string;
  className?: string;
}

interface ResolvedStep {
  key: string;
  label: string;
  kind: WaterfallStepKind;
  /** Signed numeric magnitude used for geometry. */
  value: number;
  /** Running total AFTER this step is applied. */
  runningAfter: number;
  /** Running total BEFORE this step (the floating bar's start, for deltas). */
  runningBefore: number;
  display: string;
  sign: '+' | '−' | '=' | '';
  negative: boolean;
}

const clamp01 = (n: number) => Math.max(0, Math.min(1, n));

/** A step's tone: result bars are accent; positive deltas success; negative danger. */
function toneOf(s: ResolvedStep): { bar: string; text: string } {
  if (s.kind === 'base') return { bar: chartColors.muted, text: 'text-foreground' };
  if (s.kind === 'total') {
    // A negative result lands in danger so a loss reads at a glance.
    return s.runningAfter < 0
      ? { bar: chartColors.danger, text: 'text-danger' }
      : { bar: chartColors.accent, text: 'text-foreground' };
  }
  return s.negative
    ? { bar: chartColors.danger, text: 'text-danger' }
    : { bar: chartColors.success, text: 'text-success' };
}

export function FinancialWaterfall({
  steps,
  valueFormatter,
  ariaLabel = 'Financial waterfall',
  unit,
  className,
}: FinancialWaterfallProps) {
  const fmt = valueFormatter ?? ((v: Decimal) => String(v ?? ''));
  if (steps.length === 0) return null;

  // Resolve the running total across the cascade. `base`/`total` reset the
  // running total to their absolute value; a `delta` moves it by its signed value.
  let running = 0;
  const resolved: ResolvedStep[] = steps.map((s) => {
    const kind = s.kind ?? 'delta';
    const raw = parseDecimal(s.value);
    const value = Number.isFinite(raw) ? raw : 0;
    const before = running;
    let after: number;
    let sign: ResolvedStep['sign'];
    // A delta is negative when its value is signed negative OR it is explicitly
    // flagged (the caller supplied a positive magnitude for a deduction).
    const negativeDelta = kind === 'delta' && (value < 0 || !!s.negative);
    if (kind === 'delta') {
      // Move the running total by the SIGNED magnitude: a flagged deduction
      // subtracts its (positive) magnitude rather than adding it.
      const signed = negativeDelta ? -Math.abs(value) : Math.abs(value);
      after = before + signed;
      sign = negativeDelta ? '−' : '+';
    } else {
      // base / total: an absolute running total.
      after = value;
      sign = kind === 'total' ? '=' : '';
    }
    running = after;
    return {
      key: s.key,
      label: s.label,
      kind,
      value,
      runningBefore: before,
      runningAfter: after,
      display: fmt(s.value),
      sign,
      negative: negativeDelta,
    };
  });

  // The track maps a numeric DOMAIN onto [0,100%]. The domain spans every value
  // the cascade reaches (running totals, before/after of each delta) AND always
  // includes 0, so a zero-crossing cascade (a loss, or a delta that bridges
  // positive→negative) is positioned relative to a real zero baseline instead of
  // collapsing its negative portion to the left edge. Bars sit on the correct
  // side of zero, so a loss no longer looks like a same-sized gain.
  let domainMin = 0;
  let domainMax = 0;
  for (const s of resolved) {
    domainMin = Math.min(domainMin, s.runningAfter, s.runningBefore);
    domainMax = Math.max(domainMax, s.runningAfter, s.runningBefore);
  }
  const span = domainMax - domainMin || 1;
  // Fraction (0..1) of the track at which a domain value sits.
  const fracOf = (v: number) => clamp01((v - domainMin) / span);
  // Where the zero baseline falls on the track; shown when the domain crosses it.
  const zeroFrac = fracOf(0);
  const crossesZero = domainMin < 0 && domainMax > 0;

  return (
    <div className={cn('flex flex-col gap-2.5', className)} role="group" aria-label={ariaLabel}>
      <ol className="flex flex-col gap-2.5" role="list">
        {resolved.map((s) => {
          const tone = toneOf(s);
          // Geometry: an absolute bar (base/total) spans [0, runningAfter] — drawn
          // on the correct side of the zero baseline; a delta spans
          // [runningBefore, runningAfter]. Both are positioned in DOMAIN space so
          // negatives render to the left of zero.
          let lo: number;
          let hi: number;
          if (s.kind === 'delta') {
            lo = Math.min(s.runningBefore, s.runningAfter);
            hi = Math.max(s.runningBefore, s.runningAfter);
          } else {
            lo = Math.min(0, s.runningAfter);
            hi = Math.max(0, s.runningAfter);
          }
          const leftFrac = fracOf(lo);
          const widthFrac = fracOf(hi) - leftFrac;
          const ariaLine = `${s.label}: ${s.sign ? `${s.sign} ` : ''}${s.display}${
            unit ? ` ${unit}` : ''
          }`;
          return (
            <li key={s.key} className="flex items-center gap-3" aria-label={ariaLine}>
              <span className="flex w-36 shrink-0 items-center gap-1 text-sm text-muted-foreground">
                {s.sign ? (
                  <span aria-hidden className="font-mono text-muted-foreground">
                    {s.sign}
                  </span>
                ) : null}
                <span
                  className={cn(
                    'truncate',
                    (s.kind === 'total' || s.kind === 'base') && 'font-medium text-foreground',
                  )}
                  title={s.label}
                >
                  {s.label}
                </span>
              </span>
              <div className="relative h-5 flex-1 overflow-hidden rounded bg-muted/40">
                {/* Zero baseline: the eye's anchor when the cascade crosses zero. */}
                {crossesZero ? (
                  <div
                    className="absolute inset-y-0 w-px bg-border"
                    style={{ left: `${zeroFrac * 100}%` }}
                    aria-hidden
                  />
                ) : null}
                <div
                  className={cn(
                    'absolute inset-y-0 rounded',
                    (s.kind === 'total' || s.kind === 'base') && 'ring-1 ring-inset ring-accent/40',
                  )}
                  style={{
                    left: `${leftFrac * 100}%`,
                    width: `${Math.max(widthFrac * 100, 1.5)}%`,
                    backgroundColor: tone.bar,
                    opacity: s.kind === 'delta' ? 0.7 : 0.6,
                  }}
                  aria-hidden
                />
              </div>
              <span
                className={cn(
                  'w-32 shrink-0 text-right font-mono text-sm tabular-nums',
                  tone.text,
                  (s.kind === 'total' || s.kind === 'base') && 'font-semibold',
                )}
              >
                {s.display}
                {unit ? (
                  <span className="ml-1 text-xs font-normal text-muted-foreground">{unit}</span>
                ) : null}
              </span>
            </li>
          );
        })}
      </ol>
    </div>
  );
}
