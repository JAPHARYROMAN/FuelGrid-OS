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
    <header className="flex h-14 items-center justify-between border-b border-border bg-card/40 px-4">
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={onOpenCommand}
          className="group flex items-center gap-2 rounded-md border border-border bg-background px-3 py-1.5 text-sm text-muted-foreground transition-colors hover:bg-muted"
        >
          <Search className="size-4" />
          <span>Search…</span>
          <kbd className="ml-4 inline-flex items-center gap-0.5 rounded border border-border bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
            <Command className="size-3" />K
          </kbd>
        </button>

        {(stations.data?.items?.length ?? 0) > 0 ? (
          <select
            className="h-9 rounded-md border border-border bg-background px-2 text-xs"
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

      <div className="flex items-center gap-1">
        <Link
          href="/profile"
          className="inline-flex h-9 w-9 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          aria-label="Profile"
        >
          <UserCircle className="size-4" />
        </Link>
        <Button
          variant="ghost"
          size="icon"
          aria-label="Toggle theme"
          onClick={() => setTheme(isDark ? 'light' : 'dark')}
        >
          {isDark ? <Sun className="size-4" /> : <Moon className="size-4" />}
        </Button>
        <Button variant="ghost" size="icon" aria-label="Sign out" onClick={handleLogout}>
          <LogOut className="size-4" />
        </Button>
      </div>
    </header>
  );
}
