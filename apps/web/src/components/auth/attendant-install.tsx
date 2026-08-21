'use client';

import { useEffect, useState } from 'react';
import dynamic from 'next/dynamic';
import Link from 'next/link';
import { Download, ExternalLink, QrCode } from 'lucide-react';

import { AttendantPrefsProvider, useAttendantPrefs, useT } from '@/lib/i18n';

/**
 * "Pump attendant? Install the app" affordance on the login screen (Mobile
 * Attendant App, Phase 1). Collapsed by default so the login form stays the
 * hero; expanding reveals a QR code that encodes `<origin>/attendant`, which
 * attendants scan with their phone to land on the attendant PWA (installable
 * via the existing web app manifest).
 *
 * Phase 6b: a compact language selector (English | Kiswahili) sits right next
 * to the affordance — the PRD asks for the selector to be easy to find, and
 * the login page is the attendant's first screen. It shares the attendant
 * app's persisted locale (localStorage), so the choice made here carries into
 * the app, offline included. The rest of the login page (desktop dashboard
 * surface) deliberately stays English.
 *
 * The QR renderer (qrcode.react, zero-dependency SVG) is lazy-loaded only
 * when the panel opens, so it adds nothing to the login bundle.
 */
const QRCodeSVG = dynamic(() => import('qrcode.react').then((m) => m.QRCodeSVG), {
  ssr: false,
  loading: () => <QRCodeLoading />,
});

function QRCodeLoading() {
  const t = useT();
  return (
    <div className="grid size-44 place-items-center text-xs text-muted-foreground">
      {t.install.loadingCode}
    </div>
  );
}

interface BeforeInstallPromptEvent extends Event {
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed'; platform: string }>;
}

export function AttendantInstall() {
  return (
    <AttendantPrefsProvider>
      <AttendantInstallInner />
    </AttendantPrefsProvider>
  );
}

function AttendantInstallInner() {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [installPrompt, setInstallPrompt] = useState<BeforeInstallPromptEvent | null>(null);
  const [installing, setInstalling] = useState(false);

  useEffect(() => {
    const capturePrompt = (event: Event) => {
      event.preventDefault();
      setInstallPrompt(event as BeforeInstallPromptEvent);
    };
    window.addEventListener('beforeinstallprompt', capturePrompt);
    return () => window.removeEventListener('beforeinstallprompt', capturePrompt);
  }, []);

  async function installOnThisDevice() {
    if (!installPrompt) return;
    setInstalling(true);
    try {
      await installPrompt.prompt();
      await installPrompt.userChoice;
      // A beforeinstallprompt event can be used only once, regardless of the
      // user's choice. The browser will emit another event when eligible.
      setInstallPrompt(null);
    } finally {
      setInstalling(false);
    }
  }

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
        {t.install.prompt}
      </button>

      <LanguageToggle />

      {open ? (
        <div className="flex flex-col items-center gap-2 rounded-xl border border-border bg-card p-4 shadow-elev-sm">
          <div className="rounded-lg bg-white p-3">
            <QRCodeSVG value={url} size={176} marginSize={0} title={t.install.qrTitle} />
          </div>
          <p className="max-w-[240px] text-xs text-muted-foreground">
            {t.install.scanInstruction1}
            <span className="font-medium text-foreground">{t.install.addToHomeScreen}</span>
            {t.install.scanInstruction2}
          </p>
          <p className="font-mono text-[11px] text-muted-foreground">{url}</p>
          <div className="mt-1 flex w-full flex-col gap-2">
            {installPrompt ? (
              <button
                type="button"
                className="flex h-12 w-full items-center justify-center gap-2 rounded-lg bg-accent px-4 text-sm font-medium text-accent-foreground"
                disabled={installing}
                onClick={installOnThisDevice}
              >
                <Download className="size-4" aria-hidden />
                {installing ? t.install.installing : t.install.installThisDevice}
              </button>
            ) : null}
            <Link
              href="/attendant"
              className="flex h-12 w-full items-center justify-center gap-2 rounded-lg border border-border px-4 text-sm font-medium text-foreground"
            >
              <ExternalLink className="size-4" aria-hidden />
              {t.install.openApp}
            </Link>
          </div>
        </div>
      ) : null}
    </div>
  );
}

/**
 * The attendant language selector on the login page (PRD §15.2: easy to
 * find). Two pill buttons — the active one is filled AND aria-pressed, never
 * colour-only. Switching is instant and persists offline-safe.
 */
function LanguageToggle() {
  const t = useT();
  const { locale, setLocale } = useAttendantPrefs();

  return (
    <div className="flex items-center gap-1.5" role="group" aria-label={t.install.languageLabel}>
      <span className="text-xs text-muted-foreground">{t.install.languageLabel}:</span>
      {(
        [
          { value: 'en', label: t.settings.english },
          { value: 'sw', label: t.settings.swahili },
        ] as const
      ).map((option) => {
        const active = option.value === locale;
        return (
          <button
            key={option.value}
            type="button"
            aria-pressed={active}
            className={
              'min-h-11 rounded-full border px-3 text-xs font-medium ' +
              (active
                ? 'border-accent bg-accent text-accent-foreground'
                : 'border-border bg-background text-muted-foreground hover:text-foreground')
            }
            onClick={() => setLocale(option.value)}
          >
            {option.label}
          </button>
        );
      })}
    </div>
  );
}
