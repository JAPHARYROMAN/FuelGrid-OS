'use client';

import * as React from 'react';
import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider } from 'next-themes';

import { SdkError } from '@fuelgrid/sdk';

import { handleUnauthorized } from '@/lib/api';
import { captureError, initSentry } from '@/lib/sentry';
import { toast } from '@/lib/toast';

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

/**
 * Mutation failures must be *visible* (PAGE-008) — a silent onSuccess-only
 * mutation used to swallow the error. This is the global backstop: every
 * mutation that doesn't surface its own message still pops a toast. A 401
 * still routes through reportError to the logout backstop and is not toasted.
 */
function reportMutationError(error: unknown) {
  if (error instanceof SdkError) {
    if (error.status === 401) {
      reportError(error);
      return;
    }
    toast.error(
      error.status === 403 ? "You don't have permission" : 'Action failed',
      error.message,
    );
    reportError(error);
    return;
  }
  if (error instanceof DOMException && error.name === 'AbortError') return;
  toast.error('Something went wrong', error instanceof Error ? error.message : undefined);
  reportError(error);
}

function makeQueryClient() {
  return new QueryClient({
    queryCache: new QueryCache({ onError: reportError }),
    mutationCache: new MutationCache({ onError: reportMutationError }),
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

export function Providers({
  children,
  nonce,
}: {
  children: React.ReactNode;
  /**
   * Per-request CSP nonce (forwarded by middleware on `x-nonce`, read in the
   * root server layout). next-themes injects an inline anti-flash <script>;
   * passing the nonce lets that script satisfy the strict `script-src`
   * `'nonce-…'` policy instead of being blocked and logging a CSP warning.
   */
  nonce?: string;
}) {
  const queryClient = getQueryClient();

  // Init Sentry once on first client render. The helper is a no-op when
  // NEXT_PUBLIC_SENTRY_DSN is unset.
  React.useEffect(() => {
    initSentry();
  }, []);

  return (
    <ThemeProvider
      attribute="class"
      defaultTheme="dark"
      enableSystem={false}
      themes={['light', 'dark', 'navy']}
      nonce={nonce}
    >
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    </ThemeProvider>
  );
}
