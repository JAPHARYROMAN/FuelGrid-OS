'use client';

import { useRouter } from 'next/navigation';
import { useTheme } from 'next-themes';
import { Command, LogOut, Moon, Search, Sun } from 'lucide-react';

import { SdkError } from '@fuelgrid/sdk';
import { Button } from '@fuelgrid/ui';

import { api } from '@/lib/api';
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

  async function handleLogout() {
    try {
      await api.logout();
    } catch (err) {
      // 401/expired/already-invalid is fine — we're logging out anyway.
      if (!(err instanceof SdkError) || err.status >= 500) {
        // Surface real server errors but don't block the local logout.
        console.error('logout failed', err);
      }
    }
    clearSession();
    resetTenantContext();
    router.replace('/login');
  }

  const isDark = (theme ?? resolvedTheme) === 'dark';

  return (
    <header className="flex h-14 items-center justify-between border-b border-border bg-card/40 px-4">
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

      <div className="flex items-center gap-1">
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
