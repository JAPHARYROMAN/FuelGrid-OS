'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { SdkError } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  ErrorState,
  LoadingState,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

const CHECK_LABELS: Record<string, string> = {
  unposted_cash_reconciliations: 'Unposted cash reconciliations',
  open_deposits: 'Deposits in flight',
  unmatched_bank_lines: 'Unmatched bank lines',
  expenses_awaiting_posting: 'Expenses awaiting posting',
  unissued_customer_invoices: 'Unissued customer invoices',
  open_payables: 'Open payables',
  open_customer_invoices: 'Open customer invoices',
};

const BLOCKING = new Set([
  'unposted_cash_reconciliations',
  'open_deposits',
  'unmatched_bank_lines',
  'expenses_awaiting_posting',
  'unissued_customer_invoices',
]);

type PeriodAction = 'start-close' | 'close' | 'reopen' | 'lock';

export default function FinanceClosePage() {
  const qc = useQueryClient();
  const checklist = useQuery({
    queryKey: ['close-checklist'],
    queryFn: ({ signal }) => api.getCloseChecklist(signal),
  });

  const transition = useMutation({
    mutationFn: ({ id, action }: { id: string; action: PeriodAction }) =>
      api.transitionAccountingPeriod(id, action),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['close-checklist'] }),
  });

  return (
    <div className="flex flex-col gap-5">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Period close</h1>
        <p className="text-sm text-muted-foreground">
          Resolve blockers, then close and lock the accounting period.
        </p>
      </header>

      {checklist.isPending ? (
        <LoadingState />
      ) : checklist.isError ? (
        (() => {
          const err = checklist.error;
          const forbidden = err instanceof SdkError && err.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access to close' : "Couldn't load the checklist"}
              description={
                forbidden
                  ? "You don't have the finance.read permission."
                  : String((err as Error).message)
              }
              onRetry={forbidden ? undefined : () => checklist.refetch()}
            />
          );
        })()
      ) : (
        <>
          <Card>
            <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
              <CardTitle className="text-base">Close checklist</CardTitle>
              <Badge tone={checklist.data.can_close ? 'success' : 'warning'}>
                {checklist.data.can_close
                  ? 'Ready to close'
                  : `${checklist.data.blockers} blocker(s)`}
              </Badge>
            </CardHeader>
            <CardContent className="flex flex-col gap-1.5 text-sm">
              {Object.entries(checklist.data.checks).map(([key, count]) => (
                <div key={key} className="flex items-center justify-between gap-2">
                  <span className="text-muted-foreground">{CHECK_LABELS[key] ?? key}</span>
                  <span className="flex items-center gap-2 tabular-nums">
                    <span className="font-medium">{count}</span>
                    {count > 0 && BLOCKING.has(key) ? (
                      <Badge tone="warning">blocks close</Badge>
                    ) : null}
                  </span>
                </div>
              ))}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Accounting periods</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-2 text-sm">
              {checklist.data.periods.length === 0 ? (
                <p className="text-muted-foreground">No accounting periods yet.</p>
              ) : (
                checklist.data.periods.map((p) => (
                  <div key={p.id} className="flex items-center justify-between gap-2">
                    <span>
                      {p.start_date} → {p.end_date}
                    </span>
                    <span className="flex items-center gap-2">
                      <Badge tone={p.status === 'locked' ? 'neutral' : 'warning'}>{p.status}</Badge>
                      {p.status === 'open' ? (
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={transition.isPending}
                          onClick={() => transition.mutate({ id: p.id, action: 'start-close' })}
                        >
                          Start close
                        </Button>
                      ) : null}
                      {p.status === 'closing' ? (
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={transition.isPending}
                          onClick={() => transition.mutate({ id: p.id, action: 'close' })}
                        >
                          Close
                        </Button>
                      ) : null}
                      {p.status === 'closed' ? (
                        <Button
                          size="sm"
                          disabled={transition.isPending || !checklist.data.can_close}
                          onClick={() => transition.mutate({ id: p.id, action: 'lock' })}
                        >
                          Lock
                        </Button>
                      ) : null}
                    </span>
                  </div>
                ))
              )}
              {transition.isError ? (
                <p className="rounded-md bg-danger/10 px-3 py-2 text-danger" role="alert">
                  {transition.error instanceof SdkError
                    ? transition.error.message
                    : 'Could not transition the period'}
                </p>
              ) : null}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
