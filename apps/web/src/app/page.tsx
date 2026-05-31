'use client';

import { useEffect } from 'react';
import { useRouter } from 'next/navigation';

import { useAuthStore } from '@/stores/auth-store';

/**
 * Root route is a thin redirector — authenticated users go to the
 * command center, the rest get the login screen. The real session lives
 * in the httpOnly cookie (checked by middleware); this client-side hop
 * uses the non-sensitive `authed` hint only to pick the destination
 * without a flash.
 */
export default function HomePage() {
  const router = useRouter();
  const hydrated = useAuthStore((s) => s.hydrated);
  const authed = useAuthStore((s) => s.authed);

  useEffect(() => {
    if (!hydrated) return;
    router.replace(authed ? '/command-center' : '/login');
  }, [hydrated, authed, router]);

  return null;
}
