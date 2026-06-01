'use client';

import * as React from 'react';
import { Download, FileSpreadsheet, FileText } from 'lucide-react';

import { SdkError, type ReportSpec } from '@fuelgrid/sdk';
import { Button } from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { usePermission } from '@/hooks/use-permissions';

/** Trigger a browser download for an already-fetched blob. */
function triggerDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

/** One download: spec + filename, or null when required filters are missing. */
export type DownloadBuild = () => { spec: ReportSpec; filename: string } | null;

export interface DownloadDescriptor {
  label: string;
  format: 'csv' | 'pdf' | 'xlsx';
  build: DownloadBuild;
}

const ICONS: Record<DownloadDescriptor['format'], React.ReactNode> = {
  csv: <Download className="size-4" />,
  pdf: <FileText className="size-4" />,
  xlsx: <FileSpreadsheet className="size-4" />,
};

/**
 * A permission-gated row of download buttons (CSV / PDF / XLSX) for a report
 * view. The permission decision is computed once and shared across the buttons.
 */
export function ReportDownloads({
  permission,
  stationId,
  downloads,
}: {
  permission: string;
  stationId?: string | null;
  downloads: DownloadDescriptor[];
}) {
  const allowed = usePermission(permission, { stationID: stationId });
  const [error, setError] = React.useState<string | null>(null);

  return (
    <div className="flex flex-col items-end gap-1">
      <div className="flex flex-wrap items-center justify-end gap-2">
        {downloads.map((d) => (
          <DownloadButton
            key={`${d.format}-${d.label}`}
            descriptor={d}
            allowed={allowed}
            onError={setError}
          />
        ))}
      </div>
      {error ? (
        <span className="text-xs text-danger" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}

function DownloadButton({
  descriptor,
  allowed,
  onError,
}: {
  descriptor: DownloadDescriptor;
  allowed: boolean | null;
  onError: (message: string | null) => void;
}) {
  const [busy, setBusy] = React.useState(false);
  const denied = allowed === false;

  async function download() {
    const built = descriptor.build();
    if (!built) {
      onError('Pick the required filters first.');
      return;
    }
    onError(null);
    setBusy(true);
    try {
      const blob = await api.fetchReportBlob(built.spec);
      triggerDownload(blob, built.filename);
    } catch (err) {
      onError(err instanceof SdkError ? err.message : 'Could not generate the export.');
    } finally {
      setBusy(false);
    }
  }

  return (
    <Button
      size="sm"
      variant="secondary"
      disabled={busy || denied || allowed === null}
      title={denied ? "You don't have permission" : undefined}
      onClick={download}
    >
      {ICONS[descriptor.format]}
      {busy ? 'Preparing…' : descriptor.label}
    </Button>
  );
}
