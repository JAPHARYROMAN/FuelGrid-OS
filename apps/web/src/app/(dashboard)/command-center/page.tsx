'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { LayoutDashboard } from 'lucide-react';

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  LoadingState,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { setSentryUser } from '@/lib/sentry';

export default function CommandCenterPage() {
  const meQuery = useQuery({
    queryKey: ['me'],
    queryFn: ({ signal }) => api.me(signal),
  });

  // Associate Sentry events with the signed-in user once /me resolves. No-op
  // when Sentry is unconfigured. This is the earliest point the app reliably
  // has the user/tenant id (the login response carries only a token).
  const me = meQuery.data;
  React.useEffect(() => {
    if (me) setSentryUser({ id: me.user_id, tenantId: me.tenant_id });
  }, [me]);

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Command Center</h1>
        <p className="text-sm text-muted-foreground">
          The flagship surface. KPIs, network map, alerts, AI summary — they all land here as the
          operations layer comes online.
        </p>
      </header>

      <section>
        {meQuery.isPending ? (
          <LoadingState title="Loading your session…" />
        ) : meQuery.isError ? (
          <ErrorState
            title="Couldn't load your session"
            description={String((meQuery.error as Error).message)}
            onRetry={() => meQuery.refetch()}
          />
        ) : (
          <Card>
            <CardHeader>
              <CardTitle>Session</CardTitle>
              <CardDescription>
                You're authenticated. The shell is the work of Stage 8; concrete dashboards arrive
                with the fuel domain in Phase 2.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <dl className="grid grid-cols-1 gap-4 text-sm md:grid-cols-3">
                <div className="flex flex-col gap-1">
                  <dt className="text-xs uppercase tracking-wider text-muted-foreground">User</dt>
                  <dd className="font-mono text-xs tabular-nums">{meQuery.data.user_id}</dd>
                </div>
                <div className="flex flex-col gap-1">
                  <dt className="text-xs uppercase tracking-wider text-muted-foreground">Tenant</dt>
                  <dd className="font-mono text-xs tabular-nums">{meQuery.data.tenant_id}</dd>
                </div>
                <div className="flex flex-col gap-1">
                  <dt className="text-xs uppercase tracking-wider text-muted-foreground">MFA</dt>
                  <dd className="text-sm">
                    {meQuery.data.mfa_satisfied ? 'Satisfied' : 'Not required'}
                  </dd>
                </div>
              </dl>
            </CardContent>
          </Card>
        )}
      </section>

      <section>
        <EmptyState
          title="No KPIs yet"
          description="Network revenue, liters sold, reconciliation status, and station ranking arrive once the inventory and finance layers ship."
          icon={<LayoutDashboard className="size-7" />}
        />
      </section>
    </div>
  );
}
