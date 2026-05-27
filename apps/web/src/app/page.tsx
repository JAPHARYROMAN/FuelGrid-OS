'use client';

import { useEffect } from 'react';
import { useRouter } from 'next/navigation';

import { useAuthStore } from '@/stores/auth-store';

/**
 * Root route is a thin redirector — authenticated users go to the
 * command center, the rest get the login screen. Doing this on the
 * client (rather than via server-side middleware) keeps Stage 8 within
 * the localStorage-only auth contract; cookie-backed middleware can
 * replace this in a later stage.
 */
export default function HomePage() {
  const router = useRouter();
  const hydrated = useAuthStore((s) => s.hydrated);
  const token = useAuthStore((s) => s.token);

  useEffect(() => {
    if (!hydrated) return;
    router.replace(token ? '/command-center' : '/login');
  }, [hydrated, token, router]);

  return null;
}
