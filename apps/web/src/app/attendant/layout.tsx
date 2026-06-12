'use client';

import { ProtectedRoute } from '@/components/auth/protected-route';
import { Toaster } from '@/components/toaster';
import { AttendantPrefsProvider } from '@/lib/i18n';

import { AttendantShell } from './attendant-shell';

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
 *
 * i18n + accessibility (Phase 6b): AttendantPrefsProvider carries the
 * locale (en/sw) and display modes (large text, high contrast), persisted in
 * localStorage; AttendantShell applies the modes as data attributes and
 * hosts the "Display & language" sheet.
 */
export default function AttendantLayout({ children }: { children: React.ReactNode }) {
  return (
    <AttendantPrefsProvider>
      <ProtectedRoute>
        <AttendantShell>{children}</AttendantShell>
        <Toaster />
      </ProtectedRoute>
    </AttendantPrefsProvider>
  );
}
