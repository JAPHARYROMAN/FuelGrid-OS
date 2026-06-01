/**
 * Chart color tokens for recharts.
 *
 * recharts paints inline SVG (stroke/fill on <path>/<rect>), so it cannot pick
 * up Tailwind utility classes the way the rest of the Refined Console does.
 * Instead we feed it the SAME semantic design tokens via CSS `hsl(var(--…))`
 * references — so every series tracks the live theme (light/dark) and the one
 * confident indigo accent, with no hard-coded hex anywhere.
 */
export const chartColors = {
  /** Brand indigo — primary series. */
  accent: 'hsl(var(--color-accent))',
  /** Faint indigo wash for area fills. */
  accentMuted: 'hsl(var(--color-accent-muted))',
  /** Muted gridlines / axes — never competes with the data. */
  grid: 'hsl(var(--color-border))',
  /** Axis tick + label text. */
  muted: 'hsl(var(--color-muted-foreground))',
  /** Foreground (tooltip values). */
  foreground: 'hsl(var(--color-foreground))',
  /** Tooltip / popover surface. */
  surface: 'hsl(var(--color-popover))',
  /** Status tokens — reserved for state, used by variance / P&L charts. */
  success: 'hsl(var(--color-success))',
  warning: 'hsl(var(--color-warning))',
  danger: 'hsl(var(--color-danger))',
} as const;

/** Shared tick styling for X/Y axes. */
export const axisTick = {
  fill: chartColors.muted,
  fontSize: 11,
  fontFamily: 'var(--font-mono, ui-monospace, monospace)',
} as const;
