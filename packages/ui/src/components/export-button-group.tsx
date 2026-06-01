'use client';

import * as React from 'react';
import { Download, FileSpreadsheet, FileText } from 'lucide-react';

import { cn } from '../lib/cn';
import { Button } from './button';

/**
 * ExportButtonGroup — a consistent row of export buttons (CSV / PDF / XLSX).
 * Each button calls an async `onDownload` and tracks its own busy state, so one
 * export spinning does not block the others. When the caller reports the user
 * lacks permission (`permitted={false}`) the buttons disable with a tooltip,
 * and a denial / error message surfaces below the row.
 *
 * The component is presentation + state only: it never builds blobs or touches
 * the network — the caller's `onDownload` does that and may throw to surface an
 * error string here.
 */
export type ExportFormat = 'csv' | 'pdf' | 'xlsx';

const FORMAT_META: Record<ExportFormat, { label: string; icon: React.ReactNode }> = {
  csv: { label: 'CSV', icon: <Download className="size-4" /> },
  pdf: { label: 'PDF', icon: <FileText className="size-4" /> },
  xlsx: { label: 'Excel', icon: <FileSpreadsheet className="size-4" /> },
};

export interface ExportAction {
  format: ExportFormat;
  /** Override the default label ("CSV" / "PDF" / "Excel"). */
  label?: string;
  /** Async download handler. Throw to surface an error message in the group. */
  onDownload: () => void | Promise<void>;
  /** Disable just this action (e.g. required filters not chosen yet). */
  disabled?: boolean;
}

export interface ExportButtonGroupProps {
  actions: ExportAction[];
  /**
   * Permission gate. `false` disables every button and shows a denial note;
   * `null` (still resolving) disables without a note; `true`/undefined allows.
   */
  permitted?: boolean | null;
  /** Message shown when permission is denied. */
  deniedMessage?: string;
  /** Button size; defaults to "sm" to sit inline on cards/toolbars. */
  size?: 'sm' | 'md';
  className?: string;
}

export function ExportButtonGroup({
  actions,
  permitted,
  deniedMessage = "You don't have permission to export this report.",
  size = 'sm',
  className,
}: ExportButtonGroupProps) {
  const [error, setError] = React.useState<string | null>(null);
  const denied = permitted === false;
  const resolving = permitted === null;

  return (
    <div className={cn('flex flex-col items-start gap-1', className)}>
      <div className="flex flex-wrap items-center gap-2">
        {actions.map((a) => (
          <ExportButton
            key={`${a.format}-${a.label ?? ''}`}
            action={a}
            size={size}
            gated={denied || resolving}
            denied={denied}
            onError={setError}
          />
        ))}
      </div>
      {denied ? (
        <span className="text-xs text-muted-foreground" role="note">
          {deniedMessage}
        </span>
      ) : error ? (
        <span className="text-xs text-danger" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}

function ExportButton({
  action,
  size,
  gated,
  denied,
  onError,
}: {
  action: ExportAction;
  size: 'sm' | 'md';
  gated: boolean;
  denied: boolean;
  onError: (message: string | null) => void;
}) {
  const [busy, setBusy] = React.useState(false);
  const meta = FORMAT_META[action.format];

  async function run() {
    onError(null);
    setBusy(true);
    try {
      await action.onDownload();
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Could not generate the export.');
    } finally {
      setBusy(false);
    }
  }

  return (
    <Button
      type="button"
      size={size}
      variant="secondary"
      disabled={busy || gated || action.disabled}
      title={denied ? "You don't have permission" : undefined}
      onClick={run}
    >
      {meta.icon}
      {busy ? 'Preparing…' : (action.label ?? meta.label)}
    </Button>
  );
}
