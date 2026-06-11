'use client';

import { useEffect, useState } from 'react';
import { Fuel, Wifi, WifiOff } from 'lucide-react';

import { ProtectedRoute } from '@/components/auth/protected-route';
import { Toaster } from '@/components/toaster';

/**
 * Mobile Attendant App shell (Phase 1). Deliberately NOT the desktop
 * (dashboard) chrome: no sidebar, no command palette — a narrow, large-type,
 * touch-first column for pump attendants in the field (PRD §5).
 *
 * Auth: the same session as the main app. Server-side middleware redirects
 * unauthenticated visits to /login?next=/attendant; ProtectedRoute is the
 * client-side flash guard on top.
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
            <ConnectionHint />
          </div>
        </header>
        <main className="mx-auto w-full max-w-md flex-1 px-4 py-5 pb-12">{children}</main>
      </div>
      <Toaster />
    </ProtectedRoute>
  );
}

/**
 * Online/offline hint (PRD sync-status visibility). Phase 1 has no offline
 * queue yet (Phase 6); this only tells the attendant whether actions can
 * reach the server right now. Colour is always paired with text.
 */
function ConnectionHint() {
  const [online, setOnline] = useState(true);

  useEffect(() => {
    setOnline(navigator.onLine);
    const up = () => setOnline(true);
    const down = () => setOnline(false);
    window.addEventListener('online', up);
    window.addEventListener('offline', down);
    return () => {
      window.removeEventListener('online', up);
      window.removeEventListener('offline', down);
    };
  }, []);

  return (
    <span
      className={
        'flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium ' +
        (online ? 'bg-success/10 text-success' : 'bg-danger/10 text-danger')
      }
      role="status"
    >
      {online ? (
        <Wifi className="size-3.5" aria-hidden />
      ) : (
        <WifiOff className="size-3.5" aria-hidden />
      )}
      {online ? 'Online' : 'Offline'}
    </span>
  );
}
