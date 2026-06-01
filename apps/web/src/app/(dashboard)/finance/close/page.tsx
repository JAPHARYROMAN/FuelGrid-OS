'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CalendarRange, ListChecks } from 'lucide-react';

import { SdkError } from '@fuelgrid/sdk';
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
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
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Finance"
        title="Period close"
        description="Resolve blockers, then close and lock the accounting period."
      />

      {checklist.isPending ? (
        <div className="flex flex-col gap-7">
          <Skeleton className="h-64 rounded-xl" />
          <Skeleton className="h-48 rounded-xl" />
        </div>
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
              <div className="flex items-center gap-3">
                <span className="flex size-9 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">
                  <ListChecks className="size-4" />
                </span>
                <CardTitle>Close checklist</CardTitle>
              </div>
              <Badge tone={checklist.data.can_close ? 'success' : 'warning'}>
                {checklist.data.can_close
                  ? 'Ready to close'
                  : `${checklist.data.blockers} blocker(s)`}
              </Badge>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Check</TableHead>
                    <TableHead className="text-right">Count</TableHead>
                    <TableHead className="text-right">Status</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {Object.entries(checklist.data.checks).map(([key, count]) => (
                    <TableRow key={key}>
                      <TableCell className="text-muted-foreground">
                        {CHECK_LABELS[key] ?? key}
                      </TableCell>
                      <TableCell className="text-right font-mono font-medium tabular-nums">
                        {count}
                      </TableCell>
                      <TableCell className="text-right">
                        {count > 0 && BLOCKING.has(key) ? (
                          <Badge tone="warning">blocks close</Badge>
                        ) : (
                          <span className="text-xs text-muted-foreground">—</span>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Accounting periods</CardTitle>
              <p className="text-sm text-muted-foreground">
                Transition periods through close and lock.
              </p>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              {checklist.data.periods.length === 0 ? (
                <EmptyState
                  title="No accounting periods yet"
                  description="Periods will appear here once the ledger has activity."
                  icon={<CalendarRange className="size-8" />}
                />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Period</TableHead>
                      <TableHead className="text-right">Status</TableHead>
                      <TableHead className="text-right">Action</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {checklist.data.periods.map((p) => (
                      <TableRow key={p.id}>
                        <TableCell className="font-mono tabular-nums">
                          {p.start_date} → {p.end_date}
                        </TableCell>
                        <TableCell className="text-right">
                          <Badge tone={p.status === 'locked' ? 'neutral' : 'warning'}>
                            {p.status}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-right">
                          {p.status === 'open' ? (
                            <PermissionGate permission="period.close">
                              <Button
                                size="sm"
                                variant="outline"
                                disabled={transition.isPending && transition.variables?.id === p.id}
                                onClick={() =>
                                  transition.mutate({ id: p.id, action: 'start-close' })
                                }
                              >
                                Start close
                              </Button>
                            </PermissionGate>
                          ) : null}
                          {p.status === 'closing' ? (
                            <PermissionGate permission="period.close">
                              <Button
                                size="sm"
                                variant="outline"
                                disabled={transition.isPending && transition.variables?.id === p.id}
                                onClick={() => transition.mutate({ id: p.id, action: 'close' })}
                              >
                                Close
                              </Button>
                            </PermissionGate>
                          ) : null}
                          {p.status === 'closed' ? (
                            <PermissionGate permission="period.lock">
                              <Button
                                size="sm"
                                disabled={
                                  (transition.isPending && transition.variables?.id === p.id) ||
                                  !checklist.data.can_close
                                }
                                onClick={() => transition.mutate({ id: p.id, action: 'lock' })}
                              >
                                Lock
                              </Button>
                            </PermissionGate>
                          ) : null}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
              {transition.isError ? (
                <p
                  className="rounded-md border border-danger/40 bg-danger/5 px-3 py-2 text-sm text-danger"
                  role="alert"
                >
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
