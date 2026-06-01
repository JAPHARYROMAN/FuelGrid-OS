import * as React from 'react';

import { cn } from '../lib/cn';

/**
 * Skeleton — a quiet shimmer placeholder for loading content. Prefer this over
 * a centered spinner for list/table/card layouts so the page keeps its shape
 * while data loads.
 */
export function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn('animate-pulse rounded-md bg-muted/70', className)}
      aria-hidden="true"
      {...props}
    />
  );
}
