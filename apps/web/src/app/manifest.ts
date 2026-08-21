import type { MetadataRoute } from 'next';

/**
 * Web App Manifest, served by Next.js at /manifest.webmanifest.
 *
 * FuelGrid's install affordance is specifically for pump attendants, so the
 * installed app must reopen the touch-first attendant workspace rather than
 * the desktop command centre. The root-scoped service worker provides the
 * attendant shell and offline action queue.
 */
export default function manifest(): MetadataRoute.Manifest {
  return {
    id: '/attendant',
    name: 'FuelGrid Attendant',
    short_name: 'Attendant',
    description: 'Pump attendant shift readings, collections, and field workflow.',
    start_url: '/attendant',
    scope: '/',
    display: 'standalone',
    orientation: 'portrait',
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
