'use client';

import * as React from 'react';
import * as TooltipPrimitive from '@radix-ui/react-tooltip';

import { cn } from '../lib/cn';

/**
 * Tooltip — Radix tooltip skinned for the Refined Console. `TooltipProvider`
 * wraps the app once (in the dashboard shell) so individual tooltips don't each
 * pay the provider cost. The default `delayDuration` keeps icon-button hints
 * from firing on a fast pointer sweep.
 */
export const TooltipProvider = TooltipPrimitive.Provider;
export const TooltipRoot = TooltipPrimitive.Root;
export const TooltipTrigger = TooltipPrimitive.Trigger;

export const TooltipContent = React.forwardRef<
  React.ElementRef<typeof TooltipPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Content>
>(({ className, sideOffset = 6, ...props }, ref) => (
  <TooltipPrimitive.Portal>
    <TooltipPrimitive.Content
      ref={ref}
      sideOffset={sideOffset}
      className={cn(
        'z-50 overflow-hidden rounded-md border border-border bg-card px-2.5 py-1.5 text-xs font-medium text-foreground shadow-lg',
        'data-[state=delayed-open]:animate-in data-[state=closed]:animate-out',
        className,
      )}
      {...props}
    />
  </TooltipPrimitive.Portal>
));
TooltipContent.displayName = TooltipPrimitive.Content.displayName;

export interface TooltipProps {
  label: React.ReactNode;
  children: React.ReactNode;
  side?: TooltipPrimitive.TooltipContentProps['side'];
  delayDuration?: number;
  asChild?: boolean;
}

/**
 * Tooltip — convenience wrapper for the common "hover this element, show a
 * short label" case. Assumes a `TooltipProvider` is mounted higher in the tree.
 */
export function Tooltip({
  label,
  children,
  side = 'bottom',
  delayDuration = 200,
  asChild = true,
}: TooltipProps) {
  return (
    <TooltipRoot delayDuration={delayDuration}>
      <TooltipTrigger asChild={asChild}>{children}</TooltipTrigger>
      <TooltipContent side={side}>{label}</TooltipContent>
    </TooltipRoot>
  );
}
