'use client';

import * as React from 'react';
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
  PageHeader,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { usePermission } from '@/hooks/use-permissions';

import { PeriodSelect } from '../_components/filters';

/**
 * Export center — the accountant-ready exports that sit outside the structured
 * envelope API (statements + general ledger), plus a placeholder for export
 * history. Each download streams the existing report endpoint and is audited.
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
          <EmptyState
            title="History coming soon"
            description="A searchable log of every generated export will appear here. For now, all exports are recorded in the system audit log."
          />
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
