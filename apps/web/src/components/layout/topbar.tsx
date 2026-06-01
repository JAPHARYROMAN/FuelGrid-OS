'use client';

import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useTheme } from 'next-themes';
import { Command, LogOut, Moon, Search, Sun, UserCircle } from 'lucide-react';

import { SdkError } from '@fuelgrid/sdk';
import { Button } from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { clearSentryUser } from '@/lib/sentry';
import { NotificationBell } from './notification-bell';
import { useAuthStore } from '@/stores/auth-store';
import { useTenantStore } from '@/stores/tenant-store';

interface TopbarProps {
  onOpenCommand: () => void;
}

export function Topbar({ onOpenCommand }: TopbarProps) {
  const router = useRouter();
  const { theme, setTheme, resolvedTheme } = useTheme();

  const clearSession = useAuthStore((s) => s.clearSession);
  const resetTenantContext = useTenantStore((s) => s.reset);
  const activeStationID = useTenantStore((s) => s.activeStationID);
  const setActiveStation = useTenantStore((s) => s.setActiveStation);

  const authed = useAuthStore((s) => s.authed);

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
    enabled: authed,
  });

  async function handleLogout() {
    try {
      await api.logout();
    } catch (err) {
      if (!(err instanceof SdkError) || err.status >= 500) {
        console.error('logout failed', err);
      }
    }
    clearSession();
    resetTenantContext();
    clearSentryUser();
    router.replace('/login');
  }

  const isDark = (theme ?? resolvedTheme) === 'dark';

  return (
    <header className="sticky top-0 z-30 flex h-16 items-center justify-between gap-4 border-b border-border bg-background/80 px-4 backdrop-blur-md sm:px-6">
      <div className="flex min-w-0 items-center gap-3">
        <button
          type="button"
          onClick={onOpenCommand}
          className="group flex h-9 w-56 items-center gap-2 rounded-lg border border-border bg-card/60 px-3 text-sm text-muted-foreground shadow-elev-sm transition-colors hover:border-accent/40 hover:text-foreground"
        >
          <Search className="size-4" />
          <span className="flex-1 text-left">Search…</span>
          <kbd className="inline-flex items-center gap-0.5 rounded border border-border bg-muted px-1.5 py-0.5 font-mono text-[10px] font-medium text-muted-foreground">
            <Command className="size-3" />K
          </kbd>
        </button>

        {(stations.data?.items?.length ?? 0) > 0 ? (
          <select
            className="hidden h-9 rounded-lg border border-border bg-card/60 px-2.5 text-xs text-foreground shadow-elev-sm outline-none transition-colors hover:border-accent/40 focus-visible:border-accent sm:block"
            value={activeStationID ?? ''}
            onChange={(e) => setActiveStation(e.target.value || null)}
            aria-label="Active station"
          >
            <option value="">All stations</option>
            {(stations.data?.items ?? []).map((s) => (
              <option key={s.id} value={s.id}>
                {s.code} — {s.name}
              </option>
            ))}
          </select>
        ) : null}
      </div>

      <div className="flex items-center gap-0.5">
        <NotificationBell />
        <Button
          variant="ghost"
          size="icon"
          aria-label="Toggle theme"
          onClick={() => setTheme(isDark ? 'light' : 'dark')}
        >
          {isDark ? <Sun className="size-4" /> : <Moon className="size-4" />}
        </Button>
        <Link
          href="/profile"
          className="inline-flex size-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          aria-label="Profile"
        >
          <UserCircle className="size-[18px]" />
        </Link>
        <span className="mx-1 h-5 w-px bg-border" aria-hidden="true" />
        <Button variant="ghost" size="icon" aria-label="Sign out" onClick={handleLogout}>
          <LogOut className="size-4" />
        </Button>
      </div>
    </header>
  );
}
