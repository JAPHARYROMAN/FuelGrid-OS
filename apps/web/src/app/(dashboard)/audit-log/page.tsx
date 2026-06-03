'use client';

import * as React from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Download, ScrollText } from 'lucide-react';

import { SdkError, type AuditLogEntry } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  EmptyState,
  ErrorState,
  Input,
  Label,
  PageHeader,
  Skeleton,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@fuelgrid/ui';

import { triggerDownload } from '@/components/document-actions';
import { PermissionGate } from '@/components/permission-gate';
import { usePermission } from '@/hooks/use-permissions';
import { api } from '@/lib/api';
import { toast } from '@/lib/toast';

const PAGE_SIZE = 50;

interface Filters {
  action: string;
  entity_type: string;
  entity_id: string;
  actor_id: string;
  since: string;
  until: string;
}

const blankFilters: Filters = {
  action: '',
  entity_type: '',
  entity_id: '',
  actor_id: '',
  since: '',
  until: '',
};

/** Pretty-print a JSON-ish audit value (before/after snapshot) for the detail view. */
function formatValue(v: unknown): string {
  if (v == null) return '—';
  if (typeof v === 'string') return v;
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}

export default function AuditLogPage() {
  const [filters, setFilters] = React.useState<Filters>(blankFilters);
  const [appliedFilters, setAppliedFilters] = React.useState<Filters>(blankFilters);
  const [offset, setOffset] = React.useState(0);
  const [selected, setSelected] = React.useState<AuditLogEntry | null>(null);

  const canView = usePermission('audit.read');

  const list = useQuery({
    queryKey: ['audit-log', appliedFilters, offset],
    queryFn: ({ signal }) =>
      api.listAuditLogs(
        {
          action: appliedFilters.action || undefined,
          entityType: appliedFilters.entity_type || undefined,
          entityID: appliedFilters.entity_id || undefined,
          actorID: appliedFilters.actor_id || undefined,
          since: appliedFilters.since ? new Date(appliedFilters.since).toISOString() : undefined,
          until: appliedFilters.until ? new Date(appliedFilters.until).toISOString() : undefined,
          limit: PAGE_SIZE,
          offset,
        },
        signal,
      ),
    enabled: canView !== false,
  });

  const exporter = useMutation({
    mutationFn: () =>
      api.exportAuditLogs({
        from: appliedFilters.since ? appliedFilters.since.slice(0, 10) : undefined,
        to: appliedFilters.until ? appliedFilters.until.slice(0, 10) : undefined,
      }),
    onSuccess: (result) => {
      const blob = new Blob([result.csv], { type: 'text/csv;charset=utf-8' });
      triggerDownload(blob, `audit-log-${result.from}_${result.to}.csv`);
      toast.success('Export ready', `${result.row_count} row(s) downloaded as CSV.`);
    },
    onError: (err) => {
      toast.error(
        'Could not export the audit trail',
        err instanceof SdkError ? err.message : undefined,
      );
    },
  });

  function applyAndReset() {
    setOffset(0);
    setAppliedFilters(filters);
  }

  const items = list.data?.items ?? [];
  const hasMore = list.data?.has_more ?? false;
  const forbidden =
    canView === false || (list.error instanceof SdkError && list.error.status === 403);

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Monitoring"
        title="Audit log"
        description="Append-only record of sensitive actions, scoped to your tenant. Click a row for the before/after snapshot."
        actions={
          <PermissionGate permission="audit.read">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              disabled={exporter.isPending}
              onClick={() => exporter.mutate()}
            >
              <Download className="size-4" />
              {exporter.isPending ? 'Exporting…' : 'Export CSV'}
            </Button>
          </PermissionGate>
        }
      />

      <form
        className="grid grid-cols-2 gap-3 rounded-xl border border-border bg-card/40 p-4 md:grid-cols-6"
        onSubmit={(e) => {
          e.preventDefault();
          applyAndReset();
        }}
      >
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="action">Action</Label>
          <Input
            id="action"
            placeholder="e.g. expense.approved"
            value={filters.action}
            onChange={(e) => setFilters({ ...filters, action: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="entity_type">Entity type</Label>
          <Input
            id="entity_type"
            placeholder="expense, station…"
            value={filters.entity_type}
            onChange={(e) => setFilters({ ...filters, entity_type: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="entity_id">Entity id</Label>
          <Input
            id="entity_id"
            placeholder="uuid"
            value={filters.entity_id}
            onChange={(e) => setFilters({ ...filters, entity_id: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="actor_id">Actor id</Label>
          <Input
            id="actor_id"
            placeholder="uuid"
            value={filters.actor_id}
            onChange={(e) => setFilters({ ...filters, actor_id: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="since">Since</Label>
          <Input
            id="since"
            type="datetime-local"
            value={filters.since}
            onChange={(e) => setFilters({ ...filters, since: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="until">Until</Label>
          <Input
            id="until"
            type="datetime-local"
            value={filters.until}
            onChange={(e) => setFilters({ ...filters, until: e.target.value })}
          />
        </div>
        <div className="col-span-2 flex gap-2 md:col-span-6">
          <Button type="submit">Apply</Button>
          <Button
            type="button"
            variant="ghost"
            onClick={() => {
              setFilters(blankFilters);
              setAppliedFilters(blankFilters);
              setOffset(0);
            }}
          >
            Reset
          </Button>
        </div>
      </form>

      {forbidden ? (
        <ErrorState
          title="No access"
          description="You don't have permission to view the audit log (audit.read)."
        />
      ) : list.isPending ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-14 rounded-lg" />
          ))}
        </div>
      ) : list.isError ? (
        <ErrorState
          title="Couldn't load audit logs"
          description={String((list.error as Error).message)}
          onRetry={() => list.refetch()}
        />
      ) : items.length === 0 ? (
        <EmptyState
          title="No matching audit entries"
          description="Sensitive actions show up here as they happen. Try widening the date range or clearing filters."
          icon={<ScrollText />}
        />
      ) : (
        <>
          <Card>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>When</TableHead>
                    <TableHead>Action</TableHead>
                    <TableHead>Entity</TableHead>
                    <TableHead>Entity id</TableHead>
                    <TableHead>Actor</TableHead>
                    <TableHead className="text-right">Detail</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((e) => (
                    <TableRow key={e.id}>
                      <TableCell className="whitespace-nowrap font-mono text-xs">
                        {new Date(e.occurred_at).toLocaleString()}
                      </TableCell>
                      <TableCell>
                        <Badge tone="accent">{e.action}</Badge>
                      </TableCell>
                      <TableCell className="text-muted-foreground">{e.entity_type}</TableCell>
                      <TableCell className="font-mono text-[11px] text-muted-foreground">
                        {e.entity_id ?? '—'}
                      </TableCell>
                      <TableCell className="font-mono text-[11px] text-muted-foreground">
                        {e.actor_id ?? '—'}
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          onClick={() => setSelected(e)}
                        >
                          View
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          <div className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">
              Showing {offset + 1}–{offset + items.length}
            </p>
            <div className="flex gap-2">
              <Button
                type="button"
                variant="secondary"
                size="sm"
                disabled={offset === 0 || list.isFetching}
                onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
              >
                Previous
              </Button>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                disabled={!hasMore || list.isFetching}
                onClick={() => setOffset(offset + PAGE_SIZE)}
              >
                Next
              </Button>
            </div>
          </div>
        </>
      )}

      <Dialog open={selected != null} onOpenChange={(open) => !open && setSelected(null)}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{selected?.action}</DialogTitle>
            <DialogDescription>
              {selected
                ? `${selected.entity_type} · ${new Date(selected.occurred_at).toLocaleString()}`
                : null}
            </DialogDescription>
          </DialogHeader>
          {selected ? (
            <div className="flex flex-col gap-4">
              <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm">
                <div>
                  <dt className="text-muted-foreground">Entity id</dt>
                  <dd className="font-mono text-xs">{selected.entity_id ?? '—'}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Actor id</dt>
                  <dd className="font-mono text-xs">{selected.actor_id ?? '—'}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">IP</dt>
                  <dd className="font-mono text-xs">{selected.ip ?? '—'}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Request id</dt>
                  <dd className="font-mono text-xs">{selected.request_id ?? '—'}</dd>
                </div>
                {selected.reason ? (
                  <div className="col-span-2">
                    <dt className="text-muted-foreground">Reason</dt>
                    <dd>{selected.reason}</dd>
                  </div>
                ) : null}
              </dl>
              <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                <div className="flex flex-col gap-1.5">
                  <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                    Before
                  </p>
                  <pre className="max-h-64 overflow-auto rounded-lg border border-border bg-muted/40 p-3 text-[11px] leading-relaxed">
                    {formatValue(selected.previous_value)}
                  </pre>
                </div>
                <div className="flex flex-col gap-1.5">
                  <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                    After
                  </p>
                  <pre className="max-h-64 overflow-auto rounded-lg border border-border bg-muted/40 p-3 text-[11px] leading-relaxed">
                    {formatValue(selected.new_value)}
                  </pre>
                </div>
              </div>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}
