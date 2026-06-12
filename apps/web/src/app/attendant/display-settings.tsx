'use client';

import { useState } from 'react';
import { Settings, X } from 'lucide-react';

import { useAttendantPrefs, useT, type Contrast, type Locale, type TextSize } from '@/lib/i18n';

import { useSheetFocusTrap } from './use-focus-trap';

/**
 * "Display & language" affordance in the attendant header (Phase 6b, PRD
 * §15.1/§15.2): a gear button opening a small bottom sheet with three plain
 * choices — language (English/Kiswahili), text size (Normal/Large), contrast
 * (Normal/High). Every change applies instantly (context update, no reload)
 * and persists to localStorage, so it works fully offline.
 */
export function DisplaySettingsButton() {
  const t = useT();
  const [open, setOpen] = useState(false);

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="flex size-11 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
        aria-haspopup="dialog"
        aria-label={t.settings.title}
      >
        <Settings className="size-5" aria-hidden />
      </button>
      {open ? <DisplaySettingsSheet onClose={() => setOpen(false)} /> : null}
    </>
  );
}

export function DisplaySettingsSheet({ onClose }: { onClose: () => void }) {
  const t = useT();
  const prefs = useAttendantPrefs();
  const panelRef = useSheetFocusTrap(onClose);

  return (
    <div
      className="fixed inset-0 z-50 flex items-end justify-center bg-black/50"
      role="dialog"
      aria-modal="true"
      aria-label={t.settings.title}
    >
      <div
        ref={panelRef}
        className="flex w-full max-w-md flex-col rounded-t-2xl border border-border bg-background"
      >
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <h2 className="text-base font-semibold">{t.settings.title}</h2>
          <button
            type="button"
            className="flex size-11 items-center justify-center rounded-md text-muted-foreground hover:text-foreground"
            onClick={onClose}
            aria-label={t.settings.close}
          >
            <X className="size-5" aria-hidden />
          </button>
        </div>

        <div className="flex flex-col gap-4 px-4 py-4">
          <ChoiceRow<Locale>
            legend={t.settings.language}
            value={prefs.locale}
            options={[
              { value: 'en', label: t.settings.english },
              { value: 'sw', label: t.settings.swahili },
            ]}
            onChange={prefs.setLocale}
          />
          <ChoiceRow<TextSize>
            legend={t.settings.textSize}
            value={prefs.textSize}
            options={[
              { value: 'normal', label: t.settings.textNormal },
              { value: 'large', label: t.settings.textLarge },
            ]}
            onChange={prefs.setTextSize}
          />
          <ChoiceRow<Contrast>
            legend={t.settings.contrast}
            value={prefs.contrast}
            options={[
              { value: 'normal', label: t.settings.contrastNormal },
              { value: 'high', label: t.settings.contrastHigh },
            ]}
            onChange={prefs.setContrast}
          />
        </div>

        <div className="border-t border-border px-4 py-3">
          <button
            type="button"
            className="flex h-12 w-full items-center justify-center rounded-lg bg-accent text-base font-medium text-accent-foreground"
            onClick={onClose}
          >
            {t.settings.done}
          </button>
        </div>
      </div>
    </div>
  );
}

/**
 * A two-option segmented choice: large touch targets (h-12 ≥ 44px), the
 * active option marked with aria-pressed AND a visible filled style + check —
 * never colour alone (PRD §15.1).
 */
function ChoiceRow<T extends string>({
  legend,
  value,
  options,
  onChange,
}: {
  legend: string;
  value: T;
  options: Array<{ value: T; label: string }>;
  onChange: (value: T) => void;
}) {
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="pb-2 text-sm font-medium text-muted-foreground">{legend}</legend>
      <div className="flex gap-2">
        {options.map((option) => {
          const active = option.value === value;
          return (
            <button
              key={option.value}
              type="button"
              aria-pressed={active}
              className={
                'h-12 flex-1 rounded-lg border text-base font-medium ' +
                (active
                  ? 'border-accent bg-accent text-accent-foreground'
                  : 'border-border bg-background text-foreground hover:bg-muted')
              }
              onClick={() => onChange(option.value)}
            >
              {option.label}
            </button>
          );
        })}
      </div>
    </fieldset>
  );
}
