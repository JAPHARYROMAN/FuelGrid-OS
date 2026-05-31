import type { MetadataRoute } from 'next';

const appName = process.env.NEXT_PUBLIC_APP_NAME ?? 'FuelGrid OS';

/**
 * Web App Manifest, served by Next.js at /manifest.webmanifest.
 *
 * Scope is deliberately "installable PWA basics" only: name, icons, and
 * standalone display so the app can be added to a home screen. There is NO
 * service worker yet, so this does not provide offline support — see
 * docs/architecture.md and the Phase-14 roadmap for the planned offline work.
 */
export default function manifest(): MetadataRoute.Manifest {
  return {
    name: appName,
    short_name: 'FuelGrid',
    description: 'The operating system for modern fuel businesses.',
    start_url: '/',
    scope: '/',
    display: 'standalone',
    background_color: '#0a0f1a',
    theme_color: '#3b82f6',
    icons: [
      {
        src: '/icons/icon-192.png',
        sizes: '192x192',
        type: 'image/png',
        purpose: 'any',
      },
      {
        src: '/icons/icon-512.png',
        sizes: '512x512',
        type: 'image/png',
        purpose: 'any',
      },
      {
        src: '/icons/icon-512.png',
        sizes: '512x512',
        type: 'image/png',
        purpose: 'maskable',
      },
    ],
  };
}
