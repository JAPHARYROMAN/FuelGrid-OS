'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { ChevronDown, ChevronRight, Receipt, Users } from 'lucide-react';

import { SdkError, type SupplierAging } from '@fuelgrid/sdk';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  PageHeader,
  Skeleton,
  Stat,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@fuelgrid/ui';

import { DocumentActions } from '@/components/document-actions';
import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';

// The five day-aged buckets, in display order. Each maps a SupplierAging
// decimal-string field to a column header. Math is server-side; this only
// labels columns.
const BUCKETS: ReadonlyArray<{ key: keyof SupplierAging; label: string }> = [
  { key: 'current', label: 'Current' },
  { key: 'd1_30', label: '1–30' },
  { key: 'd31_60', label: '31–60' },
  { key: 'd61_90', label: '61–90' },
  { key: 'd90_plus', label: '90+' },
];

export default function PayablesAgingPage() {
  const aging = useQuery({
    queryKey: ['ap-aging'],
    queryFn: ({ signal }) => api.getApAging(signal),
  });
  // Enrich supplier_id with the supplier's name/code for the drilldown table.
  // The aging endpoint returns ids only; suppliers is a small reference list.
  const suppliers = useQuery({
    queryKey: ['suppliers'],
    queryFn: ({ signal }) => api.listSuppliers(signal),
  });

  const supplierByID = React.useMemo(() => {
    const m = new Map<string, { name: string; code: string }>();
    for (const s of suppliers.data?.items ?? []) m.set(s.id, { name: s.name, code: s.code });
    return m;
  }, [suppliers.data]);

  const [expanded, setExpanded] = React.useState<Set<string>>(() => new Set());
  const toggle = React.useCallback((id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const rows = aging.data?.items ?? [];
  // Totals come straight from the server (exact SQL sums), never re-summed here.
  const totals = aging.data?.totals;
  const totalOutstanding = totals?.outstanding ?? '0';
  const openTotal = totals?.open_count ?? 0;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Finance · Payables"
        title="Payables Aging"
        description="Outstanding supplier balances split into day-aged buckets by invoice due date."
        actions={
          <DocumentActions
            onFetch={() => api.supplierBalancesPdf()}
            filename="supplier-balances.pdf"
            permission="payable.read"
            viewLabel="View"
            downloadLabel="Download PDF"
          />
        }
      />

      {aging.isPending ? (
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          {Array.from({ length: 2 }).map((_, i) => (
            <Skeleton key={i} className="h-[120px] rounded-xl" />
          ))}
        </section>
      ) : aging.isError ? (
        (() => {
          const forbidden = aging.error instanceof SdkError && aging.error.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access' : "Couldn't load payables"}
              description={
                forbidden
                  ? "You don't have permission to view payables (payable.read)."
                  : String((aging.error as Error).message)
              }
              onRetry={forbidden ? undefined : () => aging.refetch()}
            />
          );
        })()
      ) : (
        <>
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Stat label="Suppliers with balance" value={String(rows.length)} icon={<Users />} />
            <Stat
              label="Total payable"
              value={formatMoney(totalOutstanding)}
              hint={`${openTotal} open invoice${openTotal === 1 ? '' : 's'}`}
              icon={<Receipt />}
            />
          </section>

          <Card>
            <CardHeader>
              <CardTitle>Aging by supplier</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {rows.length === 0 ? (
                <div className="p-4">
                  <EmptyState
                    title="No outstanding payables"
                    description="No supplier is owed money right now."
                    icon={<Receipt />}
                  />
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Supplier</TableHead>
                      <TableHead className="text-right">Open</TableHead>
                      {BUCKETS.map((b) => (
                        <TableHead key={b.key} className="text-right">
                          {b.label}
                        </TableHead>
                      ))}
                      <TableHead className="text-right">Total</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {rows.map((r) => {
                      const sup = supplierByID.get(r.supplier_id);
                      const isOpen = expanded.has(r.supplier_id);
                      return (
                        <React.Fragment key={r.supplier_id}>
                          <TableRow>
                            <TableCell>
                              <button
                                type="button"
                                onClick={() => toggle(r.supplier_id)}
                                aria-expanded={isOpen}
                                aria-label={`Toggle bucket detail for ${sup?.name ?? r.supplier_id}`}
                                className="flex items-center gap-1.5 text-left hover:underline"
                              >
                                {isOpen ? (
                                  <ChevronDown className="size-4 shrink-0 text-muted-foreground" />
                                ) : (
                                  <ChevronRight className="size-4 shrink-0 text-muted-foreground" />
                                )}
                                <span>{sup?.name ?? '—'}</span>
                              </button>
                            </TableCell>
                            <TableCell className="text-right tabular-nums">
                              {r.open_count}
                            </TableCell>
                            {BUCKETS.map((b) => (
                              <TableCell
                                key={b.key}
                                className="text-right font-mono tabular-nums text-muted-foreground"
                              >
                                {formatMoney(r[b.key] as string)}
                              </TableCell>
                            ))}
                            <TableCell className="text-right font-mono font-medium tabular-nums">
                              {formatMoney(r.outstanding)}
                            </TableCell>
                          </TableRow>
                          {isOpen ? (
                            <TableRow className="bg-muted/30">
                              <TableCell colSpan={BUCKETS.length + 3} className="py-3">
                                <dl className="flex flex-wrap gap-x-8 gap-y-2 px-7 text-xs">
                                  <div>
                                    <dt className="text-muted-foreground">Code</dt>
                                    <dd className="font-mono">{sup?.code ?? r.supplier_id}</dd>
                                  </div>
                                  {BUCKETS.map((b) => (
                                    <div key={b.key}>
                                      <dt className="text-muted-foreground">{b.label}</dt>
                                      <dd className="font-mono tabular-nums">
                                        {formatMoney(r[b.key] as string)}
                                      </dd>
                                    </div>
                                  ))}
                                  <div>
                                    <dt className="text-muted-foreground">Open invoices</dt>
                                    <dd className="tabular-nums">{r.open_count}</dd>
                                  </div>
                                </dl>
                              </TableCell>
                            </TableRow>
                          ) : null}
                        </React.Fragment>
                      );
                    })}
                    {totals ? (
                      <TableRow className="border-t-2 bg-muted/40 font-medium">
                        <TableCell className="font-medium">Totals</TableCell>
                        <TableCell className="text-right tabular-nums">
                          {totals.open_count}
                        </TableCell>
                        {BUCKETS.map((b) => (
                          <TableCell
                            key={b.key}
                            className="text-right font-mono font-medium tabular-nums"
                          >
                            {formatMoney(totals[b.key] as string)}
                          </TableCell>
                        ))}
                        <TableCell className="text-right font-mono font-semibold tabular-nums">
                          {formatMoney(totals.outstanding)}
                        </TableCell>
                      </TableRow>
                    ) : null}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
