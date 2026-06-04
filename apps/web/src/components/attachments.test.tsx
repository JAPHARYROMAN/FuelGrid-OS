import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError, type Attachment } from '@fuelgrid/sdk';

const listAttachments = vi.fn();
const uploadAttachment = vi.fn();
const deleteAttachment = vi.fn();
const attachmentUrl = vi.fn((id: string) => `/api/v1/attachments/${id}`);

vi.mock('@/lib/api', () => ({
  api: {
    listAttachments: (...args: unknown[]) => listAttachments(...args),
    uploadAttachment: (...args: unknown[]) => uploadAttachment(...args),
    deleteAttachment: (...args: unknown[]) => deleteAttachment(...args),
    attachmentUrl: (id: string) => attachmentUrl(id),
  },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

const toastError = vi.fn();
const toastSuccess = vi.fn();
vi.mock('@/lib/toast', () => ({
  toast: {
    error: (...a: unknown[]) => toastError(...a),
    success: (...a: unknown[]) => toastSuccess(...a),
  },
}));

import { AttachmentList } from './attachments';

function renderList() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AttachmentList entityType="expense" entityId="exp-1" permission="attachment.manage" />
    </QueryClientProvider>,
  );
}

const pdf: Attachment = {
  id: 'att-1',
  entity_type: 'expense',
  entity_id: 'exp-1',
  filename: 'receipt.pdf',
  content_type: 'application/pdf',
  size_bytes: 2048,
  checksum: 'abc',
  created_at: '2026-02-01T00:00:00Z',
  download_url: '/api/v1/attachments/att-1',
};

describe('AttachmentList', () => {
  beforeEach(() => {
    permitted = true;
    listAttachments.mockReset();
    uploadAttachment.mockReset();
    deleteAttachment.mockReset();
    toastError.mockReset();
  });

  afterEach(() => vi.clearAllMocks());

  it('lists attachments with a download link', async () => {
    listAttachments.mockResolvedValue({ items: [pdf], count: 1 });
    renderList();

    const link = await screen.findByRole('link', { name: 'receipt.pdf' });
    expect(link).toHaveAttribute('href', '/api/v1/attachments/att-1');
    expect(screen.getByText('2.0 KB')).toBeInTheDocument();
  });

  it('shows the empty state when there are no attachments', async () => {
    listAttachments.mockResolvedValue({ items: [], count: 0 });
    renderList();

    expect(await screen.findByText('No attachments')).toBeInTheDocument();
  });

  it('rejects an oversized file client-side without calling the API', async () => {
    listAttachments.mockResolvedValue({ items: [], count: 0 });
    renderList();
    await screen.findByText('No attachments');

    const input = screen.getByLabelText('Upload attachment') as HTMLInputElement;
    const big = new File(['x'], 'big.pdf', { type: 'application/pdf' });
    Object.defineProperty(big, 'size', { value: 6 * 1024 * 1024 });
    fireEvent.change(input, { target: { files: [big] } });

    expect(toastError).toHaveBeenCalledWith('File too large', expect.any(String));
    expect(uploadAttachment).not.toHaveBeenCalled();
  });

  it('rejects an unsupported type client-side without calling the API', async () => {
    listAttachments.mockResolvedValue({ items: [], count: 0 });
    renderList();
    await screen.findByText('No attachments');

    const input = screen.getByLabelText('Upload attachment') as HTMLInputElement;
    const txt = new File(['x'], 'notes.txt', { type: 'text/plain' });
    fireEvent.change(input, { target: { files: [txt] } });

    expect(toastError).toHaveBeenCalledWith('Unsupported file', expect.any(String));
    expect(uploadAttachment).not.toHaveBeenCalled();
  });

  it('uploads an accepted file', async () => {
    listAttachments.mockResolvedValue({ items: [], count: 0 });
    uploadAttachment.mockResolvedValue(pdf);
    renderList();
    await screen.findByText('No attachments');

    const input = screen.getByLabelText('Upload attachment') as HTMLInputElement;
    const ok = new File(['x'], 'receipt.png', { type: 'image/png' });
    fireEvent.change(input, { target: { files: [ok] } });

    await waitFor(() =>
      expect(uploadAttachment).toHaveBeenCalledWith(
        expect.objectContaining({ entityType: 'expense', entityID: 'exp-1' }),
      ),
    );
  });

  it('confirms before removing an attachment', async () => {
    listAttachments.mockResolvedValue({ items: [pdf], count: 1 });
    deleteAttachment.mockResolvedValue(undefined);
    renderList();

    fireEvent.click(await screen.findByRole('button', { name: 'Remove receipt.pdf' }));
    // Confirm dialog appears; the actual delete only fires on confirm.
    expect(deleteAttachment).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole('button', { name: /^remove$/i }));

    await waitFor(() => expect(deleteAttachment).toHaveBeenCalledWith('att-1'));
  });

  it('hides write controls and explains when the user lacks read permission', () => {
    permitted = false;
    listAttachments.mockResolvedValue({ items: [], count: 0 });
    renderList();

    expect(screen.getByText(/permission to view attachments/i)).toBeInTheDocument();
    expect(listAttachments).not.toHaveBeenCalled();
  });

  it('surfaces a load error with retry', async () => {
    listAttachments.mockRejectedValue(new SdkError('boom', 500, { error: 'boom' }));
    renderList();

    expect(await screen.findByText("Couldn't load attachments")).toBeInTheDocument();
  });
});
