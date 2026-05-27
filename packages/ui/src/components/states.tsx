import * as React from 'react';
import { AlertCircle, Inbox, Loader2 } from 'lucide-react';

import { cn } from '../lib/cn';
import { Button } from './button';

/**
 * State components are the "boring but mandatory" surfaces every screen
 * must support per docs/ui-ux.md §18.4. Build a feature against these
 * three components first; never let a screen ship with a spinner sitting
 * on a blank background.
 */

interface BaseStateProps {
  title: string;
  description?: React.ReactNode;
  icon?: React.ReactNode;
  action?: React.ReactNode;
  className?: string;
}

export function EmptyState({ title, description, icon, action, className }: BaseStateProps) {
  return (
    <div
      className={cn(
        'flex w-full flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-border bg-muted/20 p-10 text-center',
        className,
      )}
    >
      <div className="text-muted-foreground">{icon ?? <Inbox className="size-8" />}</div>
      <h3 className="text-base font-semibold text-foreground">{title}</h3>
      {description ? <p className="max-w-md text-sm text-muted-foreground">{description}</p> : null}
      {action ? <div className="pt-1">{action}</div> : null}
    </div>
  );
}

export function LoadingState({
  title = 'Loading…',
  description,
  className,
}: Partial<BaseStateProps>) {
  return (
    <div
      className={cn(
        'flex w-full flex-col items-center justify-center gap-3 rounded-xl border border-border bg-muted/10 p-10 text-center',
        className,
      )}
      role="status"
      aria-live="polite"
    >
      <Loader2 className="size-6 animate-spin text-muted-foreground" />
      <h3 className="text-base font-semibold text-foreground">{title}</h3>
      {description ? <p className="max-w-md text-sm text-muted-foreground">{description}</p> : null}
    </div>
  );
}

export interface ErrorStateProps extends BaseStateProps {
  /** Optional retry callback. Renders a "Try again" button when provided. */
  onRetry?: () => void;
}

export function ErrorState({
  title,
  description,
  icon,
  onRetry,
  action,
  className,
}: ErrorStateProps) {
  const retry = onRetry ? (
    <Button variant="secondary" onClick={onRetry}>
      Try again
    </Button>
  ) : null;

  return (
    <div
      className={cn(
        'flex w-full flex-col items-center justify-center gap-3 rounded-xl border border-danger/40 bg-danger/5 p-10 text-center',
        className,
      )}
      role="alert"
    >
      <div className="text-danger">{icon ?? <AlertCircle className="size-8" />}</div>
      <h3 className="text-base font-semibold text-foreground">{title}</h3>
      {description ? <p className="max-w-md text-sm text-muted-foreground">{description}</p> : null}
      {action ?? retry}
    </div>
  );
}
