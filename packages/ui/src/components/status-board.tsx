'use client';

import { cn } from '../lib/cn';
import { chartColors } from '../lib/chart-theme';

/**
 * StatusBoard — a token-themed board of settlement/status chips.
 *
 * Each item is a payment medium (or any tracked stage) carrying a human label,
 * a terminal STATUS rendered as TEXT, and an optional figure. The status drives
 * a semantic tone, but colour is never the sole signal: every chip prints its
 * status word, an icon dot, and an accessible label — so the board reads for
 * colour-vision-deficient users and in monochrome print.
 *
 * Money/litre figures arrive as already-formatted display strings (the caller
 * owns the numeric->text contract and any formatMoney call); the component
 * never parses values for business math. Built as a clean shared primitive: the
 * Cash Reconciliation report uses it for the cash / mobile-money / card / bank
 * deposit settlement board, and later phases can reuse it for any
 * pending/settled/at-risk medium board.
 */

/** A semantic status a chip can carry. Maps to a token tone + glyph. */
export type StatusTone = 'settled' | 'pending' | 'at_risk' | 'neutral';

interface ToneStyle {
  /** Outer chip classes (border + wash). */
  chip: string;
  /** Status pill classes. */
  pill: string;
  /** The dot/glyph colour. */
  dot: string;
}

const TONE_STYLE: Record<StatusTone, ToneStyle> = {
  settled: {
    chip: 'border-success/30 bg-success/5',
    pill: 'bg-success/15 text-success',
    dot: 'bg-success',
  },
  pending: {
    chip: 'border-warning/30 bg-warning/5',
    pill: 'bg-warning/15 text-warning',
    dot: 'bg-warning',
  },
  at_risk: {
    chip: 'border-danger/30 bg-danger/5',
    pill: 'bg-danger/15 text-danger',
    dot: 'bg-danger',
  },
  neutral: {
    chip: 'border-border bg-muted/30',
    pill: 'bg-muted/60 text-muted-foreground',
    dot: 'bg-muted-foreground',
  },
};

export interface StatusBoardItem {
  /** Stable key. */
  key: string;
  /** Medium / stage label (e.g. "Mobile money"). */
  label: string;
  /** The terminal status word shown as text (e.g. "Settled", "Pending"). */
  status: string;
  /** Semantic tone driving the colour. Defaults to "neutral". */
  tone?: StatusTone;
  /** Optional already-formatted figure (e.g. formatMoney(amount)). */
  amount?: string;
  /** Optional small caption under the figure (e.g. "2 deposits posted"). */
  detail?: string;
  /** Accessible description; falls back to `${label}: ${status}`. */
  ariaLabel?: string;
}

export interface StatusBoardProps {
  items: StatusBoardItem[];
  /** Tailwind grid column classes. Defaults to a responsive 2/4 grid. */
  columnsClassName?: string;
  className?: string;
}

export function StatusBoard({
  items,
  columnsClassName = 'grid-cols-1 sm:grid-cols-2',
  className,
}: StatusBoardProps) {
  if (items.length === 0) return null;
  return (
    <ul className={cn('grid gap-3', columnsClassName, className)} role="list">
      {items.map((it) => {
        const tone = it.tone ?? 'neutral';
        const style = TONE_STYLE[tone];
        return (
          <li
            key={it.key}
            className={cn('flex flex-col gap-2 rounded-lg border p-3', style.chip)}
            aria-label={it.ariaLabel ?? `${it.label}: ${it.status}`}
          >
            <div className="flex items-center justify-between gap-2">
              <span className="text-sm font-medium text-foreground">{it.label}</span>
              <span
                className={cn(
                  'inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-semibold uppercase tracking-wide',
                  style.pill,
                )}
              >
                <span className={cn('size-1.5 rounded-full', style.dot)} aria-hidden="true" />
                {it.status}
              </span>
            </div>
            {it.amount != null ? (
              <span className="font-mono text-sm font-semibold tabular-nums text-foreground">
                {it.amount}
              </span>
            ) : null}
            {it.detail ? <span className="text-xs text-muted-foreground">{it.detail}</span> : null}
          </li>
        );
      })}
    </ul>
  );
}

/** The token colour a caller can read for a legend swatch, by tone. */
export function statusToneColor(tone: StatusTone): string {
  switch (tone) {
    case 'settled':
      return chartColors.success;
    case 'pending':
      return chartColors.warning;
    case 'at_risk':
      return chartColors.danger;
    default:
      return chartColors.muted;
  }
}
