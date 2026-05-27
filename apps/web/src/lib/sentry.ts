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
