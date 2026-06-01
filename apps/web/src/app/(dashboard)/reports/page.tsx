'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { BarChart3, Database, Download, FileSpreadsheet, Receipt, Scale } from 'lucide-react';

import { SdkError, type ReportPeriod, type ReportSpec } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  PageHeader,
  Skeleton,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { usePermission } from '@/hooks/use-permissions';

const PERIODS: { value: ReportPeriod; label: string }[] = [
  { value: 'this-month', label: 'This month' },
  { value: 'last-month', label: 'Last month' },
  { value: 'ytd', label: 'Year to date' },
  { value: 'last-30', label: 'Last 30 days' },
];

const selectClasses =
  'h-9 rounded-md border border-border bg-background px-2.5 text-sm text-foreground ' +
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50';

// Parse the download filename out of a Content-Disposition-style attachment,
// falling back to a sensible default. Browsers don't expose the response
// headers on a fetched blob unless we read them, so the caller passes a default.
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

interface ReportRowProps {
  icon: React.ReactNode;
  title: string;
  description: string;
  permission: string;
  stationId?: string | null;
  /** Builds the report spec + download filename when the button is clicked. */
  build: () => { spec: ReportSpec; filename: string } | null;
  /** Extra filter controls rendered before the download button. */
  controls?: React.ReactNode;
}

