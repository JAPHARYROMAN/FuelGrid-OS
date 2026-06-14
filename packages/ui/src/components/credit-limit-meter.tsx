import { cn } from '../lib/cn';
import { chartColors } from '../lib/chart-theme';

/**
 * CreditLimitMeter — an accessible credit-limit utilization gauge.
 *
 * Each meter shows one customer's exposure as a percentage of their credit
 * limit: a labelled track with a filled bar, the utilization percent as TEXT,
 * and an optional exposure / limit caption. The fill's TONE (ok / warning /
 * over) is driven by the utilization against a soft warning threshold and the
 * 100% limit, but COLOUR IS NEVER THE SOLE SIGNAL — every meter prints its
 * percent and a status word, and carries a `role="meter"` with aria-valuenow /
 * aria-valuemin / aria-valuemax / aria-valuetext so screen readers announce the
 * exact figure. Reads in monochrome print and for colour-vision-deficient users.
 *
 * The component never does business math: `utilization` is a finite number used
 * ONLY for the bar geometry + tone threshold, and exposure / limit arrive as
 * already-formatted display strings (the caller owns the numeric->text money
 * contract and any formatMoney call). The Customer Credit report (§5.9) uses it
 * for the per-top-customer utilization meters; later phases can reuse it for any
 * limit/quota gauge.
 */

/** A semantic utilization tone. Maps to a token colour + status word. */
export type CreditMeterTone = 'ok' | 'warning' | 'over';

interface ToneStyle {
  /** The filled-bar colour. */
  fill: string;
  /** The percent + status text colour. */
  text: string;
  /** The default status word. */
  word: string;
}

const TONE_STYLE: Record<CreditMeterTone, ToneStyle> = {
  ok: { fill: chartColors.success, text: 'text-success', word: 'Within limit' },
  warning: { fill: chartColors.warning, text: 'text-warning', word: 'Near limit' },
  over: { fill: chartColors.danger, text: 'text-danger', word: 'Over limit' },
};

/**
 * Derive the meter tone from the utilization percent and the soft warning
 * threshold: at/over 100% is `over`, at/over the warning threshold is `warning`,
 * otherwise `ok`. A non-positive limit (utilization 0 with no limit) reads `ok`.
 */
export function creditMeterTone(utilization: number, warningPct = 80): CreditMeterTone {
  if (utilization >= 100) return 'over';
  if (utilization >= warningPct) return 'warning';
  return 'ok';
}

export interface CreditLimitMeterItem {
  /** Stable key. */
  key: string;
  /** Customer / entity label. */
  label: string;
  /**
   * Utilization percent (exposure / limit * 100). Used ONLY for bar geometry and
   * the tone threshold — never re-derived into money. Clamped to [0, 100] for the
   * bar width; the printed percent shows the true value (so >100% reads honestly).
   */
  utilization: number;
  /** Soft warning threshold percent. Defaults to 80. */
  warningPct?: number;
  /** Already-formatted exposure figure, e.g. formatMoney(exposure). */
  exposure?: string;
  /** Already-formatted credit-limit figure, e.g. formatMoney(limit). */
  limit?: string;
  /** Override the derived status word (e.g. "On hold"). */
  statusWord?: string;
  /** Force a tone (e.g. on-hold customers render `over`); else derived. */
  tone?: CreditMeterTone;
}

export interface CreditLimitMeterProps {
  items: CreditLimitMeterItem[];
  className?: string;
}

/** Clamp a value into [min, max]; non-finite values clamp to min. */
function clamp(v: number, min: number, max: number): number {
  if (!Number.isFinite(v)) return min;
  return Math.min(max, Math.max(min, v));
}

/** Format a utilization percent for display (no decimals when whole). */
function formatPct(v: number): string {
  if (!Number.isFinite(v)) return '0%';
  const rounded = Math.round(v * 10) / 10;
  return `${Number.isInteger(rounded) ? rounded.toFixed(0) : rounded.toFixed(1)}%`;
}

export function CreditLimitMeter({ items, className }: CreditLimitMeterProps) {
  if (items.length === 0) return null;
  return (
    <ul className={cn('flex flex-col gap-4', className)} role="list">
      {items.map((it) => {
        const warning = it.warningPct ?? 80;
        const tone = it.tone ?? creditMeterTone(it.utilization, warning);
        const style = TONE_STYLE[tone];
        const word = it.statusWord ?? style.word;
        const pctText = formatPct(it.utilization);
        const barWidth = clamp(it.utilization, 0, 100);
        const ariaText = `${it.label}: ${pctText} of credit limit used — ${word}`;
        return (
          <li key={it.key} className="flex flex-col gap-1.5">
            <div className="flex items-baseline justify-between gap-2">
              <span className="truncate text-sm font-medium text-foreground" title={it.label}>
                {it.label}
              </span>
              <span
                className={cn('shrink-0 font-mono text-sm font-semibold tabular-nums', style.text)}
              >
                {pctText}
              </span>
            </div>
            <div
              className="h-2 w-full overflow-hidden rounded-full bg-muted"
              role="meter"
              // Clamp aria-valuenow into the declared [0,100] range (WAI-ARIA
              // requires valuenow ≤ valuemax). The TRUE percent (e.g. 125%) is
              // still announced via aria-valuetext and printed as text, so an
              // over-limit customer reads honestly without an out-of-range value.
              aria-valuenow={Math.round(clamp(it.utilization, 0, 100))}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuetext={ariaText}
              aria-label={ariaText}
            >
              <div
                className="h-full rounded-full transition-all"
                style={{ width: `${barWidth}%`, backgroundColor: style.fill }}
              />
            </div>
            <div className="flex items-center justify-between gap-2 text-xs">
              <span className={cn('font-medium', style.text)}>{word}</span>
              {it.exposure != null && it.limit != null ? (
                <span className="font-mono tabular-nums text-muted-foreground">
                  {it.exposure} / {it.limit}
                </span>
              ) : null}
            </div>
          </li>
        );
      })}
    </ul>
  );
}
