import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { ArrowDownRight, ArrowUpRight } from 'lucide-react';

import { cn } from '../lib/cn';
import { Skeleton } from './skeleton';

/**
 * MetricCard — a labeled metric tile that extends the Stat language with an
 * optional sublabel and a richer trend. Numbers use the mono/tabular face so a
 * row of metrics aligns to the pixel; success/danger color appears only on the
 * trend (state, not decoration). Loading renders an inline skeleton so the tile
 * keeps its footprint instead of collapsing the grid.
 */
const trendVariants = cva('inline-flex items-center gap-0.5 text-xs font-medium tabular-nums', {
  variants: {
    trend: {
      up: 'text-success',
      down: 'text-danger',
      flat: 'text-muted-foreground',
    },
  },
  defaultVariants: { trend: 'flat' },
});

export interface MetricCardProps
  extends Omit<React.HTMLAttributes<HTMLDivElement>, 'title'>, VariantProps<typeof trendVariants> {
  label: string;
  value: React.ReactNode;
  /** A small caption above the label, e.g. a category or station code. */
  sublabel?: React.ReactNode;
  unit?: string;
  /** Trend figure, e.g. "+12.4%". Coloured by the `trend` direction. */
  trendValue?: string;
  /** A neutral hint beside the trend, e.g. "vs last month". */
  hint?: string;
  icon?: React.ReactNode;
  /** Renders a skeleton in place of the value. */
  loading?: boolean;
  /** Slot below the value, e.g. a <Sparkline />. */
  children?: React.ReactNode;
}

export function MetricCard({
  label,
  value,
  sublabel,
  unit,
  trend,
  trendValue,
  hint,
  icon,
  loading = false,
  className,
  children,
  ...props
}: MetricCardProps) {
  return (
    <div
      className={cn(
        'group relative flex flex-col gap-3 rounded-xl border border-border/80 bg-card p-5 shadow-elev-sm',
        className,
      )}
      {...props}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="flex min-w-0 flex-col gap-0.5">
          {sublabel ? (
            <span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
              {sublabel}
            </span>
          ) : null}
          <span className="text-sm font-medium text-muted-foreground">{label}</span>
        </div>
        {icon ? (
          <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-accent-muted/60 text-accent [&_svg]:size-4">
            {icon}
          </span>
        ) : null}
      </div>

      {loading ? (
        <Skeleton className="h-8 w-28 rounded-md" />
      ) : (
        <div className="flex items-baseline gap-1.5">
          <span className="font-mono text-2xl font-semibold tracking-tight tabular-nums text-foreground">
            {value}
          </span>
          {unit ? <span className="text-sm text-muted-foreground">{unit}</span> : null}
        </div>
      )}

      {!loading && (trendValue || hint) ? (
        <div className="flex items-center gap-2">
          {trendValue ? (
            <span className={cn(trendVariants({ trend }))}>
              {trend === 'up' ? (
                <ArrowUpRight className="size-3.5" />
              ) : trend === 'down' ? (
                <ArrowDownRight className="size-3.5" />
              ) : null}
              {trendValue}
            </span>
          ) : null}
          {hint ? <span className="text-xs text-muted-foreground">{hint}</span> : null}
        </div>
      ) : null}

      {children ? <div className="-mb-1 mt-1">{children}</div> : null}
    </div>
  );
}
