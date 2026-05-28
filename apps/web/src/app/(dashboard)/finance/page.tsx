'use client';

import { useQuery } from '@tanstack/react-query';

import { SdkError, type JournalEntry } from '@fuelgrid/sdk';
import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  LoadingState,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

function money(n?: string) {
  if (n == null) return '—';
  const v = Number(n);
  return Number.isFinite(v) ? v.toLocaleString(undefined, { minimumFractionDigits: 2 }) : n;
}

export default function FinancePage() {
  const overview = useQuery({
    queryKey: ['finance-overview'],
    queryFn: ({ signal }) => api.getFinanceOverview(signal),
  });

  return (
    <div className="flex flex-col gap-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold tracking-tight">Finance</h1>
          <p className="text-sm text-muted-foreground">
            Balance sheet, profit &amp; loss, payables, and recent journal activity.
          </p>
        </div>
        <a
          href="/finance/close"
          className="rounded-md border border-border px-3 py-1.5 text-sm font-medium hover:bg-muted"
        >
          Period close →
        </a>
      </header>

      {overview.isPending ? (
        <LoadingState />
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
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Balance sheet</CardTitle>
            </CardHeader>
            <CardContent className="grid grid-cols-3 gap-3 text-sm">
              <Metric label="Assets" value={money(overview.data.balance_sheet.assets)} />
              <Metric label="Liabilities" value={money(overview.data.balance_sheet.liabilities)} />
              <Metric label="Equity" value={money(overview.data.balance_sheet.equity)} />
            </CardContent>
          </Card>

          {/* Profit & loss */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Profit &amp; loss (last 30 days)</CardTitle>
            </CardHeader>
            <CardContent className="grid grid-cols-3 gap-3 text-sm">
              <Metric label="Revenue" value={money(overview.data.income_statement.revenue)} />
              <Metric label="Expenses" value={money(overview.data.income_statement.expenses)} />
              <Metric label="Net profit" value={money(overview.data.income_statement.net_profit)} />
            </CardContent>
          </Card>

          {/* Control counts */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Control</CardTitle>
            </CardHeader>
            <CardContent className="grid grid-cols-2 gap-3 text-sm">
              <Metric
                label="Suppliers with payables"
                value={String(overview.data.ap_supplier_count)}
              />
              <Metric label="Open periods" value={String(overview.data.open_periods)} />
            </CardContent>
          </Card>

          {/* Recent journal entries */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Recent journal entries</CardTitle>
            </CardHeader>
            <CardContent className="text-sm">
              {overview.data.recent_entries.length === 0 ? (
                <EmptyState
                  title="No journal entries yet"
                  description="Posted entries will appear here as activity is recognized."
                />
              ) : (
                <div className="flex flex-col gap-1.5">
                  {overview.data.recent_entries.map((e: JournalEntry) => (
                    <div key={e.id} className="flex items-center justify-between gap-2">
                      <span className="flex items-center gap-2">
                        <span className="text-muted-foreground tabular-nums">
                          #{e.entry_number}
                        </span>
                        <span>{e.entry_date}</span>
                        <span className="text-muted-foreground">{e.source_type}</span>
                      </span>
                      <span className="flex items-center gap-3 tabular-nums">
                        <span className="font-medium">{money(e.total)}</span>
                        <Badge tone={e.status === 'reversed' ? 'warning' : 'neutral'}>
                          {e.status}
                        </Badge>
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5 rounded-md bg-muted/40 px-3 py-2">
      <span className="text-xs uppercase tracking-wider text-muted-foreground">{label}</span>
      <span className="font-semibold tabular-nums">{value}</span>
    </div>
  );
}
