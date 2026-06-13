'use client';

import { Fuel } from 'lucide-react';

import { useAttendantPrefs, useT } from '@/lib/i18n';

import { DisplaySettingsButton } from './display-settings';
import { AttendantNotificationBell } from './notification-bell';
import { ServiceWorkerManager } from './sw-register';
import { OfflineHint, SyncStatusChip } from './sync-status';

/**
 * The attendant shell chrome (header + offline surfaces + content column),
 * separated from the route layout so it can be exercised directly in tests.
 *
 * Phase 6b: the shell root carries the accessibility modes as data
 * attributes — `data-text-size` (normal|large) and `data-contrast`
 * (normal|high) — which the scoped CSS in globals.css turns into larger
 * typography/touch targets and stronger text/border/status contrast
 * (PRD §15.1). Both persist via the attendant prefs provider.
 */
export function AttendantShell({ children }: { children: React.ReactNode }) {
  const t = useT();
  const { textSize, contrast } = useAttendantPrefs();

  return (
    <div
      data-text-size={textSize}
      data-contrast={contrast}
      className="flex min-h-screen flex-col bg-background text-foreground"
    >
      <header className="sticky top-0 z-10 border-b border-border bg-background/95 backdrop-blur">
        <div className="mx-auto flex h-14 w-full max-w-md items-center justify-between gap-2 px-4">
          <span className="flex items-center gap-2 text-base font-semibold tracking-tight">
            <span className="flex size-8 items-center justify-center rounded-lg bg-accent text-accent-foreground">
              <Fuel className="size-4" aria-hidden />
            </span>
            {t.shell.appName}
          </span>
          <div className="flex items-center gap-1">
            <SyncStatusChip />
            <AttendantNotificationBell />
            <DisplaySettingsButton />
          </div>
        </div>
      </header>
      <ServiceWorkerManager />
      <OfflineHint />
      <main className="mx-auto w-full max-w-md flex-1 px-4 py-5 pb-12">{children}</main>
    </div>
  );
}