function ReportRow({ icon, title, description, permission, stationId, build, controls }: ReportRowProps) {
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  async function download() {
    const built = build();
    if (!built) {
      setError('Pick the required filters first.');
      return;
    }
    setError(null);
    setBusy(true);
    try {
      const blob = await api.fetchReportBlob(built.spec);
      triggerDownload(blob, built.filename);
    } catch (err) {
      setError(err instanceof SdkError ? err.message : 'Could not generate the export.');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-3 border-b border-border px-4 py-4 last:border-b-0 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 items-start gap-3">
        <span className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">
          {icon}
        </span>
        <div className="flex min-w-0 flex-col">
          <span className="text-sm font-medium text-foreground">{title}</span>
          <span className="text-xs text-muted-foreground">{description}</span>
          {error ? (
            <span className="mt-1 text-xs text-danger" role="alert">
              {error}
            </span>
          ) : null}
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-2 self-end sm:self-auto">
        {controls}
        <PermissionButton
          permission={permission}
          stationId={stationId}
          busy={busy}
          onClick={download}
        />
      </div>
    </div>
  );
}

// A download button gated on the backend permission via usePermission. Inlined
// here (rather than wrapping with PermissionGate) so the busy + disabled states
// compose cleanly with the click handler.
function PermissionButton({
  permission,
  stationId,
  busy,
  onClick,
}: {
  permission: string;
  stationId?: string | null;
  busy: boolean;
  onClick: () => void;
}) {
  const allowed = usePermission(permission, { stationID: stationId });
  const denied = allowed === false;
  return (
    <Button
      size="sm"
      variant="secondary"
      disabled={busy || denied || allowed === null}
      title={denied ? "You don't have permission" : undefined}
      onClick={onClick}
    >
      <Download className="size-4" />
      {busy ? 'Preparing…' : 'CSV'}
    </Button>
  );
}

export default function ReportsPage() {
  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });

  const stationItems = stations.data?.items ?? [];
  const [stationId, setStationId] = React.useState<string>('');
  const [period, setPeriod] = React.useState<ReportPeriod>('this-month');

  // Default the station filter to the first station once loaded.
  React.useEffect(() => {
    const first = stationItems[0];
    if (!stationId && first) setStationId(first.id);
  }, [stationId, stationItems]);

  const stationCode = stationItems.find((s) => s.id === stationId)?.code ?? 'station';

  const stationSelect = (
    <select
      className={selectClasses}
      value={stationId}
      onChange={(e) => setStationId(e.target.value)}
      aria-label="Station"
      disabled={stationItems.length === 0}
    >
      {stationItems.length === 0 ? <option value="">No stations</option> : null}
      {stationItems.map((s) => (
        <option key={s.id} value={s.id}>
          {s.code} — {s.name}
        </option>
      ))}
    </select>
  );

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports & exports"
        title="Reports"
        description="Download standard operational and financial reports as CSV. Money and litres are exported as exact decimals — open them in any spreadsheet."
      />

      {stations.isError ? (
        <ErrorState
          title="Couldn't load stations"
          description={String((stations.error as Error).message)}
          onRetry={() => stations.refetch()}
        />
      ) : (
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
          {/* Station reports */}
          <Card>
            <CardHeader className="flex-row items-center justify-between space-y-0">
              <div className="flex flex-col gap-1">
                <CardTitle>Station reports</CardTitle>
                <p className="text-sm text-muted-foreground">
                  Scoped to the selected station.
                </p>
              </div>
              {stations.isPending ? <Skeleton className="h-9 w-40 rounded-md" /> : stationSelect}
            </CardHeader>
            <CardContent className="p-0">
              {stations.isPending ? (
                <div className="flex flex-col gap-2 p-4">
                  {Array.from({ length: 3 }).map((_, i) => (
                    <Skeleton key={i} className="h-16 rounded-lg" />
                  ))}
                </div>
              ) : stationItems.length === 0 ? (
                <div className="p-4">
                  <EmptyState
                    title="No stations yet"
                    description="Create a station to export its reports."
                  />
                </div>
              ) : (
                <div className="flex flex-col">
                  <ReportRow
                    icon={<BarChart3 className="size-4" />}
                    title="Revenue days"
                    description="Recent revenue days — gross/net, COGS, margin and the tender split."
                    permission="revenue.read"
                    stationId={stationId}
                    build={() =>
                      stationId
                        ? {
                            spec: { kind: 'revenue', stationID: stationId },
                            filename: `revenue-${stationCode}.csv`,
                          }
                        : null
                    }
                  />
                  <ReportRow
                    icon={<Database className="size-4" />}
                    title="Inventory snapshot"
                    description="Per-tank book balance, latest physical dip and last variance."
                    permission="inventory.read"
                    stationId={stationId}
                    build={() =>
                      stationId
                        ? {
                            spec: { kind: 'inventory', stationID: stationId },
                            filename: `inventory-${stationCode}.csv`,
                          }
                        : null
                    }
                  />
                  <ReportRow
                    icon={<Scale className="size-4" />}
                    title="Reconciliation"
                    description="The active day's per-tank book→physical variance breakdown."
                    permission="reconciliation.read"
                    stationId={stationId}
                    build={() =>
                      stationId
                        ? {
                            spec: { kind: 'reconciliation', stationID: stationId },
                            filename: `reconciliation-${stationCode}.csv`,
                          }
                        : null
                    }
                  />
                </div>
              )}
            </CardContent>
          </Card>

          {/* Financial reports */}
          <Card>
            <CardHeader>
              <CardTitle>Financial reports</CardTitle>
              <p className="text-sm text-muted-foreground">Tenant-wide, over posted journals.</p>
            </CardHeader>
            <CardContent className="p-0">
              <div className="flex flex-col">
                <ReportRow
                  icon={<FileSpreadsheet className="size-4" />}
                  title="Financial statements"
                  description="Profit & loss and balance sheet for the chosen period."
                  permission="finance.read"
                  build={() => ({
                    spec: { kind: 'financials', period },
                    filename: `financials-${period}.csv`,
                  })}
                  controls={
                    <select
                      className={selectClasses}
                      value={period}
                      onChange={(e) => setPeriod(e.target.value as ReportPeriod)}
                      aria-label="Reporting period"
                    >
                      {PERIODS.map((p) => (
                        <option key={p.value} value={p.value}>
                          {p.label}
                        </option>
                      ))}
                    </select>
                  }
                />
                <ReportRow
                  icon={<Receipt className="size-4" />}
                  title="AR aging"
                  description="Every credit customer with an outstanding receivable balance."
                  permission="customer.read"
                  build={() => ({ spec: { kind: 'ar-aging' }, filename: 'ar-aging.csv' })}
                />
              </div>
            </CardContent>
          </Card>
        </div>
      )}

      <p className="text-xs text-muted-foreground">
        <Badge tone="neutral">Audited</Badge> Every export is recorded in the audit log.
      </p>
    </div>
  );
}
