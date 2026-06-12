'use client';

import { Fuel } from 'lucide-react';

import { ProtectedRoute } from '@/components/auth/protected-route';
import { Toaster } from '@/components/toaster';

import { ServiceWorkerManager } from './sw-register';
import { OfflineHint, SyncStatusChip } from './sync-status';

/**
 * Mobile Attendant App shell (Phase 1). Deliberately NOT the desktop
 * (dashboard) chrome: no sidebar, no command palette — a narrow, large-type,
 * touch-first column for pump attendants in the field (PRD §5).
 *
 * Auth: the same session as the main app. Server-side middleware redirects
 * unauthenticated visits to /login?next=/attendant; ProtectedRoute is the
 * client-side flash guard on top.
 *
 * Offline (Phase 6a): the header chip is the real sync indicator backed by
 * the offline action queue (tap → sync details sheet); the strip under the
 * header marks stale data while offline; ServiceWorkerManager registers
 * /sw.js (app-shell offline) and offers the update-reload affordance.
 */
export default function AttendantLayout({ children }: { children: React.ReactNode }) {
  return (
    <ProtectedRoute>
      <div className="flex min-h-screen flex-col bg-background text-foreground">
        <header className="sticky top-0 z-10 border-b border-border bg-background/95 backdrop-blur">
          <div className="mx-auto flex h-14 w-full max-w-md items-center justify-between px-4">
            <span className="flex items-center gap-2 text-base font-semibold tracking-tight">
              <span className="flex size-8 items-center justify-center rounded-lg bg-accent text-accent-foreground">
                <Fuel className="size-4" aria-hidden />
              </span>
              Attendant
            </span>
            <SyncStatusChip />
          </div>
        </header>
        <ServiceWorkerManager />
        <OfflineHint />
        <main className="mx-auto w-full max-w-md flex-1 px-4 py-5 pb-12">{children}</main>
      </div>
      <Toaster />
    </ProtectedRoute>
  );
}
