'use client';

import * as Sentry from '@sentry/browser';

/**
 * initSentry wires the browser SDK exactly once, only if a DSN is set.
 * Skipping the heavier @sentry/nextjs integration on purpose — that
 * package injects Next.js config, owns source-map upload, and adds
 * meaningful build complexity. We can swap it in when there's a
 * concrete need (server-side error capture, edge runtime, sourcemaps).
 */
let initialized = false;

export function initSentry() {
  if (initialized) return;
  if (typeof window === 'undefined') return;

  const dsn = process.env.NEXT_PUBLIC_SENTRY_DSN;
  if (!dsn) return;

  Sentry.init({
    dsn,
    environment: process.env.NEXT_PUBLIC_APP_ENV ?? 'development',
    // Default sample rate keeps the free tier from filling up on a
    // misbehaving page. Override per environment.
    tracesSampleRate: Number(process.env.NEXT_PUBLIC_SENTRY_TRACES_SAMPLE_RATE ?? 0),
  });

  initialized = true;
}

/** True only once init has actually wired Sentry (i.e. a DSN was set). */
function isActive(): boolean {
  return initialized;
}

/**
 * Capture an unexpected error. NO-OP when Sentry was never initialized (no
 * DSN), so dev/CI/build are unaffected. `requestId` (from SdkError) is set as
 * a tag so the captured event can be correlated with server logs/traces.
 */
export function captureError(error: unknown, requestId?: string | null) {
  if (!isActive()) return;
  Sentry.captureException(error, requestId ? { tags: { request_id: requestId } } : undefined);
}

/**
 * reportError is the clean entry point for *render* errors caught by React
 * error boundaries (app/global-error.tsx, app/(segment)/error.tsx). Unlike
 * `captureError` (which correlates an SdkError with a server request id), a
 * render error carries no request id — instead Next.js attaches a `digest`
 * that maps to the server-side log line, plus we tag the boundary that caught
 * it. NO-OP when Sentry was never initialized (no DSN), so dev/CI/build are
 * unaffected.
 */
export function reportError(error: Error & { digest?: string }, context?: { boundary?: string }) {
  if (!isActive()) return;
  const tags: Record<string, string> = {};
  if (error.digest) tags.digest = error.digest;
  if (context?.boundary) tags.boundary = context.boundary;
  Sentry.captureException(error, Object.keys(tags).length ? { tags } : undefined);
}

/** Associate subsequent events with the signed-in user. NO-OP when inactive. */
export function setSentryUser(user: { id?: string; tenantId?: string }) {
  if (!isActive()) return;
  Sentry.setUser({ id: user.id, tenant_id: user.tenantId });
}

/** Clear the associated user on logout. NO-OP when inactive. */
export function clearSentryUser() {
  if (!isActive()) return;
  Sentry.setUser(null);
}
