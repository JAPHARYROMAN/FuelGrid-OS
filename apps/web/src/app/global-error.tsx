'use client';

import * as React from 'react';

import { reportError } from '@/lib/sentry';

/**
 * Root error boundary. This catches render errors thrown above (or in) the
 * root layout — when it fires, the layout (and globals.css) is replaced, so
 * we must render our own <html>/<body> and CANNOT rely on Tailwind tokens or
 * @fuelgrid/ui being styled. Inline styles keep the fallback legible even when
 * the stylesheet never loaded. Segment boundaries (app/(dashboard)/error.tsx,
 * app/(auth)/error.tsx) handle the common case with the real design system;
 * this is the last line of defence.
 */
export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  React.useEffect(() => {
    reportError(error, { boundary: 'global' });
  }, [error]);

  return (
    <html lang="en">
      <body
        style={{
          margin: 0,
          minHeight: '100vh',
          display: 'grid',
          placeItems: 'center',
          padding: '1.5rem',
          fontFamily:
            'ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, Helvetica, Arial, sans-serif',
          background: '#0a0a0a',
          color: '#fafafa',
        }}
      >
        <div
          style={{
            width: '100%',
            maxWidth: '28rem',
            borderRadius: '0.75rem',
            border: '1px solid rgba(248, 113, 113, 0.4)',
            background: 'rgba(248, 113, 113, 0.06)',
            padding: '2rem',
            textAlign: 'center',
          }}
          role="alert"
        >
          <h1 style={{ margin: '0 0 0.5rem', fontSize: '1.125rem', fontWeight: 600 }}>
            Something went wrong
          </h1>
          <p style={{ margin: '0 0 1.5rem', fontSize: '0.875rem', color: '#a1a1aa' }}>
            An unexpected error broke this page. The issue has been reported. You can try again, and
            if it keeps happening, reload the app.
          </p>
          {error.digest ? (
            <p style={{ margin: '0 0 1.5rem', fontSize: '0.75rem', color: '#71717a' }}>
              Reference: {error.digest}
            </p>
          ) : null}
          <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'center' }}>
            <button
              type="button"
              onClick={() => reset()}
              style={{
                height: '2.5rem',
                padding: '0 1rem',
                borderRadius: '0.375rem',
                border: 'none',
                cursor: 'pointer',
                fontSize: '0.875rem',
                fontWeight: 500,
                background: '#fafafa',
                color: '#0a0a0a',
              }}
            >
              Try again
            </button>
            <a
              href="/command-center"
              style={{
                height: '2.5rem',
                display: 'inline-flex',
                alignItems: 'center',
                padding: '0 1rem',
                borderRadius: '0.375rem',
                border: '1px solid #3f3f46',
                textDecoration: 'none',
                fontSize: '0.875rem',
                fontWeight: 500,
                color: '#fafafa',
              }}
            >
              Reload app
            </a>
          </div>
        </div>
      </body>
    </html>
  );
}
