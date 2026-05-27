'use client';

import { useEffect } from 'react';
import { usePathname, useRouter } from 'next/navigation';

import { LoadingState } from '@fuelgrid/ui';

import { useAuthStore } from '@/stores/auth-store';

/**
 * ProtectedRoute is the client-side guard for the authenticated
 * surface area of the app. It does three things:
 *
 *   1. Waits for the auth store to rehydrate from localStorage so we
 *      never flash-redirect a user whose token is still in flight.
 *   2. Redirects to /login with the current URL preserved as `?next=`
 *      so post-login navigation lands them back where they came from.
 *   3. Renders nothing during the redirect to avoid the protected UI
 *      briefly painting before the navigation fires.
 */
export function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const hydrated = useAuthStore((s) => s.hydrated);
  const token = useAuthStore((s) => s.token);

  useEffect(() => {
    if (!hydrated) return;
    if (!token) {
      const next = encodeURIComponent(pathname || '/');
      router.replace(`/login?next=${next}`);
    }
  }, [hydrated, token, pathname, router]);

  if (!hydrated) {
    return (
      <div className="grid min-h-screen place-items-center p-6">
        <LoadingState title="Checking session…" />
      </div>
    );
  }

  if (!token) return null;

  return <>{children}</>;
}
