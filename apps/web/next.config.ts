import type { NextConfig } from 'next';

/**
 * The browser talks to the API directly (fetch from the SDK), so the API
 * origin must be allowed in connect-src. Read it from the public env vars
 * the app already uses; fall back to 'self' when neither is defined.
 */
const apiOrigin = process.env.NEXT_PUBLIC_API_BASE_URL ?? process.env.NEXT_PUBLIC_API_URL ?? '';

/**
 * When Sentry is configured, its browser SDK POSTs events to the ingest host
 * encoded in the DSN (e.g. https://<key>@o123.ingest.sentry.io/456). Add only
 * that origin to connect-src — conditionally, so CSP is unchanged (and not
 * weakened) when no DSN is set. A malformed DSN simply contributes nothing.
 */
function sentryConnectSrc(): string {
  const dsn = process.env.NEXT_PUBLIC_SENTRY_DSN;
  if (!dsn) return '';
  try {
    return new URL(dsn).origin;
  } catch {
    return '';
  }
}

const connectSrc = ["'self'", apiOrigin, sentryConnectSrc()].filter(Boolean).join(' ');

/**
 * Strict Content-Security-Policy plus hardening headers applied to every
 * route. Kept conservative: only 'self' for scripts, 'unsafe-inline' is
 * limited to styles (required by Tailwind's injected style tags), and the
 * page may not be framed.
 */
const contentSecurityPolicy = [
  "default-src 'self'",
  `connect-src ${connectSrc}`,
  // PWA: the web app manifest and its icon PNGs are served same-origin.
  // Explicit (does not weaken default-src 'self', which already covers these).
  "manifest-src 'self'",
  "img-src 'self' data:",
  "style-src 'self' 'unsafe-inline'",
  "script-src 'self'",
  "font-src 'self' data:",
  "object-src 'none'",
  "frame-ancestors 'none'",
  "base-uri 'self'",
  "form-action 'self'",
].join('; ');

const securityHeaders = [
  { key: 'Content-Security-Policy', value: contentSecurityPolicy },
  { key: 'X-Frame-Options', value: 'DENY' },
  { key: 'X-Content-Type-Options', value: 'nosniff' },
  { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
  {
    key: 'Permissions-Policy',
    value: 'camera=(), microphone=(), geolocation=(), payment=(), usb=()',
  },
];

const nextConfig: NextConfig = {
  reactStrictMode: true,
  /**
   * apps/web imports source from sibling workspace packages
   * (@fuelgrid/ui, @fuelgrid/sdk). transpilePackages tells Next.js to
   * compile their TypeScript instead of treating them as published
   * pre-built modules.
   */
  transpilePackages: ['@fuelgrid/ui', '@fuelgrid/sdk'],
  async headers() {
    return [
      {
        source: '/:path*',
        headers: securityHeaders,
      },
    ];
  },
};

export default nextConfig;
