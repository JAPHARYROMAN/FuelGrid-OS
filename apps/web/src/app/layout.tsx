import type { Metadata, Viewport } from 'next';

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

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className="font-sans antialiased">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
