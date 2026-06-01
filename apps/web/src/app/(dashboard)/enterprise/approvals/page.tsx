'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertOctagon, CheckCircle2 } from 'lucide-react';

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

import { PermissionGate } from '@/components/permission-gate';
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
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Enterprise"
        title="Approvals & exceptions"
        description="Clear the enterprise approval queue and see unresolved exceptions across the network."
      />

      {/* Exception queue */}
      {exceptions.isPending ? (
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-[120px] rounded-xl" />
          ))}
        </section>
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
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {Object.entries(exceptions.data.checks).map(([k, v]) => (
            <Stat key={k} label={EXCEPTION_LABELS[k] ?? k} value={v} icon={<AlertOctagon />} />
          ))}
        </section>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Pending approvals</CardTitle>
          <p className="text-sm text-muted-foreground">Requests awaiting a decision.</p>
        </CardHeader>
        <CardContent>
          {requests.isPending ? (
            <div className="flex flex-col gap-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-14 rounded-lg" />
              ))}
            </div>
          ) : (requests.data?.items?.length ?? 0) === 0 ? (
            <EmptyState
              title="Queue clear"
              description="No approvals are waiting."
              icon={<CheckCircle2 className="size-8" />}
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Workflow</TableHead>
                  <TableHead className="text-right">Amount</TableHead>
                  <TableHead className="text-right">Approvals</TableHead>
                  <TableHead className="text-right">Decision</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {requests.data!.items.map((a: ApprovalRequest) => (
                  <TableRow key={a.id}>
                    <TableCell>
                      <Badge tone="neutral">{a.workflow_type}</Badge>
                    </TableCell>
                    <TableCell className="text-right font-mono font-medium tabular-nums">
                      {formatMoney(a.amount)}
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums text-muted-foreground">
                      {a.approvals_count}/{a.required_approvals}
                    </TableCell>
                    <TableCell className="text-right">
                      <span className="flex items-center justify-end gap-2">
                        <PermissionGate permission="approval_request.decide">
                          <Button
                            size="sm"
                            disabled={decide.isPending && decide.variables?.id === a.id}
                            onClick={() => decide.mutate({ id: a.id, decision: 'approve' })}
                          >
                            Approve
                          </Button>
                        </PermissionGate>
                        <PermissionGate permission="approval_request.decide">
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={decide.isPending && decide.variables?.id === a.id}
                            onClick={() => decide.mutate({ id: a.id, decision: 'reject' })}
                          >
                            Reject
                          </Button>
                        </PermissionGate>
                      </span>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
