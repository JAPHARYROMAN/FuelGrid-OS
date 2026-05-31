'use client';

import { AlertCircle, CheckCircle2, Info, X } from 'lucide-react';

import { toast, useToasts, type ToastTone } from '@/lib/toast';

const TONE_STYLES: Record<ToastTone, { wrap: string; icon: React.ReactNode }> = {
  error: {
    wrap: 'border-danger/40 bg-danger/10 text-foreground',
    icon: <AlertCircle className="size-4 shrink-0 text-danger" />,
  },
  success: {
    wrap: 'border-success/40 bg-success/10 text-foreground',
    icon: <CheckCircle2 className="size-4 shrink-0 text-success" />,
  },
  info: {
    wrap: 'border-border bg-muted text-foreground',
    icon: <Info className="size-4 shrink-0 text-muted-foreground" />,
  },
};

/**
 * Toaster renders the live toast stack in a fixed corner. Mounted once in
 * the dashboard layout; everything else just calls `toast.*` from the store.
 */
export function Toaster() {
  const toasts = useToasts();

  if (toasts.length === 0) return null;

  return (
    <div
      className="pointer-events-none fixed bottom-4 right-4 z-50 flex w-full max-w-sm flex-col gap-2"
      role="region"
      aria-label="Notifications"
    >
      {toasts.map((t) => {
        const tone = TONE_STYLES[t.tone];
        return (
          <div
            key={t.id}
            className={`pointer-events-auto flex items-start gap-2.5 rounded-lg border px-3.5 py-3 text-sm shadow-lg ${tone.wrap}`}
            role={t.tone === 'error' ? 'alert' : 'status'}
          >
            {tone.icon}
            <div className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className="font-medium">{t.title}</span>
              {t.description ? (
                <span className="text-xs text-muted-foreground">{t.description}</span>
              ) : null}
            </div>
            <button
              type="button"
              onClick={() => toast.dismiss(t.id)}
              className="shrink-0 rounded-sm text-muted-foreground transition-colors hover:text-foreground"
              aria-label="Dismiss"
            >
              <X className="size-4" />
            </button>
          </div>
        );
      })}
    </div>
  );
}
