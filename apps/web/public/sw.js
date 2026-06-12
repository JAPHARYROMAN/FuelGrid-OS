/* eslint-disable */
/**
 * FuelGrid Attendant service worker (Mobile Attendant Phase 6a).
 *
 * Scope: app-shell offline for the attendant PWA only.
 *
 *   - cache-first for immutable static assets (/_next/static, /icons,
 *     /manifest.webmanifest) — content-hashed or rarely changing.
 *   - network-first WITH cache fallback for /attendant page navigations, so
 *     the app still OPENS offline with the last-known shell. The cached
 *     document is only the client-rendered app shell; shift data comes from
 *     the API at runtime (and from the page's own snapshot cache offline).
 *
 * NEVER cached (PRD §16 — no sensitive data in SW caches):
 *   - anything under /api/ (the /api/bff proxy carries the httpOnly session
 *     cookie and returns shift/financial data),
 *   - any non-GET request,
 *   - any cross-origin request,
 *   - navigations outside /attendant (the desktop dashboard is untouched).
 *
 * Updates: a new SW waits (no skipWaiting on install) so the page can show a
 * gentle "App updated — reload" affordance; the page posts SKIP_WAITING when
 * the attendant accepts, then reloads on controllerchange.
 */

const VERSION = 'v1';
const STATIC_CACHE = `fg-attendant-static-${VERSION}`;
const SHELL_CACHE = `fg-attendant-shell-${VERSION}`;
const KNOWN_CACHES = [STATIC_CACHE, SHELL_CACHE];

/** The navigation fallback document cached for the attendant shell. */
const SHELL_FALLBACK_PATH = '/attendant';

self.addEventListener('install', () => {
  // Deliberately no skipWaiting(): the update flow is user-controlled.
});

self.addEventListener('message', (event) => {
  if (event.data && event.data.type === 'SKIP_WAITING') {
    self.skipWaiting();
  }
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      // Drop caches from older SW versions.
      const names = await caches.keys();
      await Promise.all(
        names
          .filter((n) => n.startsWith('fg-attendant-') && !KNOWN_CACHES.includes(n))
          .map((n) => caches.delete(n)),
      );
      await self.clients.claim();
    })(),
  );
});

/** Whether this GET request is a static asset we may cache-first. */
function isStaticAsset(url) {
  return (
    url.pathname.startsWith('/_next/static/') ||
    url.pathname.startsWith('/icons/') ||
    url.pathname === '/manifest.webmanifest' ||
    url.pathname === '/favicon.ico'
  );
}

/** Cache-first: serve from cache, fill the cache from the network on miss. */
async function cacheFirst(request) {
  const cached = await caches.match(request);
  if (cached) return cached;
  const response = await fetch(request);
  if (response.ok) {
    const cache = await caches.open(STATIC_CACHE);
    await cache.put(request, response.clone());
  }
  return response;
}

/**
 * Network-first for attendant shell navigations: fresh document when online
 * (cached as the new fallback), last-known shell when offline. Per-path entry
 * plus the /attendant root as the final fallback so a deep link still opens.
 */
async function networkFirstShell(request, url) {
  try {
    const response = await fetch(request);
    if (response.ok) {
      const cache = await caches.open(SHELL_CACHE);
      await cache.put(url.pathname, response.clone());
    }
    return response;
  } catch (err) {
    const cache = await caches.open(SHELL_CACHE);
    const byPath = await cache.match(url.pathname);
    if (byPath) return byPath;
    const fallback = await cache.match(SHELL_FALLBACK_PATH);
    if (fallback) return fallback;
    throw err;
  }
}

self.addEventListener('fetch', (event) => {
  const request = event.request;

  // Non-GET requests are NEVER intercepted or cached — mutations (and their
  // offline queueing) are handled entirely by the page.
  if (request.method !== 'GET') return;

  const url = new URL(request.url);

  // Same-origin only; and never any API/BFF traffic — those responses carry
  // session-scoped shift and financial data that must not persist in SW
  // caches (PRD §16).
  if (url.origin !== self.location.origin) return;
  if (url.pathname.startsWith('/api/')) return;

  if (request.mode === 'navigate') {
    // Only the attendant shell gets an offline navigation fallback; the
    // desktop dashboard stays untouched.
    if (url.pathname === '/attendant' || url.pathname.startsWith('/attendant/')) {
      event.respondWith(networkFirstShell(request, url));
    }
    return;
  }

  if (isStaticAsset(url)) {
    event.respondWith(cacheFirst(request));
  }
});
