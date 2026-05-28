'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { SdkError, type ApprovalRequest } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  LoadingState,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

const EXCEPTION_LABELS: Record<string, string> = {
  open_incidents: 'Open incidents',
  unresolved_shift_exceptions: 'Unresolved shift exceptions',
  unmatched_bank_lines: 'Unmatched bank lines',
  unposted_cash_reconciliations: 'Unposted cash reconciliations',
  approvals_waiting: 'Approvals waiting',
  open_credit_alerts: 'Open credit alerts',
};

export default function EnterpriseApprovalsPage() {
  const qc = useQueryClient();
  const requests = useQuery({
    queryKey: ['approval-requests', 'requested'],
    queryFn: ({ signal }) => api.listApprovalRequests({ status: 'requested' }, signal),
  });
  const exceptions = useQuery({
    queryKey: ['enterprise-exceptions'],
    queryFn: ({ signal }) => api.getEnterpriseExceptions(signal),
  });
  const decide = useMutation({
    mutationFn: ({ id, decision }: { id: string; decision: 'approve' | 'reject' }) =>
      api.decideApprovalRequest(id, { decision }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['approval-requests', 'requested'] }),
  });

  return (
    <div className="flex flex-col gap-5">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Approvals &amp; exceptions</h1>
        <p className="text-sm text-muted-foreground">
          Clear the enterprise approval queue and see unresolved exceptions across the network.
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Exception queue</CardTitle>
        </CardHeader>
        <CardContent className="text-sm">
          {exceptions.isPending ? (
            <LoadingState />
          ) : exceptions.isError ? (
            <ErrorState
              title="Couldn't load exceptions"
              description={String((exceptions.error as Error).message)}
              onRetry={
                exceptions.error instanceof SdkError && exceptions.error.status === 403
                  ? undefined
                  : () => exceptions.refetch()
              }
            />
          ) : (
            <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
              {Object.entries(exceptions.data.checks).map(([k, v]) => (
                <div key={k} className="flex flex-col gap-0.5 rounded-md bg-muted/40 px-3 py-2">
                  <span className="text-xs uppercase tracking-wider text-muted-foreground">
                    {EXCEPTION_LABELS[k] ?? k}
                  </span>
                  <span className="font-semibold tabular-nums">{v}</span>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Pending approvals</CardTitle>
        </CardHeader>
        <CardContent className="text-sm">
          {requests.isPending ? (
            <LoadingState />
          ) : (requests.data?.items?.length ?? 0) === 0 ? (
            <EmptyState title="Queue clear" description="No approvals are waiting." />
          ) : (
            <div className="flex flex-col gap-2">
              {requests.data!.items.map((a: ApprovalRequest) => (
                <div key={a.id} className="flex items-center justify-between gap-2">
                  <span className="flex items-center gap-2">
                    <Badge tone="neutral">{a.workflow_type}</Badge>
                    <span className="tabular-nums">{a.amount}</span>
                    <span className="text-muted-foreground">
                      {a.approvals_count}/{a.required_approvals}
                    </span>
                  </span>
                  <span className="flex items-center gap-2">
                    <Button
                      size="sm"
                      disabled={decide.isPending}
                      onClick={() => decide.mutate({ id: a.id, decision: 'approve' })}
                    >
                      Approve
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={decide.isPending}
                      onClick={() => decide.mutate({ id: a.id, decision: 'reject' })}
                    >
                      Reject
                    </Button>
                  </span>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
