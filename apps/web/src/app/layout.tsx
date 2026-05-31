import type { Metadata, Viewport } from 'next';
import { headers } from 'next/headers';

import { Providers } from './providers';
import './globals.css';

const appName = process.env.NEXT_PUBLIC_APP_NAME ?? 'FuelGrid OS';

export const metadata: Metadata = {
  title: appName,
  description: 'The operating system for modern fuel businesses.',
  // Served by app/manifest.ts at /manifest.webmanifest. Installable-PWA
  // basics only — no service worker / offline support yet (see Phase 14).
  manifest: '/manifest.webmanifest',
  applicationName: appName,
  appleWebApp: {
    capable: true,
    title: appName,
    statusBarStyle: 'default',
  },
  icons: {
    icon: [
      { url: '/icons/icon-192.png', sizes: '192x192', type: 'image/png' },
      { url: '/icons/icon-512.png', sizes: '512x512', type: 'image/png' },
    ],
    apple: [{ url: '/icons/icon-192.png', sizes: '192x192', type: 'image/png' }],
  },
};

export const viewport: Viewport = {
  themeColor: '#3b82f6',
};

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  // Read the per-request nonce that src/middleware.ts forwards on the `x-nonce`
  // request header. Reading headers() also opts every route into dynamic
  // rendering, which is required for Next to inject the per-request nonce onto
  // its inline hydration/bootstrap scripts — a statically prerendered page is
  // generated at build time and could not carry a request-scoped nonce, so the
  // nonce CSP would block hydration. (The nonce value itself is consumed by
  // Next internally via the request CSP header; reading it here is what makes
  // the render dynamic.)
  await headers();

  return (
    <html lang="en" suppressHydrationWarning>
      <body className="font-sans antialiased">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
