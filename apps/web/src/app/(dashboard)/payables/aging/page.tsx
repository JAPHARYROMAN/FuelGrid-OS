'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { Receipt, Users } from 'lucide-react';

import { SdkError } from '@fuelgrid/sdk';
import {
  BarChart,
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

// Sum a list of decimal-string amounts for the header total. Display only —
// exact money math stays server-side; this is a readout.
function sumAmounts(amounts: string[]): string {
  const total = amounts.reduce((acc, b) => acc + (Number(b) || 0), 0);
  return total.toFixed(2);
}

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

  const rows = aging.data?.items ?? [];
  const total = sumAmounts(rows.map((r) => r.outstanding));
  const openTotal = rows.reduce((acc, r) => acc + (r.open_count ?? 0), 0);
  const topRows = [...rows]
    .sort((a, b) => (Number(b.outstanding) || 0) - (Number(a.outstanding) || 0))
    .slice(0, 8)
    .map((r) => ({
      name: supplierByID.get(r.supplier_id)?.name ?? r.supplier_id.slice(0, 8),
      outstanding: r.outstanding,
    }));

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Finance · Payables"
        title="Payables Aging"
        description="Every supplier with an outstanding payable balance, ranked by what's owed."
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
              value={formatMoney(total)}
              hint={`${openTotal} open invoice${openTotal === 1 ? '' : 's'}`}
              icon={<Receipt />}
            />
          </section>

          {topRows.length > 0 ? (
            <Card>
              <CardHeader>
                <CardTitle>Top balances</CardTitle>
              </CardHeader>
              <CardContent>
                <BarChart
                  data={topRows}
                  xKey="name"
                  series={[{ key: 'outstanding', label: 'Outstanding' }]}
                  valueFormatter={(v) => formatMoney(v as string)}
                  layout="vertical"
                  height={Math.max(200, topRows.length * 36)}
                />
              </CardContent>
            </Card>
          ) : null}

          <Card>
            <CardHeader>
              <CardTitle>Outstanding by supplier</CardTitle>
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
                      <TableHead>Code</TableHead>
                      <TableHead className="text-right">Open invoices</TableHead>
                      <TableHead className="text-right">Outstanding</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {rows.map((r) => {
                      const sup = supplierByID.get(r.supplier_id);
                      return (
                        <TableRow key={r.supplier_id}>
                          <TableCell>{sup?.name ?? '—'}</TableCell>
                          <TableCell className="font-mono text-[11px] text-muted-foreground">
                            {sup?.code ?? r.supplier_id}
                          </TableCell>
                          <TableCell className="text-right tabular-nums">{r.open_count}</TableCell>
                          <TableCell className="text-right font-mono font-medium tabular-nums">
                            {formatMoney(r.outstanding)}
                          </TableCell>
                        </TableRow>
                      );
                    })}
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
