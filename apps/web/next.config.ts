import type { NextConfig } from 'next';

/**
 * The Content-Security-Policy is NOT set here.
 *
 * The CSP must carry a fresh per-request nonce so Next.js can nonce its inline
 * hydration bootstrap scripts (a static `script-src 'self'` with no nonce
 * blocks them and breaks hydration in a strict browser). A static header in
 * next.config.ts cannot be per-request, so the nonce-bearing CSP — including
 * the script-src / style-src / connect-src directives — is built and applied in
 * src/middleware.ts instead. Defining a second CSP here would conflict with /
 * override the middleware one, so only the non-CSP hardening headers live here.
 */
const securityHeaders = [
  { key: 'X-Frame-Options', value: 'DENY' },
  { key: 'X-Content-Type-Options', value: 'nosniff' },
  { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
  {
    key: 'Permissions-Policy',
    value: 'camera=(), microphone=(), geolocation=(), payment=(), usb=()',
  },
];

/**
 * Sentry source-map upload — env-gated opt-in, OFF by default.
 *
 * Uploading source maps lets Sentry symbolicate minified production stack
 * traces, but it depends on the heavier `@sentry/nextjs` integration (which
 * this app does not install today — see src/lib/sentry.ts, which uses the
 * lighter `@sentry/browser`) plus an auth token. To keep CI/dev/local builds
 * a no-op we gate the whole thing behind SENTRY_AUTH_TOKEN: when it is unset
 * (every default build, including CI), `maybeWithSentry` returns the config
 * untouched and nothing is uploaded.
 *
 * To enable later (no ci.yml change required here — that wiring is owned
 * elsewhere):
 *   1. add `@sentry/nextjs` to apps/web deps,
 *   2. set SENTRY_AUTH_TOKEN, SENTRY_ORG and SENTRY_PROJECT in the build env.
 * The dynamic require means a missing package is tolerated (logged + skipped)
 * rather than failing the build.
 */
function maybeWithSentry(config: NextConfig): NextConfig {
  const token = process.env.SENTRY_AUTH_TOKEN;
  if (!token) return config;
  try {
    // Resolved lazily so the dependency is only required when the upload is
    // actually opted into. eslint/ts: intentional dynamic require.
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const { withSentryConfig } = require('@sentry/nextjs') as {
      withSentryConfig: (cfg: NextConfig, opts: Record<string, unknown>) => NextConfig;
    };
    return withSentryConfig(config, {
      org: process.env.SENTRY_ORG,
      project: process.env.SENTRY_PROJECT,
      authToken: token,
      silent: true,
      // Only upload; do not let the plugin mutate runtime behaviour beyond maps.
      widenClientFileUpload: true,
    });
  } catch (err) {
    // Token set but the integration isn't installed — degrade to a no-op
    // instead of breaking the build.
    console.warn(
      '[next.config] SENTRY_AUTH_TOKEN set but @sentry/nextjs not installed; skipping source-map upload.',
      err,
    );
    return config;
  }
}

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

export default maybeWithSentry(nextConfig);
