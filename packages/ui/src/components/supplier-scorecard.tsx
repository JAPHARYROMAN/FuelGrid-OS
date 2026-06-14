'use client';

import { cn } from '../lib/cn';

/**
 * SupplierScorecard — a reusable, accessible supplier-performance treatment
 * (blueprint §5.7 "Supplier Scorecard Logic").
 *
 * Each supplier carries a deterministic 0-100 composite score (computed server-
 * side, never here), a band (Excellent / Good / Fair / At risk), a compact
 * letter grade, and a set of per-dimension sub-scores (on-time, quantity
 * accuracy, disputes, document completeness, delivery-variance history, and an
 * optional, cost-gated price-competitiveness dimension). The score drives a
 * semantic tone, but colour is NEVER the sole signal: every card prints the band
 * word, the numeric score, the grade letter, and each sub-score as a labelled
 * value with an aria-described bar — so the scorecard reads for colour-vision-
 * deficient users and in monochrome print.
 *
 * This component computes nothing about money or supplier facts; it is pure
 * presentation of already-scored data. Cards render worst-first as the caller
 * orders them, surfacing the suppliers that need attention at the top.
 */

/** RiskBadge-aligned tone a score band maps to. */
export type ScorecardTone = 'low' | 'medium' | 'high' | 'critical';

interface ToneStyle {
  card: string;
  badge: string;
  bar: string;
}

// "low" risk = best supplier (success), "critical" = worst (danger). The tone
// vocabulary matches RiskBadge so the two read on one scale across the report.
const TONE_STYLE: Record<ScorecardTone, ToneStyle> = {
  low: { card: 'border-success/30', badge: 'bg-success/15 text-success', bar: 'bg-success' },
  medium: { card: 'border-info/30', badge: 'bg-info/15 text-info', bar: 'bg-info' },
  high: { card: 'border-warning/30', badge: 'bg-warning/15 text-warning', bar: 'bg-warning' },
  critical: { card: 'border-danger/30', badge: 'bg-danger/15 text-danger', bar: 'bg-danger' },
};

/** One labelled sub-score (0-100) on a supplier card. */
export interface ScorecardDimension {
  key: string;
  label: string;
  /** 0-100. */
  score: number;
}

/** One supplier's scored card. */
export interface SupplierScorecardItem {
  key: string;
  name: string;
  /** 0-100 composite. */
  score: number;
  /** Band word (Excellent / Good / Fair / At risk). */
  band: string;
  /** RiskBadge-aligned tone. */
  tone: ScorecardTone;
  /** Compact letter grade (A/B/C/D). */
  grade: string;
  /** Per-dimension sub-scores. */
  dimensions: ScorecardDimension[];
  /** Optional small caption (e.g. "12 deliveries · 1 dispute"). */
  detail?: string;
}

export interface SupplierScorecardProps {
  suppliers: SupplierScorecardItem[];
  /** Tailwind grid column classes. Defaults to a responsive 1/2 grid. */
  columnsClassName?: string;
  className?: string;
}

/** Clamp + round a sub-score into the 0-100 bar width. */
function barWidth(score: number): number {
  if (!Number.isFinite(score)) return 0;
  return Math.max(0, Math.min(100, Math.round(score)));
}

export function SupplierScorecard({
  suppliers,
  columnsClassName = 'grid-cols-1 lg:grid-cols-2',
  className,
}: SupplierScorecardProps) {
  if (suppliers.length === 0) return null;
  return (
    <ul className={cn('grid gap-4', columnsClassName, className)} role="list">
      {suppliers.map((s) => {
        const style = TONE_STYLE[s.tone];
        return (
          <li
            key={s.key}
            className={cn(
              'flex flex-col gap-3 rounded-xl border bg-card p-4 shadow-elev-sm',
              style.card,
            )}
            aria-label={`${s.name}: score ${s.score} of 100, ${s.band}`}
          >
            <div className="flex items-start justify-between gap-3">
              <div className="flex min-w-0 flex-col gap-0.5">
                <span className="truncate text-sm font-semibold text-foreground">{s.name}</span>
                {s.detail ? (
                  <span className="text-xs text-muted-foreground">{s.detail}</span>
                ) : null}
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <span className="font-mono text-2xl font-semibold tabular-nums text-foreground">
                  {s.score}
                </span>
                <span
                  className={cn(
                    'inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-[11px] font-semibold uppercase tracking-wide',
                    style.badge,
                  )}
                >
                  {s.grade} · {s.band}
                </span>
              </div>
            </div>

            <dl className="flex flex-col gap-2">
              {s.dimensions.map((d) => {
                const w = barWidth(d.score);
                return (
                  <div
                    key={d.key}
                    className="grid grid-cols-[7.5rem_1fr_2.5rem] items-center gap-2"
                  >
                    <dt className="truncate text-xs text-muted-foreground">{d.label}</dt>
                    <dd
                      className="h-1.5 overflow-hidden rounded-full bg-muted"
                      role="meter"
                      aria-valuenow={w}
                      aria-valuemin={0}
                      aria-valuemax={100}
                      aria-label={`${d.label}: ${w} of 100`}
                    >
                      <div
                        className={cn('h-full rounded-full', style.bar)}
                        style={{ width: `${w}%` }}
                      />
                    </dd>
                    <span className="text-right font-mono text-xs tabular-nums text-foreground">
                      {w}
                    </span>
                  </div>
                );
              })}
            </dl>
          </li>
        );
      })}
    </ul>
  );
}
