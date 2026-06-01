import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { ArrowDownRight, ArrowUpRight } from 'lucide-react';

import { cn } from '../lib/cn';

/**
 * Stat — a single KPI tile. The value uses the mono/tabular face so numbers
 * across a row of stats align to the pixel. Delta is optional and is the only
 * place success/danger color appears here (state, not decoration).
 */
const deltaVariants = cva('inline-flex items-center gap-0.5 text-xs font-medium tabular-nums', {
  variants: {
    direction: {
      up: 'text-success',
      down: 'text-danger',
      flat: 'text-muted-foreground',
    },
  },
  defaultVariants: { direction: 'flat' },
});

export interface StatProps
  extends React.HTMLAttributes<HTMLDivElement>, VariantProps<typeof deltaVariants> {
  label: string;
  value: React.ReactNode;
  unit?: string;
  delta?: string;
  hint?: string;
  icon?: React.ReactNode;
}

export function Stat({
  label,
  value,
  unit,
  delta,
  direction,
  hint,
  icon,
  className,
  ...props
}: StatProps) {
  return (
    <div
      className={cn(
        'group relative flex flex-col gap-3 rounded-xl border border-border/80 bg-card p-5 shadow-elev-sm',
        className,
      )}
      {...props}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-medium text-muted-foreground">{label}</span>
        {icon ? (
          <span className="flex size-8 items-center justify-center rounded-lg bg-accent-muted/60 text-accent [&_svg]:size-4">
            {icon}
          </span>
        ) : null}
      </div>

      <div className="flex items-baseline gap-1.5">
        <span className="font-mono text-2xl font-semibold tracking-tight tabular-nums text-foreground">
          {value}
        </span>
        {unit ? <span className="text-sm text-muted-foreground">{unit}</span> : null}
      </div>

      {delta || hint ? (
        <div className="flex items-center gap-2">
          {delta ? (
            <span className={cn(deltaVariants({ direction }))}>
              {direction === 'up' ? (
                <ArrowUpRight className="size-3.5" />
              ) : direction === 'down' ? (
                <ArrowDownRight className="size-3.5" />
              ) : null}
              {delta}
            </span>
          ) : null}
          {hint ? <span className="text-xs text-muted-foreground">{hint}</span> : null}
        </div>
      ) : null}
    </div>
  );
}
