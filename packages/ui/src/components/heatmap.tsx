'use client';

import { cn } from '../lib/cn';
import { chartColors } from '../lib/chart-theme';

/**
 * Heatmap — a token-themed grid of magnitude cells.
 *
 * Each cell carries an `intensity` in 0..1 (its share of the strongest value in
 * the grid) that drives a graduated wash of one semantic token, plus the
 * caller-formatted figure rendered as TEXT inside the cell. Colour is never the
 * sole signal: every cell shows its number, an optional sub-label, and
 * over-threshold cells get a distinct ring + a textual badge — so the grid is
 * readable for colour-vision-deficient users and in monochrome print.
 *
 * Values arrive as decimal STRINGS (the numeric->text money/litre contract);
 * the component never parses them for business math — the caller supplies both
 * the already-formatted display string and the pre-computed numeric `intensity`
 * (so the float coercion stays at the call site, exactly like the chart
 * wrappers). Built as a clean shared primitive: later phases reuse it for the
 * sales-by-hour heatmap (§5.2) and the risk heatmap (§5.11).
 */

/** One semantic accent a heatmap cell washes toward as intensity rises. */
export type HeatmapTone = 'danger' | 'warning' | 'success' | 'accent' | 'neutral';

const TONE_HSL: Record<HeatmapTone, string> = {
  danger: 'var(--color-danger)',
  warning: 'var(--color-warning)',
  success: 'var(--color-success)',
  accent: 'var(--color-accent)',
  neutral: 'var(--color-muted-foreground)',
};

export interface HeatmapCell {
  /** Stable key (unique within its row). */
  key: string;
  /** The already-formatted figure shown as text in the cell. */
  display: string;
  /** 0..1 magnitude relative to the grid max — drives the wash opacity. */
  intensity: number;
  /** Semantic accent for this cell; defaults to the heatmap's `tone`. */
  tone?: HeatmapTone;
  /** Flagged cells get a distinct ring + the `flagLabel` chip (text, not colour-alone). */
  flagged?: boolean;
  /** Optional small caption under the figure (e.g. a tolerance band). */
  sublabel?: string;
  /** Accessible description; falls back to `${rowLabel} ${display}`. */
  ariaLabel?: string;
}

export interface HeatmapRow {
  /** Stable key. */
  key: string;
  /** Row header shown on the left. */
  label: string;
  /** Optional secondary header line (e.g. a product/category). */
  sublabel?: string;
  cells: HeatmapCell[];
}

export interface HeatmapProps {
  rows: HeatmapRow[];
  /** Column headers, aligned to each row's cells by index. */
  columns: string[];
  /** Default cell accent. Defaults to "accent". */
  tone?: HeatmapTone;
  /** Text chip shown on flagged cells. Defaults to "Over". */
  flagLabel?: string;
  /** Lowest wash opacity (intensity 0). Defaults to 0.06. */
  minOpacity?: number;
  /**
   * Highest wash opacity (intensity 1). Defaults to 0.6 — capped so the
   * fixed `text-foreground` figure keeps ≥4.5:1 (WCAG AA) against even the
   * heaviest danger wash in dark mode, where a 0.85 wash drops the number below
   * AA. The whole premise of the grid is "read the figure, not the colour", so
   * the figure must stay legible at full intensity.
   */
  maxOpacity?: number;
  className?: string;
}

const clamp01 = (n: number) => (Number.isFinite(n) ? Math.max(0, Math.min(1, n)) : 0);

/** The wash background for a cell: a token hsl at an intensity-scaled alpha. */
function cellBackground(tone: HeatmapTone, intensity: number, min: number, max: number): string {
  const alpha = min + clamp01(intensity) * (max - min);
  return `hsl(${TONE_HSL[tone]} / ${alpha.toFixed(3)})`;
}

export function Heatmap({
  rows,
  columns,
  tone = 'accent',
  flagLabel = 'Over',
  minOpacity = 0.06,
  maxOpacity = 0.6,
  className,
}: HeatmapProps) {
  if (rows.length === 0 || columns.length === 0) return null;

  return (
    <div className={cn('w-full overflow-x-auto', className)}>
      <table className="w-full border-separate border-spacing-1">
        <thead>
          <tr>
            <th scope="col" className="w-px" />
            {columns.map((c) => (
              <th
                key={c}
                scope="col"
                className="px-2 pb-1 text-center text-[11px] font-medium uppercase tracking-wider text-muted-foreground"
              >
                {c}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.key}>
              <th
                scope="row"
                className="whitespace-nowrap py-1 pr-3 text-left align-middle text-sm font-medium text-foreground"
              >
                {row.label}
                {row.sublabel ? (
                  <span className="block font-mono text-[11px] font-normal text-muted-foreground">
                    {row.sublabel}
                  </span>
                ) : null}
              </th>
              {row.cells.map((cell) => {
                const cellTone = cell.tone ?? tone;
                return (
                  <td key={cell.key} className="p-0">
                    <div
                      className={cn(
                        'relative flex min-h-[3.25rem] flex-col items-center justify-center gap-0.5 rounded-md border px-2 py-1.5 text-center',
                        cell.flagged ? 'border-danger ring-1 ring-danger/60' : 'border-border/60',
                      )}
                      style={{
                        backgroundColor: cellBackground(
                          cellTone,
                          cell.intensity,
                          minOpacity,
                          maxOpacity,
                        ),
                      }}
                      role="img"
                      aria-label={cell.ariaLabel ?? `${row.label} ${cell.display}`}
                      title={cell.ariaLabel ?? `${row.label}: ${cell.display}`}
                    >
                      <span className="font-mono text-sm font-semibold tabular-nums text-foreground">
                        {cell.display}
                      </span>
                      {cell.sublabel ? (
                        <span className="font-mono text-[10px] tabular-nums text-muted-foreground">
                          {cell.sublabel}
                        </span>
                      ) : null}
                      {cell.flagged ? (
                        <span className="mt-0.5 rounded-full bg-danger px-1.5 text-[9px] font-semibold uppercase tracking-wide text-danger-foreground">
                          {flagLabel}
                        </span>
                      ) : null}
                    </div>
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** The token color a caller can read for a legend swatch, by tone. */
export function heatmapToneColor(tone: HeatmapTone): string {
  switch (tone) {
    case 'danger':
      return chartColors.danger;
    case 'warning':
      return chartColors.warning;
    case 'success':
      return chartColors.success;
    case 'neutral':
      return chartColors.muted;
    default:
      return chartColors.accent;
  }
}
