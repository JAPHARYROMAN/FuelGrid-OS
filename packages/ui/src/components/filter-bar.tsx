import * as React from 'react';

import { cn } from '../lib/cn';

/**
 * FilterBar — a consistent container/layout for a row of filter controls
 * (selects, date ranges, search) above a report or list. It owns spacing and
 * wrapping only; the controls themselves stay caller-owned. An optional
 * `actions` slot pins reset/apply buttons to the right.
 */
export interface FilterBarProps extends React.HTMLAttributes<HTMLDivElement> {
  /** Right-aligned actions (reset, apply, export trigger). */
  actions?: React.ReactNode;
}

export function FilterBar({ className, children, actions, ...props }: FilterBarProps) {
  return (
    <div
      className={cn(
        'flex flex-col gap-3 rounded-xl border border-border/80 bg-card p-3 sm:flex-row sm:flex-wrap sm:items-end',
        className,
      )}
      role="group"
      aria-label="Filters"
      {...props}
    >
      <div className="flex flex-1 flex-wrap items-end gap-3">{children}</div>
      {actions ? <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  );
}

/**
 * FilterField — a labeled wrapper for a single control, keeping the label/
 * control rhythm consistent across the bar.
 */
export interface FilterFieldProps extends React.HTMLAttributes<HTMLDivElement> {
  label?: React.ReactNode;
}

export function FilterField({ label, className, children, ...props }: FilterFieldProps) {
  return (
    <div className={cn('flex min-w-0 flex-col gap-1', className)} {...props}>
      {label ? (
        <span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
          {label}
        </span>
      ) : null}
      {children}
    </div>
  );
}
