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

import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';

import { ReportDownloads } from '../_components/downloads';
import { ReportInsightsPanel } from '../_components/insights';

// Sum a list of decimal-string balances for the header total. Display only.
function sumBalances(balances: string[]): string {
  const total = balances.reduce((acc, b) => acc + (Number(b) || 0), 0);
  return total.toFixed(2);
}

export default function CustomerAgingPage() {
  const aging = useQuery({
    queryKey: ['ar-aging'],
    queryFn: ({ signal }) => api.getARaging(signal),
  });
  const insights = useQuery({
    queryKey: ['report-insights', 'customer-aging'],
    queryFn: ({ signal }) => api.getReportInsights('customer-aging', undefined, signal),
  });

  const rows = aging.data?.items ?? [];
  const total = sumBalances(rows.map((r) => r.balance));
  const topRows = [...rows]
    .sort((a, b) => (Number(b.balance) || 0) - (Number(a.balance) || 0))
    .slice(0, 8)
    .map((r) => ({ name: r.name, balance: r.balance }));

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Customers"
        title="Customer Aging"
        description="Every credit customer with an outstanding receivable balance."
      />

      <ReportDownloads
        permission="customer.read"
        downloads={[
          {
            label: 'CSV',
            format: 'csv',
            build: () => ({ spec: { kind: 'ar-aging' }, filename: 'customer-aging.csv' }),
          },
        ]}
      />

      <ReportInsightsPanel
        data={insights.data}
        isPending={insights.isPending}
        isError={insights.isError}
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
              title={forbidden ? 'No access' : "Couldn't load receivables"}
              description={
                forbidden
                  ? "You don't have permission to view receivables."
                  : String((aging.error as Error).message)
              }
              onRetry={forbidden ? undefined : () => aging.refetch()}
            />
          );
        })()
      ) : (
        <>
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Stat label="Customers with balance" value={String(rows.length)} icon={<Users />} />
            <Stat label="Total receivable" value={formatMoney(total)} icon={<Receipt />} />
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
                  series={[{ key: 'balance', label: 'Balance' }]}
                  valueFormatter={(v) => formatMoney(v as string)}
                  layout="vertical"
                  height={Math.max(200, topRows.length * 36)}
                />
              </CardContent>
            </Card>
          ) : null}

          <Card>
            <CardHeader>
              <CardTitle>Outstanding balances</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {rows.length === 0 ? (
                <div className="p-4">
                  <EmptyState
                    title="No outstanding balances"
                    description="No customer owes money."
                  />
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Customer</TableHead>
                      <TableHead>Code</TableHead>
                      <TableHead className="text-right">Balance</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {rows.map((c) => (
                      <TableRow key={c.customer_id}>
                        <TableCell>{c.name}</TableCell>
                        <TableCell className="text-muted-foreground">{c.code}</TableCell>
                        <TableCell className="text-right font-mono font-medium tabular-nums">
                          {formatMoney(c.balance)}
                        </TableCell>
                      </TableRow>
                    ))}
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
