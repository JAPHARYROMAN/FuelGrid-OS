'use client';

import { useEffect, useRef, useState } from 'react';
import { RefreshCw } from 'lucide-react';

import { useT } from '@/lib/i18n';

/**
 * Service-worker registration + update affordance for the attendant shell
 * (Mobile Attendant Phase 6a).
 *
 * CSP: registration happens from this bundled module (no inline <script>), so
 * the strict nonce-based `script-src` is untouched; /sw.js itself is fetched
 * by the SW machinery under `worker-src` → `default-src 'self'`.
 *
 * New workers normally activate immediately so broken/stale installed shells
 * recover without user intervention. The waiting-worker affordance remains as
 * a defensive fallback for browsers that defer activation.
 */
export function ServiceWorkerManager() {
  const t = useT();
  const [waiting, setWaiting] = useState<ServiceWorker | null>(null);
  const reloadingRef = useRef(false);

  useEffect(() => {
    if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) return;
    // Dev builds churn assets constantly; the SW is a production concern.
    if (process.env.NODE_ENV !== 'production') return;

    let cancelled = false;

    navigator.serviceWorker
      .register('/sw.js', { updateViaCache: 'none' })
      .then((registration) => {
        if (cancelled) return;
        void registration.update();
        if (registration.waiting && navigator.serviceWorker.controller) {
          setWaiting(registration.waiting);
        }
        registration.addEventListener('updatefound', () => {
          const installing = registration.installing;
          if (!installing) return;
          installing.addEventListener('statechange', () => {
            // "installed" with an existing controller = a NEW version is
            // waiting (first-ever install has no controller — nothing to
            // announce).
            if (installing.state === 'installed' && navigator.serviceWorker.controller) {
              setWaiting(registration.waiting ?? installing);
            }
          });
        });
      })
      .catch(() => {
        // Registration failure only means no offline shell — never block the app.
      });

    const onControllerChange = () => {
      if (reloadingRef.current) window.location.reload();
    };
    navigator.serviceWorker.addEventListener('controllerchange', onControllerChange);
    return () => {
      cancelled = true;
      navigator.serviceWorker.removeEventListener('controllerchange', onControllerChange);
    };
  }, []);

  if (!waiting) return null;

  return (
    <div className="mx-auto w-full max-w-md px-4 pt-2">
      <button
        type="button"
        className="flex h-11 w-full items-center justify-center gap-2 rounded-md bg-accent/10 text-sm font-medium text-accent"
        onClick={() => {
          reloadingRef.current = true;
          waiting.postMessage({ type: 'SKIP_WAITING' });
        }}
      >
        <RefreshCw className="size-4" aria-hidden />
        {t.sync.updateReady}
      </button>
    </div>
  );
}
