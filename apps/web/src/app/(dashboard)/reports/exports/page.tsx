'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { BookOpen, Download, FileSpreadsheet, FileText, History, ShieldCheck } from 'lucide-react';

import { type GeneralLedgerFormat, type ReportPeriod, type ReportSpec } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  LoadingState,
  PageHeader,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { usePermission } from '@/hooks/use-permissions';

import { PeriodSelect } from '../_components/filters';

/**
 * Export center — the accountant-ready exports that sit outside the structured
 * envelope API (statements + general ledger), plus the export-job history
 * (Feature 10.7). Each download streams the existing report endpoint and is
 * audited; every recorded export job appears in the history table.
 */

const GL_FORMATS: { value: GeneralLedgerFormat; label: string }[] = [
  { value: 'csv', label: 'Generic CSV' },
  { value: 'iif', label: 'QuickBooks (IIF)' },
  { value: 'xero', label: 'Xero CSV' },
];

const selectClasses =
  'h-9 rounded-md border border-border bg-background px-2.5 text-sm text-foreground ' +
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50';

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

function ExportButton({
  label,
  icon,
  permission,
  build,
}: {
  label: string;
  icon: React.ReactNode;
  permission: string;
  build: () => { spec: ReportSpec; filename: string };
}) {
  const allowed = usePermission(permission);
  const [busy, setBusy] = React.useState(false);
  const denied = allowed === false;

  async function download() {
    setBusy(true);
    try {
      const built = build();
      const blob = await api.fetchReportBlob(built.spec);
      triggerDownload(blob, built.filename);
    } catch {
      // Export errors are surfaced on the dedicated report views; the center
      // stays quiet rather than throwing a wall of red.
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
      {icon}
      {busy ? 'Preparing…' : label}
    </Button>
  );
}

/**
 * Download a completed async export job's stored bytes via the SDK (the
 * permission is re-checked server-side at delivery) and save it to disk.
 */
function DownloadJobButton({
  job,
}: {
  job: { id: string; file_name: string | null; format: string };
}) {
  const [busy, setBusy] = React.useState(false);
  async function run() {
    setBusy(true);
    try {
      const blob = await api.downloadExportJob(job.id);
      triggerDownload(blob, job.file_name ?? `${job.id}.${job.format}`);
    } catch {
      // The history row already shows the job status; a failed download stays quiet.
    } finally {
      setBusy(false);
    }
  }
  return (
    <Button size="sm" variant="ghost" disabled={busy} onClick={run}>
      <Download className="size-4" />
      {busy ? 'Downloading…' : (job.file_name ?? 'Download')}
    </Button>
  );
}

/**
 * Export history — the async export queue + receipts (Feature 10.7 / Export
 * Center), newest first. Gated by reports.export; the query only fires for
 * permitted users so a denied actor sees an explanatory empty state rather than
 * a 403. While any job is still queued/running the list polls so a completed
 * job's download action appears without a manual refresh.
 */
function ExportHistory() {
  const allowed = usePermission('reports.export');
  const history = useQuery({
    queryKey: ['export-jobs'],
    queryFn: ({ signal }) => api.listExportJobs({ limit: 20 }, signal),
    enabled: allowed === true,
    // Poll while any job is in flight so completed downloads surface promptly,
    // then fall idle once everything is terminal.
    refetchInterval: (query) => {
      const items = query.state.data?.items ?? [];
      const inFlight = items.some((j) => j.status === 'queued' || j.status === 'running');
      return inFlight ? 2000 : false;
    },
  });

  if (allowed === false) {
    return (
      <EmptyState
        title="No access"
        description="You don't have permission to view the export history."
      />
    );
  }
  if (allowed === null || history.isPending) {
    return <LoadingState title="Loading export history…" />;
  }
  if (history.isError) {
    return (
      <ErrorState
        title="Couldn't load the export history"
        description={String((history.error as Error).message)}
        onRetry={() => history.refetch()}
      />
    );
  }

  const jobs = history.data?.items ?? [];
  if (jobs.length === 0) {
    return (
      <EmptyState
        title="No exports yet"
        description="Exports you generate from the report views will appear here. Every export is also recorded in the system audit log."
      />
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border text-left text-xs uppercase tracking-wide text-muted-foreground">
            <th className="py-2 pr-4 font-medium">Report</th>
            <th className="py-2 pr-4 font-medium">Format</th>
            <th className="py-2 pr-4 font-medium">Status</th>
            <th className="py-2 pr-4 font-medium">Requested</th>
            <th className="py-2 pr-4 font-medium">File</th>
          </tr>
        </thead>
        <tbody>
          {jobs.map((j) => (
            <tr key={j.id} className="border-b border-border/60 last:border-0">
              <td className="py-2 pr-4 font-medium">{j.report_key}</td>
              <td className="py-2 pr-4 uppercase text-muted-foreground">{j.format}</td>
              <td className="py-2 pr-4">
                <Badge
                  tone={
                    j.status === 'completed'
                      ? 'success'
                      : j.status === 'failed'
                        ? 'danger'
                        : 'neutral'
                  }
                >
                  {j.status}
                </Badge>
              </td>
              <td className="py-2 pr-4 text-muted-foreground">
                {new Date(j.created_at).toLocaleString()}
              </td>
              <td className="py-2 pr-4">
                {j.status === 'failed' ? (
                  <span className="text-destructive" title={j.error ?? undefined}>
                    {j.error ?? 'Failed'}
                  </span>
                ) : j.download_url ? (
                  // Async completed job — stream the stored bytes (permission
                  // re-checked at delivery) via the SDK.
                  <DownloadJobButton job={j} />
                ) : j.file_url ? (
                  // Legacy synchronous receipt — link the same-origin file URL.
                  <a
                    className="text-accent underline-offset-2 hover:underline"
                    href={j.file_url}
                    target="_blank"
                    rel="noreferrer"
                  >
                    {j.file_name ?? 'Download'}
                  </a>
                ) : (
                  <span className="text-muted-foreground">
                    {j.status === 'completed' ? '—' : 'Preparing…'}
                  </span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export default function ExportsPage() {
  const [period, setPeriod] = React.useState<ReportPeriod>('this-month');
  const [glFormat, setGlFormat] = React.useState<GeneralLedgerFormat>('csv');

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Exports"
        title="Export center"
        description="Accountant-ready financial statements and a general-ledger export for QuickBooks, Xero or generic CSV. Every export is recorded in the audit log."
      />

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <Card className="flex flex-col">
          <CardHeader>
            <CardTitle>Financial statements</CardTitle>
            <p className="text-sm text-muted-foreground">
              P&amp;L and balance sheet for the selected period.
            </p>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <PeriodSelect value={period} onChange={setPeriod} />
            <div className="flex flex-wrap items-center gap-2">
              <ExportButton
                label="P&L CSV"
                icon={<Download className="size-4" />}
                permission="finance.read"
                build={() => ({
                  spec: { kind: 'financials', period },
                  filename: `financials-${period}.csv`,
                })}
              />
              <ExportButton
                label="Excel"
                icon={<FileSpreadsheet className="size-4" />}
                permission="finance.read"
                build={() => ({
                  spec: { kind: 'financials-xlsx', period },
                  filename: `financials-${period}.xlsx`,
                })}
              />
              <ExportButton
                label="PDF"
                icon={<FileText className="size-4" />}
                permission="finance.read"
                build={() => ({
                  spec: { kind: 'financials-pdf', period },
                  filename: `financials-${period}.pdf`,
                })}
              />
            </div>
          </CardContent>
        </Card>

        <Card className="flex flex-col">
          <CardHeader>
            <CardTitle>General ledger</CardTitle>
            <p className="text-sm text-muted-foreground">
              Accountant-ready journal export in your accounting package&apos;s format.
            </p>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="flex flex-wrap items-center gap-2">
              <PeriodSelect value={period} onChange={setPeriod} />
              <select
                className={selectClasses}
                value={glFormat}
                onChange={(e) => setGlFormat(e.target.value as GeneralLedgerFormat)}
                aria-label="General ledger format"
              >
                {GL_FORMATS.map((f) => (
                  <option key={f.value} value={f.value}>
                    {f.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <ExportButton
                label="GL export"
                icon={<BookOpen className="size-4" />}
                permission="finance.read"
                build={() => {
                  const ext = glFormat === 'iif' ? 'iif' : 'csv';
                  return {
                    spec: { kind: 'gl-export', period, format: glFormat },
                    filename: `general-ledger-${period}-${glFormat}.${ext}`,
                  };
                }}
              />
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader className="flex-row items-center gap-2 space-y-0">
          <History className="size-4 text-muted-foreground" />
          <CardTitle>Export history</CardTitle>
        </CardHeader>
        <CardContent>
          <ExportHistory />
        </CardContent>
      </Card>

      <p className="text-xs text-muted-foreground">
        <Badge tone="neutral">
          <ShieldCheck className="mr-1 inline size-3" />
          Audited
        </Badge>{' '}
        Every export is recorded in the audit log.
      </p>
    </div>
  );
}
