'use client';

import * as React from 'react';
import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider } from 'next-themes';

import { SdkError } from '@fuelgrid/sdk';

import { handleUnauthorized } from '@/lib/api';
import { captureError, initSentry } from '@/lib/sentry';

/**
 * Central error sink for every query and mutation. Two jobs:
 *   (a) a 401 anywhere is the logout backstop — clear the session + redirect;
 *   (b) anything else unexpected is captured to Sentry, tagged with the
 *       SdkError's request id for server-log correlation.
 * Expected auth errors (401, plus 403 permission denials) are NOT captured —
 * they're normal flow, not bugs.
 */
function reportError(error: unknown) {
  if (error instanceof SdkError) {
    if (error.status === 401) {
      handleUnauthorized();
      return;
    }
    // Don't noise up Sentry with routine permission denials.
    if (error.status === 403) return;
    captureError(error, error.requestId);
    return;
  }
  // AbortErrors are deliberate cancellations, not failures.
  if (error instanceof DOMException && error.name === 'AbortError') return;
  captureError(error);
}

function makeQueryClient() {
  return new QueryClient({
    queryCache: new QueryCache({ onError: reportError }),
    mutationCache: new MutationCache({ onError: reportError }),
    defaultOptions: {
      queries: {
        // Most dashboard data is read often and changes rarely; tune
        // per-query when something needs livelier refresh.
        staleTime: 30 * 1000,
        gcTime: 5 * 60 * 1000,
        refetchOnWindowFocus: false,
        retry: (failureCount, error) => {
          // Don't retry 4xx — they're typically auth/permission, not
          // transient. Retry network failures up to twice.
          const status = (error as { status?: number } | null)?.status;
          if (status && status >= 400 && status < 500) return false;
          return failureCount < 2;
        },
      },
      mutations: {
        retry: false,
      },
    },
  });
}

let browserQueryClient: QueryClient | undefined;

function getQueryClient() {
  if (typeof window === 'undefined') {
    // Server: always make a new client per request.
    return makeQueryClient();
  }
  // Browser: reuse one client across renders so caches survive.
  browserQueryClient ??= makeQueryClient();
  return browserQueryClient;
}

export function Providers({ children }: { children: React.ReactNode }) {
  const queryClient = getQueryClient();

  // Init Sentry once on first client render. The helper is a no-op when
  // NEXT_PUBLIC_SENTRY_DSN is unset.
  React.useEffect(() => {
    initSentry();
  }, []);

  return (
    <ThemeProvider attribute="class" defaultTheme="dark" enableSystem={false}>
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    </ThemeProvider>
  );
}
