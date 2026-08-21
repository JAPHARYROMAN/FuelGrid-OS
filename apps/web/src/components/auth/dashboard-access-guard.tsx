'use client';

import { useEffect } from 'react';
import { useRouter } from 'next/navigation';

import { ErrorState, LoadingState } from '@fuelgrid/ui';

import { usePermissions } from '@/hooks/use-permissions';
import { isAttendantOnlyAccess } from '@/lib/login-destination';

/**
 * Keeps dedicated attendant accounts out of the desktop application shell.
 * API permissions remain authoritative; this guard prevents the broader UI
 * from rendering while the actor's role-based access surface is resolved.
 */
export function DashboardAccessGuard({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const access = usePermissions();
  const attendantOnly = access.data ? isAttendantOnlyAccess(access.data) : false;

  useEffect(() => {
    if (attendantOnly) router.replace('/attendant');
  }, [attendantOnly, router]);

  if (access.isLoading || attendantOnly) {
    return (
      <div className="grid min-h-screen place-items-center p-6">
        <LoadingState title={attendantOnly ? 'Opening attendant app…' : 'Checking access…'} />
      </div>
    );
  }

  if (access.isError || !access.data) {
    return (
      <div className="grid min-h-screen place-items-center p-6">
        <ErrorState
          title="Could not verify access"
          description="Check your connection and try again."
          onRetry={() => void access.refetch()}
        />
      </div>
    );
  }

  return <>{children}</>;
}
