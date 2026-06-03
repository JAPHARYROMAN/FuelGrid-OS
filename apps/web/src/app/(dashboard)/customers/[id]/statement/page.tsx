'use client';

import * as React from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { ChevronRight, FileText } from 'lucide-react';

import { SdkError, type ARentry } from '@fuelgrid/sdk';
import {
  Badge,
  type BadgeProps,
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

function entryTone(type: string): BadgeProps['tone'] {
  switch (type) {
    case 'payment':
      return 'success';
    case 'charge':
      return 'info';
    case 'adjustment':
      return 'warning';
    default:
      return 'neutral';
  }
}

/**
 * Summarise a customer's AR ledger into the classic statement shape. The
 * backend returns signed amounts (charge +, payment −) and a `balance_after`
 * snapshot per entry, so the opening balance is the first entry's running
 * balance minus its own amount, and the closing balance is the live `balance`.
 * Sums use Number on decimal strings only for the derived rollups shown beside
 * the exact per-entry strings; never store these back as money.
 */
function summarise(entries: ARentry[], closing: string) {
  if (entries.length === 0) {
    return { opening: '0', charges: '0', payments: '0', adjustments: '0', closing };
  }
  const first = entries[0]!;
  const opening = (Number(first.balance_after) - Number(first.amount)).toString();
  let charges = 0;
  let payments = 0;
  let adjustments = 0;
  for (const e of entries) {
    const amt = Number(e.amount);
    if (e.entry_type === 'charge') charges += amt;
    else if (e.entry_type === 'payment') payments += amt;
    else if (e.entry_type === 'adjustment') adjustments += amt;
  }
  return {
    opening,
    charges: charges.toString(),
    payments: payments.toString(),
    adjustments: adjustments.toString(),
    closing,
  };
}

export default function CustomerStatementPage() {
  const params = useParams<{ id: string }>();
  const id = params.id;

  const statement = useQuery({
    queryKey: ['customer-statement', id],
    queryFn: ({ signal }) => api.getCustomerStatement(id, signal),
    enabled: Boolean(id),
  });

  const forbidden = statement.error instanceof SdkError && statement.error.status === 403;
  const notFound = statement.error instanceof SdkError && statement.error.status === 404;

  const data = statement.data;
  const entries = data?.entries ?? [];
  const totals = data ? summarise(entries, data.balance) : null;

  return (
    <div className="flex flex-col gap-7">
      <nav className="flex items-center gap-1 text-sm text-muted-foreground">
        <Link href="/customers" className="hover:text-foreground">
          Customers
        </Link>
        <ChevronRight className="size-4" />
        <span className="text-foreground">Statement</span>
      </nav>

      <PageHeader
        eyebrow="Finance · Credit"
        title={data ? `${data.customer.name} — statement` : 'Customer statement'}
        description="Opening balance, charges, payments, adjustments, and the closing balance from the customer's AR ledger."
      />

      {statement.isPending ? (
        <div className="flex flex-col gap-4">
          <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-20 rounded-lg" />
            ))}
          </div>
          <Skeleton className="h-48 rounded-lg" />
        </div>
      ) : statement.isError ? (
        <ErrorState
          title={
            forbidden ? 'No access' : notFound ? 'Customer not found' : "Couldn't load statement"
          }
          description={
            forbidden
              ? "You don't have permission to view this statement (customer.read)."
              : notFound
                ? 'This customer no longer exists.'
                : String((statement.error as Error).message)
          }
          onRetry={forbidden || notFound ? undefined : () => statement.refetch()}
        />
      ) : data && totals ? (
        <>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
            <Stat label="Opening balance" value={formatMoney(totals.opening)} />
            <Stat label="Charges" value={formatMoney(totals.charges)} />
            <Stat label="Payments" value={formatMoney(totals.payments)} />
            <Stat label="Adjustments" value={formatMoney(totals.adjustments)} />
            <Stat label="Closing balance" value={formatMoney(totals.closing)} />
          </div>

          {entries.length === 0 ? (
            <EmptyState
              title="No ledger activity"
              description="This customer has no recorded charges or payments yet."
              icon={<FileText />}
            />
          ) : (
            <Card>
              <CardHeader>
                <CardTitle>Ledger entries</CardTitle>
              </CardHeader>
              <CardContent className="p-0">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Date</TableHead>
                      <TableHead>Type</TableHead>
                      <TableHead>Reference</TableHead>
                      <TableHead className="text-right">Amount</TableHead>
                      <TableHead className="text-right">Balance</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {entries.map((e) => (
                      <TableRow key={e.id}>
                        <TableCell className="whitespace-nowrap font-mono text-xs">
                          {e.recorded_at.slice(0, 10)}
                        </TableCell>
                        <TableCell>
                          <Badge tone={entryTone(e.entry_type)}>{e.entry_type}</Badge>
                        </TableCell>
                        <TableCell className="text-muted-foreground">
                          {e.notes ?? e.source_ref_type ?? '—'}
                        </TableCell>
                        <TableCell className="text-right font-mono font-medium tabular-nums">
                          {formatMoney(e.amount)}
                        </TableCell>
                        <TableCell className="text-right font-mono tabular-nums text-muted-foreground">
                          {formatMoney(e.balance_after)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          )}
        </>
      ) : null}
    </div>
  );
}
