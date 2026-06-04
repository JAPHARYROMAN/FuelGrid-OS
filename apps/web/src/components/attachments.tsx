'use client';

import * as React from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Paperclip, Trash2, Upload } from 'lucide-react';

import { SdkError, type Attachment } from '@fuelgrid/sdk';
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  EmptyState,
  ErrorState,
  Skeleton,
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
import { usePermission } from '@/hooks/use-permissions';
import { api } from '@/lib/api';
import { toast } from '@/lib/toast';

/** Client-side allowlist mirroring the server (PDF/PNG/JPEG). */
const ACCEPTED_TYPES = ['application/pdf', 'image/png', 'image/jpeg'] as const;
const ACCEPT_ATTR = '.pdf,.png,.jpg,.jpeg,application/pdf,image/png,image/jpeg';
/** 5 MiB, mirroring the server cap. */
const MAX_BYTES = 5 * 1024 * 1024;

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

export interface AttachmentListProps {
  /** Entity kind the files hang off (e.g. "expense"). */
  entityType: string;
  /** The parent record's id. */
  entityId: string;
  /**
   * Permission gating the upload/remove controls. Defaults to
   * "attachment.manage" (the backend's write permission). Reads always require
   * attachment.read on the backend regardless.
   */
  permission?: string;
}

/**
 * AttachmentList renders an entity's files with download, an upload control
 * (with a client-side type/size pre-check), and remove-with-confirm. It is the
 * single reusable surface for the generic Attachments framework (C.3): drop it
 * into any detail view by passing entityType + entityId.
 *
 * Write controls are wrapped in a PermissionGate; the backend stays
 * authoritative. All states (loading / error / empty / populated) are handled,
 * and mutation errors are surfaced via toasts.
 */
export function AttachmentList({
  entityType,
  entityId,
  permission = 'attachment.manage',
}: AttachmentListProps) {
  const qc = useQueryClient();
  const fileInputRef = React.useRef<HTMLInputElement>(null);
  const [pendingDelete, setPendingDelete] = React.useState<Attachment | null>(null);

  const canRead = usePermission('attachment.read');

  const list = useQuery({
    queryKey: ['attachments', entityType, entityId],
    queryFn: ({ signal }) => api.listAttachments(entityType, entityId, signal),
    // Don't fire a request we know will 403; treat a still-loading perm as enabled.
    enabled: canRead !== false,
  });

  function invalidate() {
    void qc.invalidateQueries({ queryKey: ['attachments', entityType, entityId] });
  }

  const upload = useMutation({
    mutationFn: (file: File) => api.uploadAttachment({ entityType, entityID: entityId, file }),
    onSuccess: () => {
      invalidate();
      toast.success('Attachment uploaded');
    },
    onError: (err) =>
      toast.error('Could not upload', err instanceof SdkError ? err.message : undefined),
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteAttachment(id),
    onSuccess: () => {
      invalidate();
      setPendingDelete(null);
      toast.success('Attachment removed');
    },
    onError: (err) =>
      toast.error('Could not remove', err instanceof SdkError ? err.message : undefined),
  });

  function onPickFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    // Reset immediately so picking the same file again re-fires onChange.
    e.target.value = '';
    if (!file) return;
    if (!ACCEPTED_TYPES.includes(file.type as (typeof ACCEPTED_TYPES)[number])) {
      toast.error('Unsupported file', 'Only PDF, PNG, or JPEG files are allowed.');
      return;
    }
    if (file.size > MAX_BYTES) {
      toast.error('File too large', 'Attachments must be 5 MB or smaller.');
      return;
    }
    upload.mutate(file);
  }

  const items = list.data?.items ?? [];

  if (canRead === false) {
    return (
      <p className="text-xs text-muted-foreground">
        You don&apos;t have permission to view attachments.
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2">
        <h3 className="flex items-center gap-1.5 text-sm font-medium">
          <Paperclip className="size-4" aria-hidden /> Attachments
        </h3>
        <PermissionGate permission={permission}>
          <Button
            type="button"
            size="sm"
            variant="secondary"
            disabled={upload.isPending}
            onClick={() => fileInputRef.current?.click()}
          >
            <Upload className="size-4" aria-hidden />
            {upload.isPending ? 'Uploading…' : 'Upload'}
          </Button>
        </PermissionGate>
        <input
          ref={fileInputRef}
          type="file"
          accept={ACCEPT_ATTR}
          className="hidden"
          aria-label="Upload attachment"
          onChange={onPickFile}
        />
      </div>

      {list.isPending ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 2 }).map((_, i) => (
            <Skeleton key={i} className="h-10 rounded-md" />
          ))}
        </div>
      ) : list.isError ? (
        (() => {
          const forbidden = list.error instanceof SdkError && list.error.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access' : "Couldn't load attachments"}
              description={
                forbidden
                  ? "You don't have permission to view attachments."
                  : String((list.error as Error).message)
              }
              onRetry={forbidden ? undefined : () => list.refetch()}
            />
          );
        })()
      ) : items.length === 0 ? (
        <EmptyState
          title="No attachments"
          description="Uploaded files will appear here."
          icon={<Paperclip />}
        />
      ) : (
        <ul className="flex flex-col divide-y rounded-md border">
          {items.map((a) => (
            <li key={a.id} className="flex items-center justify-between gap-3 px-3 py-2">
              <div className="min-w-0">
                <a
                  href={api.attachmentUrl(a.id)}
                  target="_blank"
                  rel="noreferrer"
                  className="block truncate text-sm font-medium text-primary hover:underline"
                >
                  {a.filename}
                </a>
                <p className="text-xs text-muted-foreground">{formatBytes(a.size_bytes)}</p>
              </div>
              <PermissionGate permission={permission} mode="hide">
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  aria-label={`Remove ${a.filename}`}
                  disabled={remove.isPending}
                  onClick={() => setPendingDelete(a)}
                >
                  <Trash2 className="size-4" aria-hidden />
                </Button>
              </PermissionGate>
            </li>
          ))}
        </ul>
      )}

      <Dialog
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remove attachment</DialogTitle>
            <DialogDescription>
              Remove <span className="font-medium">{pendingDelete?.filename}</span>? This cannot be
              undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => setPendingDelete(null)}>
              Cancel
            </Button>
            <Button
              type="button"
              variant="danger"
              disabled={remove.isPending}
              onClick={() => {
                if (pendingDelete) remove.mutate(pendingDelete.id);
              }}
            >
              {remove.isPending ? 'Removing…' : 'Remove'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
