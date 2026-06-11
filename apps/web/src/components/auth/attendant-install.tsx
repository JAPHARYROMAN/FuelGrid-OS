'use client';

import { useState } from 'react';
import dynamic from 'next/dynamic';
import { QrCode } from 'lucide-react';

/**
 * "Pump attendant? Install the app" affordance on the login screen (Mobile
 * Attendant App, Phase 1). Collapsed by default so the login form stays the
 * hero; expanding reveals a QR code that encodes `<origin>/attendant`, which
 * attendants scan with their phone to land on the attendant PWA (installable
 * via the existing web app manifest — no service worker yet, Phase 6).
 *
 * The QR renderer (qrcode.react, zero-dependency SVG) is lazy-loaded only
 * when the panel opens, so it adds nothing to the login bundle.
 */
const QRCodeSVG = dynamic(() => import('qrcode.react').then((m) => m.QRCodeSVG), {
  ssr: false,
  loading: () => (
    <div className="grid size-44 place-items-center text-xs text-muted-foreground">
      Loading code…
    </div>
  ),
});

export function AttendantInstall() {
  const [open, setOpen] = useState(false);
  // window is always defined here: this client component renders interactive
  // content only after the user clicks (open starts false).
  const url = open ? `${window.location.origin}/attendant` : '';

  return (
    <div className="mt-4 flex flex-col items-center gap-3 text-center">
      <button
        type="button"
        className="inline-flex min-h-11 items-center gap-1.5 rounded-md px-3 text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <QrCode className="size-3.5" aria-hidden />
        Pump attendant? Install the app
      </button>

      {open ? (
        <div className="flex flex-col items-center gap-2 rounded-xl border border-border bg-card p-4 shadow-elev-sm">
          <div className="rounded-lg bg-white p-3">
            <QRCodeSVG value={url} size={176} marginSize={0} title="Open the attendant app" />
          </div>
          <p className="max-w-[240px] text-xs text-muted-foreground">
            Scan with your phone camera, sign in, then use your browser&apos;s{' '}
            <span className="font-medium text-foreground">Add to Home Screen</span> to install.
          </p>
          <p className="font-mono text-[11px] text-muted-foreground">{url}</p>
        </div>
      ) : null}
    </div>
  );
}
