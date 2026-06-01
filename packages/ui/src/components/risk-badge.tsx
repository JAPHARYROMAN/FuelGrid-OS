import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';

import { cn } from '../lib/cn';

/**
 * RiskBadge — a severity pill for risk/alert levels. Uses the same token
 * vocabulary as Badge but maps a five-level risk scale to semantic colour, with
 * an uppercase tracked label so a column of risk levels reads as a scale.
 */
const riskBadgeVariants = cva(
  'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-[11px] font-semibold uppercase tracking-wider [&_svg]:size-3',
  {
    variants: {
      severity: {
        critical: 'border-danger/30 bg-danger/15 text-danger',
        high: 'border-warning/30 bg-warning/15 text-warning',
        medium: 'border-warning/20 bg-warning/10 text-warning',
        low: 'border-info/20 bg-info/10 text-info',
        info: 'border-border bg-muted/60 text-muted-foreground',
      },
    },
    defaultVariants: { severity: 'info' },
  },
);

export type RiskSeverity = 'critical' | 'high' | 'medium' | 'low' | 'info';

export interface RiskBadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>, VariantProps<typeof riskBadgeVariants> {
  /** Icon slot, e.g. a dot or lucide glyph. */
  icon?: React.ReactNode;
}

export function RiskBadge({ className, severity, icon, children, ...props }: RiskBadgeProps) {
  return (
    <span className={cn(riskBadgeVariants({ severity }), className)} {...props}>
      {icon}
      {children ?? severity}
    </span>
  );
}

export { riskBadgeVariants };
