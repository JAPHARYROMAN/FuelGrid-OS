'use client';

import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { ArrowRight, FileText, Landmark, Scale, TrendingUp, Users } from 'lucide-react';

import { SdkError, type JournalEntry } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CategoricalBarChart,
  chartColors,
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
  formatMoney,
} from '@fuelgrid/ui';

import { DocumentActions } from '@/components/document-actions';
import { api } from '@/lib/api';

export default function FinancePage() {
  const overview = useQuery({
    queryKey: ['finance-overview'],
    queryFn: ({ signal }) => api.getFinanceOverview(signal),
  });

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Finance"
        title="Finance"
        description="Balance sheet, profit & loss, payables, and recent journal activity."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <DocumentActions
              onFetch={() => api.journalEntriesPdf()}
              filename="journal-entries.pdf"
              permission="journal.read"
              viewLabel="View journal"
              downloadLabel="Journal PDF"
            />
            <DocumentActions
              onFetch={() => api.expensesPdf()}
              filename="expenses.pdf"
              permission="finance.read"
              viewLabel="View expenses"
              downloadLabel="Expenses PDF"
            />
            <DocumentActions
              onFetch={() => api.supplierBalancesPdf()}
              filename="supplier-balances.pdf"
              permission="payable.read"
              viewLabel="View AP"
              downloadLabel="AP PDF"
            />
            <Button asChild variant="secondary">
              <Link href="/finance/close">
                Period close
                <ArrowRight className="size-4" />
              </Link>
            </Button>
          </div>
        }
      />

      {overview.isPending ? (
        <div className="flex flex-col gap-7">
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-[120px] rounded-xl" />
            ))}
          </section>
          <Skeleton className="h-64 rounded-xl" />
        </div>
      ) : overview.isError ? (
        (() => {
          const err = overview.error;
          const forbidden = err instanceof SdkError && err.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access to finance' : "Couldn't load finance"}
              description={
                forbidden
                  ? "You don't have the finance.read permission."
                  : String((err as Error).message)
              }
              onRetry={forbidden ? undefined : () => overview.refetch()}
            />
          );
        })()
      ) : (
        <>
          {/* Balance sheet */}
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
            <Stat
              label="Assets"
              value={formatMoney(overview.data.balance_sheet.assets)}
              hint="Balance sheet"
              icon={<Scale />}
            />
            <Stat
              label="Liabilities"
              value={formatMoney(overview.data.balance_sheet.liabilities)}
              hint="Balance sheet"
              icon={<Landmark />}
            />
            <Stat
              label="Equity"
              value={formatMoney(overview.data.balance_sheet.equity)}
              hint="Balance sheet"
              icon={<Scale />}
            />
          </section>

          {/* Profit & loss */}
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <Stat
              label="Revenue"
              value={formatMoney(overview.data.income_statement.revenue)}
              hint="Last 30 days"
              icon={<TrendingUp />}
            />
            <Stat
              label="Expenses"
              value={formatMoney(overview.data.income_statement.expenses)}
              hint="Last 30 days"
              icon={<TrendingUp />}
            />
            <Stat
              label="Net profit"
              value={formatMoney(overview.data.income_statement.net_profit)}
              hint="Last 30 days"
              icon={<TrendingUp />}
            />
          </section>

          {/* P&L composition */}
          <Card>
            <CardHeader>
              <CardTitle>Profit &amp; loss</CardTitle>
              <p className="text-sm text-muted-foreground">
                Revenue, expenses, and net over the last 30 days.
              </p>
            </CardHeader>
            <CardContent>
              <CategoricalBarChart
                data={[
                  { label: 'Revenue', amount: overview.data.income_statement.revenue },
                  { label: 'Expenses', amount: overview.data.income_statement.expenses },
                  { label: 'Net profit', amount: overview.data.income_statement.net_profit },
                ]}
                xKey="label"
                valueKey="amount"
                label="Amount"
                valueFormatter={(v) => formatMoney(v as string)}
                colorFor={(row) => {
                  if (row.label === 'Expenses') return chartColors.muted;
                  if (row.label === 'Net profit') {
                    return Number(row.amount) < 0 ? chartColors.danger : chartColors.success;
                  }
                  return chartColors.accent;
                }}
                height={220}
              />
            </CardContent>
          </Card>

          {/* Control counts */}
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Stat
              label="Suppliers with payables"
              value={overview.data.ap_supplier_count}
              hint="Accounts payable"
              icon={<Users />}
            />
            <Stat
              label="Open periods"
              value={overview.data.open_periods}
              hint="Accounting"
              icon={<FileText />}
            />
          </section>

          {/* Recent journal entries */}
          <Card>
            <CardHeader>
              <CardTitle>Recent journal entries</CardTitle>
              <p className="text-sm text-muted-foreground">
                Posted entries as activity is recognized.
              </p>
            </CardHeader>
            <CardContent>
              {overview.data.recent_entries.length === 0 ? (
                <EmptyState
                  title="No journal entries yet"
                  description="Posted entries will appear here as activity is recognized."
                  icon={<FileText className="size-8" />}
                />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Entry</TableHead>
                      <TableHead>Date</TableHead>
                      <TableHead>Source</TableHead>
                      <TableHead className="text-right">Total</TableHead>
                      <TableHead className="text-right">Status</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {overview.data.recent_entries.map((e: JournalEntry) => (
                      <TableRow key={e.id}>
                        <TableCell className="font-mono tabular-nums text-muted-foreground">
                          #{e.entry_number}
                        </TableCell>
                        <TableCell>{e.entry_date}</TableCell>
                        <TableCell className="text-muted-foreground">{e.source_type}</TableCell>
                        <TableCell className="text-right font-mono font-medium tabular-nums">
                          {formatMoney(e.total)}
                        </TableCell>
                        <TableCell className="text-right">
                          <Badge tone={e.status === 'reversed' ? 'warning' : 'neutral'}>
                            {e.status}
                          </Badge>
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
