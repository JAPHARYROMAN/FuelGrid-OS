'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { Building2, Check, ChevronDown, Globe2 } from 'lucide-react';

import { SdkError, type EnterpriseScope } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Skeleton,
} from '@fuelgrid/ui';

import { usePermission } from '@/hooks/use-permissions';
import { api } from '@/lib/api';
import { useTenantStore } from '@/stores/tenant-store';

// The scope-switcher (Feature 13.1) lets a user with enterprise.scope.switch
// narrow the active chain view to a company / region / group / single station
// they are entitled to. Selecting "All stations" clears the scope (tenant-wide).
//
// The selection is a read-time lens persisted in the tenant store; it never
// widens access, because scoped reads continue to enforce station access
// server-side. A user holding a tenant-wide grant has nothing to switch between,
// so the switcher shows a single "All stations" state.
export function ScopeSwitcher() {
  const canSwitch = usePermission('enterprise.scope.switch');
  const activeScope = useTenantStore((s) => s.activeScope);
  const setActiveScope = useTenantStore((s) => s.setActiveScope);

  const scopes = useQuery({
    queryKey: ['enterprise', 'scopes'],
    queryFn: ({ signal }) => api.listEnterpriseScopes(signal),
    enabled: canSwitch !== false,
  });

  // Hidden entirely for users without the permission — there is nothing to act
  // on. The same 403 guard covers a backend that refuses the listing.
  const forbidden =
    canSwitch === false || (scopes.error instanceof SdkError && scopes.error.status === 403);
  if (forbidden) return null;

  if (scopes.isPending) {
    return <Skeleton className="h-9 w-44 rounded-lg" />;
  }
  if (scopes.isError) {
    // A non-403 failure is non-fatal: just don't render the switcher.
    return null;
  }

  const options = scopes.data.scopes;
  // Nothing to switch between (tenant-wide grant, or no enterprise scopes):
  // there is no meaningful choice to offer.
  if (scopes.data.tenant_wide || options.length === 0) {
    return null;
  }

  const activeLabel = activeScope ? activeScope.label : 'All stations';

  function select(opt: EnterpriseScope | null) {
    if (opt == null) {
      setActiveScope(null);
      return;
    }
    setActiveScope({ type: opt.scope_type, id: opt.scope_id, label: opt.label });
  }

  function isActive(opt: EnterpriseScope): boolean {
    return (
      activeScope != null && activeScope.type === opt.scope_type && activeScope.id === opt.scope_id
    );
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="secondary" size="sm" className="gap-1.5" aria-label="Active scope">
          {activeScope ? (
            <Building2 className="size-3.5 text-muted-foreground" />
          ) : (
            <Globe2 className="size-3.5 text-muted-foreground" />
          )}
          <span className="max-w-[12rem] truncate">{activeLabel}</span>
          <ChevronDown className="size-3.5 text-muted-foreground" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-h-[70vh] w-64 overflow-y-auto">
        <DropdownMenuLabel>Active scope</DropdownMenuLabel>
        <DropdownMenuItem onSelect={() => select(null)}>
          <span className="flex-1">All stations</span>
          {activeScope == null ? <Check className="size-4 text-accent" /> : null}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        {options.map((opt) => (
          <DropdownMenuItem
            key={`${opt.scope_type}:${opt.scope_id ?? 'none'}`}
            onSelect={() => select(opt)}
          >
            <span className="flex min-w-0 flex-1 flex-col">
              <span className="truncate text-foreground">{opt.label}</span>
              <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                <Badge tone="neutral">{opt.scope_type}</Badge>
                {opt.station_count} station{opt.station_count === 1 ? '' : 's'}
              </span>
            </span>
            {isActive(opt) ? <Check className="size-4 text-accent" /> : null}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
