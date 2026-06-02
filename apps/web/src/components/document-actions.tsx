'use client';

import * as React from 'react';
import { Download, Eye } from 'lucide-react';

import { SdkError } from '@fuelgrid/sdk';
import { Button } from '@fuelgrid/ui';

import { usePermission } from '@/hooks/use-permissions';
import { toast } from '@/lib/toast';

/**
 * Trigger a browser download for an already-fetched blob. The single source for
 * the blob-download gesture across the app (the reports view-downloads use the
 * same shape); kept here so document/report controls share one implementation.
 */
export function triggerDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

/** Open an already-fetched blob in a new browser tab for in-app preview. */
function openBlobInTab(blob: Blob) {
  const url = URL.createObjectURL(blob);
  // Don't immediately revoke: the new tab needs the URL to load. Browsers
  // reclaim object URLs when the document is unloaded, so this is safe.
  const win = window.open(url, '_blank', 'noopener,noreferrer');
  if (!win) {
    // Popup blocked — fall back to navigating the current tab's hidden anchor
    // so the user still sees the document rather than a silent no-op.
    URL.revokeObjectURL(url);
    throw new Error('popup-blocked');
  }
}

export interface DocumentActionsProps {
  /**
   * Fetches the document Blob (e.g. `() => sdk.customersPdf()`). Called fresh on
   * each View/Download so the document always reflects current data.
   */
  onFetch: () => Promise<Blob>;
  /** Suggested filename for the Download action (e.g. "customers.pdf"). */
  filename: string;
  /** Backend permission the document requires (e.g. "customer.read"). */
  permission: string;
  /** Supply when the permission is station-scoped so the check is meaningful. */
  stationId?: string | null;
  /** Optional override for the View button label (default "View"). */
  viewLabel?: string;
  /** Optional override for the Download button label (default "Download"). */
  downloadLabel?: string;
  /** Button size, forwarded to @fuelgrid/ui Button (default "sm"). */
  size?: React.ComponentProps<typeof Button>['size'];
}

/**
 * DocumentActions is the reusable View / Download control for a downloadable
 * document (PDF). It is permission-gated (disabled + a tooltip when the user
 * lacks the permission, matching PermissionGate) and surfaces fetch failures as
 * a toast. The pattern fans out to every list-document entity: a page passes a
 * thin `onFetch` (a blob-returning SDK call) plus the filename and permission.
 *
 *   <DocumentActions
 *     onFetch={() => api.customersPdf()}
 *     filename="customers.pdf"
 *     permission="customer.read"
 *   />
 *
 * View opens the document in a new browser tab (object URL); Download saves it
 * via the shared triggerDownload gesture. Styling rides @fuelgrid/ui so it sits
 * happily next to a "New …" button and works in light/dark/navy.
 */
export function DocumentActions({
  onFetch,
  filename,
  permission,
  stationId,
  viewLabel = 'View',
  downloadLabel = 'Download',
  size = 'sm',
}: DocumentActionsProps) {
  const allowed = usePermission(permission, { stationID: stationId });
  const [busy, setBusy] = React.useState<'view' | 'download' | null>(null);

  const denied = allowed === false;
  // Disable while loading the permission answer (avoid a racing click) or denied.
  const disabled = busy !== null || denied || allowed === null;
  const title = denied ? "You don't have permission" : undefined;

  async function run(action: 'view' | 'download') {
    setBusy(action);
    try {
      const blob = await onFetch();
      if (action === 'download') {
        triggerDownload(blob, filename);
      } else {
        openBlobInTab(blob);
      }
    } catch (err) {
      if (err instanceof Error && err.message === 'popup-blocked') {
        toast.error('Allow pop-ups to preview', 'Your browser blocked the document tab.');
      } else {
        toast.error(
          'Could not generate the document',
          err instanceof SdkError ? err.message : undefined,
        );
      }
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="flex items-center gap-2">
      <Button
        type="button"
        variant="secondary"
        size={size}
        disabled={disabled}
        aria-disabled={disabled}
        title={title}
        onClick={() => run('view')}
      >
        <Eye className="size-4" />
        {busy === 'view' ? 'Opening…' : viewLabel}
      </Button>
      <Button
        type="button"
        variant="secondary"
        size={size}
        disabled={disabled}
        aria-disabled={disabled}
        title={title}
        onClick={() => run('download')}
      >
        <Download className="size-4" />
        {busy === 'download' ? 'Preparing…' : downloadLabel}
      </Button>
    </div>
  );
}
