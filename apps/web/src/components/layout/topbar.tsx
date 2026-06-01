'use client';

import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useTheme } from 'next-themes';
import {
  Building2,
  Check,
  ChevronDown,
  Command,
  LogOut,
  Menu,
  Moon,
  Search,
  Sun,
  UserCircle,
} from 'lucide-react';

import { SdkError } from '@fuelgrid/sdk';
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Tooltip,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { clearSentryUser } from '@/lib/sentry';
import { useAuthStore } from '@/stores/auth-store';
import { useTenantStore } from '@/stores/tenant-store';

interface TopbarProps {
  onOpenCommand: () => void;
  onOpenMobileNav: () => void;
}

export function Topbar({ onOpenCommand, onOpenMobileNav }: TopbarProps) {
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
  const stationItems = stations.data?.items ?? [];
  const activeStation = stationItems.find((s) => s.id === activeStationID) ?? null;
  const activeLabel = activeStation ? activeStation.code : 'All stations';

  return (
    <header className="sticky top-0 z-30 flex h-16 items-center justify-between gap-4 border-b border-border bg-background/80 px-4 backdrop-blur-md sm:px-6">
      <div className="flex min-w-0 items-center gap-3">
        {/* Mobile nav trigger — hidden on lg+ where the sidebar is persistent. */}
        <Tooltip label="Menu">
          <Button
            variant="ghost"
            size="icon"
            className="lg:hidden"
            aria-label="Open navigation"
            onClick={onOpenMobileNav}
          >
            <Menu className="size-5" />
          </Button>
        </Tooltip>

        <button
          type="button"
          onClick={onOpenCommand}
          className="group flex h-9 w-44 items-center gap-2 rounded-lg border border-border bg-card/60 px-3 text-sm text-muted-foreground shadow-elev-sm transition-colors hover:border-accent/40 hover:text-foreground sm:w-56"
        >
          <Search className="size-4" />
          <span className="flex-1 text-left">Search…</span>
          <kbd className="hidden items-center gap-0.5 rounded border border-border bg-muted px-1.5 py-0.5 font-mono text-[10px] font-medium text-muted-foreground sm:inline-flex">
            <Command className="size-3" />K
          </kbd>
        </button>

        {stationItems.length > 0 ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                type="button"
                aria-label="Active station"
                className="hidden h-9 items-center gap-1.5 rounded-lg border border-border bg-card/60 px-2.5 text-xs text-foreground shadow-elev-sm outline-none transition-colors hover:border-accent/40 focus-visible:border-accent sm:inline-flex"
              >
                <Building2 className="size-3.5 text-muted-foreground" />
                <span className="max-w-[10rem] truncate">{activeLabel}</span>
                <ChevronDown className="size-3.5 text-muted-foreground" />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="max-h-[70vh] overflow-y-auto">
              <DropdownMenuLabel>Active station</DropdownMenuLabel>
              <DropdownMenuItem onSelect={() => setActiveStation(null)}>
                <span className="flex-1">All stations</span>
                {activeStationID == null ? <Check className="size-4 text-accent" /> : null}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              {stationItems.map((s) => (
                <DropdownMenuItem key={s.id} onSelect={() => setActiveStation(s.id)}>
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate text-foreground">{s.name}</span>
                    <span className="font-mono text-[11px] text-muted-foreground">{s.code}</span>
                  </span>
                  {activeStationID === s.id ? <Check className="size-4 text-accent" /> : null}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
      </div>

      <div className="flex items-center gap-0.5">
        <Tooltip label={isDark ? 'Light mode' : 'Dark mode'}>
          <Button
            variant="ghost"
            size="icon"
            aria-label="Toggle theme"
            onClick={() => setTheme(isDark ? 'light' : 'dark')}
          >
            {isDark ? <Sun className="size-4" /> : <Moon className="size-4" />}
          </Button>
        </Tooltip>
        <Tooltip label="Profile">
          <Link
            href="/profile"
            className="inline-flex size-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            aria-label="Profile"
          >
            <UserCircle className="size-[18px]" />
          </Link>
        </Tooltip>
        <span className="mx-1 h-5 w-px bg-border" aria-hidden="true" />
        <Tooltip label="Sign out">
          <Button variant="ghost" size="icon" aria-label="Sign out" onClick={handleLogout}>
            <LogOut className="size-4" />
          </Button>
        </Tooltip>
      </div>
    </header>
  );
}
