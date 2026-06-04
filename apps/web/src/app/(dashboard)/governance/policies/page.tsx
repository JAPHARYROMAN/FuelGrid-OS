'use client';

import * as React from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ShieldCheck } from 'lucide-react';

import { SdkError, type ApprovalPolicy, type ApprovalSimulation } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  EmptyState,
  ErrorState,
  Input,
  Label,
  PageHeader,
  Skeleton,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  formatMoney,
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
import { usePermission } from '@/hooks/use-permissions';
import { api } from '@/lib/api';
import { toast } from '@/lib/toast';

// Manage actions (create policy, simulate) are gated on the backend's real
// permission code (services/api/migrations/0057 + server_routes.go), NOT the
// planning-doc-only "governance.policy.manage" which does not exist server-side.
const MANAGE_PERMISSION = 'approval_policy.manage';

// A money amount is an exact decimal string (never a float). Allow an empty
// value (treated as 0 by the engine) or a non-negative decimal.
function amountValid(v: string): boolean {
  const t = v.trim();
  return t === '' || /^\d+(\.\d{1,2})?$/.test(t);
}

export default function GovernancePoliciesPage() {
  const qc = useQueryClient();
  const [createOpen, setCreateOpen] = React.useState(false);
  const [editing, setEditing] = React.useState<ApprovalPolicy | null>(null);

  const canManage = usePermission(MANAGE_PERMISSION);

  const list = useQuery({
    queryKey: ['approval-policies'],
    queryFn: ({ signal }) => api.listApprovalPolicies(signal),
  });

  const policies = (list.data?.items ?? []) as ApprovalPolicy[];

  const invalidate = () => void qc.invalidateQueries({ queryKey: ['approval-policies'] });

  // Enable/disable toggles a policy's status. A disabled (archived) policy is
  // ignored by the approval engine, so it stops requiring approval.
  const setStatus = useMutation({
    mutationFn: (vars: { id: string; status: 'active' | 'archived' }) =>
      api.setApprovalPolicyStatus(vars.id, vars.status),
    onSuccess: (_res, vars) => {
      toast.success(vars.status === 'active' ? 'Policy enabled' : 'Policy disabled');
      invalidate();
    },
    onError: (err) =>
      toast.error('Could not update policy', err instanceof SdkError ? err.message : undefined),
  });

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Governance"
        title="Approval policies"
        description="Define which workflows require approval and how many sign-offs they need, then simulate a workflow + amount to see exactly what the approval engine would require."
        actions={
          <PermissionGate permission={MANAGE_PERMISSION} mode="hide">
            <Button type="button" size="sm" onClick={() => setCreateOpen(true)}>
              New policy
            </Button>
          </PermissionGate>
        }
      />

      <section className="grid grid-cols-1 gap-7 xl:grid-cols-[1.6fr_1fr]">
        <Card>
          <CardHeader>
            <CardTitle>Policies</CardTitle>
            <p className="text-sm text-muted-foreground">
              Active policies are evaluated when a request is raised; the strictest match wins.
            </p>
          </CardHeader>
          <CardContent className="p-0">
            {list.isPending ? (
              <div className="flex flex-col gap-2 p-4">
                {Array.from({ length: 4 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 rounded-lg" />
                ))}
              </div>
            ) : list.isError ? (
              (() => {
                const forbidden = list.error instanceof SdkError && list.error.status === 403;
                return (
                  <div className="p-4">
                    <ErrorState
                      title={forbidden ? 'No access' : "Couldn't load policies"}
                      description={
                        forbidden
                          ? "You don't have permission to view approval policies (enterprise.read)."
                          : String((list.error as Error).message)
                      }
                      onRetry={forbidden ? undefined : () => list.refetch()}
                    />
                  </div>
                );
              })()
            ) : policies.length === 0 ? (
              <div className="p-4">
                <EmptyState
                  title="No policies yet"
                  description="Without a policy, a workflow needs a single approval by default. Add a policy to require more sign-offs or a specific role."
                  icon={<ShieldCheck className="size-8" />}
                />
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Workflow</TableHead>
                    <TableHead className="text-right">Min amount</TableHead>
                    <TableHead className="text-right">Approvals</TableHead>
                    <TableHead>Required role</TableHead>
                    <TableHead>Status</TableHead>
                    <PermissionGate permission={MANAGE_PERMISSION} mode="hide">
                      <TableHead className="text-right">Actions</TableHead>
                    </PermissionGate>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {policies.map((p) => {
                    const disabled = p.status !== 'active';
                    return (
                      <TableRow key={p.id}>
                        <TableCell>
                          <Badge tone="neutral">{p.workflow_type}</Badge>
                        </TableCell>
                        <TableCell className="text-right font-mono font-medium tabular-nums">
                          {formatMoney(p.min_amount)}
                        </TableCell>
                        <TableCell className="text-right font-mono tabular-nums text-muted-foreground">
                          {p.required_approvals}
                        </TableCell>
                        <TableCell className="text-muted-foreground">
                          {p.required_role ?? '—'}
                        </TableCell>
                        <TableCell>
                          <Badge tone={p.status === 'active' ? 'success' : 'neutral'}>
                            {p.status}
                          </Badge>
                        </TableCell>
                        <PermissionGate permission={MANAGE_PERMISSION} mode="hide">
                          <TableCell className="text-right">
                            <div className="flex justify-end gap-2">
                              <Button
                                type="button"
                                size="sm"
                                variant="ghost"
                                onClick={() => setEditing(p)}
                                disabled={canManage === false}
                              >
                                Edit
                              </Button>
                              <Button
                                type="button"
                                size="sm"
                                variant={disabled ? 'primary' : 'ghost'}
                                onClick={() =>
                                  setStatus.mutate({
                                    id: p.id,
                                    status: disabled ? 'active' : 'archived',
                                  })
                                }
                                disabled={canManage === false || setStatus.isPending}
                              >
                                {disabled ? 'Enable' : 'Disable'}
                              </Button>
                            </div>
                          </TableCell>
                        </PermissionGate>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>

        <SimulatePanel />
      </section>

      <CreatePolicyDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={() => {
          setCreateOpen(false);
          invalidate();
        }}
      />

      <EditPolicyDialog
        policy={editing}
        onOpenChange={(open) => {
          if (!open) setEditing(null);
        }}
        onSaved={() => {
          setEditing(null);
          invalidate();
        }}
      />
    </div>
  );
}

function SimulatePanel() {
  const [workflowType, setWorkflowType] = React.useState('');
  const [amount, setAmount] = React.useState('');
  const [result, setResult] = React.useState<ApprovalSimulation | null>(null);

  const canManage = usePermission(MANAGE_PERMISSION);

  const simulate = useMutation({
    mutationFn: () =>
      api.simulateApprovalPolicy({
        workflow_type: workflowType.trim(),
        amount: amount.trim() || undefined,
      }),
    onSuccess: (res) => setResult(res),
    onError: (err) =>
      toast.error('Could not simulate', err instanceof SdkError ? err.message : undefined),
  });

  const ready = workflowType.trim().length > 0 && amountValid(amount);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Simulate</CardTitle>
        <p className="text-sm text-muted-foreground">
          Check whether a workflow at a given amount would require approval — nothing is saved.
        </p>
      </CardHeader>
      <CardContent>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (ready && canManage !== false) {
              setResult(null);
              simulate.mutate();
            }
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="sim-workflow">Workflow type</Label>
            <Input
              id="sim-workflow"
              placeholder="e.g. central_price"
              required
              value={workflowType}
              onChange={(e) => {
                setWorkflowType(e.target.value);
                setResult(null);
              }}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="sim-amount">Amount (optional)</Label>
            <Input
              id="sim-amount"
              inputMode="decimal"
              placeholder="e.g. 1500.00"
              value={amount}
              onChange={(e) => {
                setAmount(e.target.value);
                setResult(null);
              }}
              aria-invalid={!amountValid(amount)}
            />
            {!amountValid(amount) ? (
              <span className="text-xs text-danger">Enter a non-negative amount (decimal).</span>
            ) : (
              <span className="text-xs text-muted-foreground">
                Defaults to 0 when left blank. Decimal string, never rounded.
              </span>
            )}
          </div>
          <Button
            type="submit"
            disabled={!ready || canManage === false || simulate.isPending}
            title={
              canManage === false ? "You don't have permission to simulate policies" : undefined
            }
          >
            {simulate.isPending ? 'Simulating…' : 'Simulate'}
          </Button>
        </form>

        {result ? (
          <div className="mt-5 flex flex-col gap-3 rounded-lg border border-border bg-muted/30 p-4">
            <div className="flex items-center justify-between gap-2">
              <span className="text-sm font-medium">Outcome</span>
              <Badge tone={result.approval_required ? 'warning' : 'success'}>
                {result.approval_required ? 'Approval required' : 'No approval required'}
              </Badge>
            </div>
            <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
              <dt className="text-muted-foreground">Workflow</dt>
              <dd className="text-right font-mono">{result.workflow_type}</dd>
              <dt className="text-muted-foreground">Required approvals</dt>
              <dd className="text-right font-mono tabular-nums">{result.required_approvals}</dd>
              <dt className="text-muted-foreground">Required role</dt>
              <dd className="text-right">{result.required_role ?? '—'}</dd>
              <dt className="text-muted-foreground">Matched policy</dt>
              <dd className="text-right font-mono text-xs">{result.policy_id ?? 'none'}</dd>
            </dl>
            {!result.approval_required ? (
              <p className="text-xs text-muted-foreground">
                No active policy matches this workflow and amount, so a request would not be gated
                by policy.
              </p>
            ) : null}
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

function CreatePolicyDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: () => void;
}) {
  const [workflowType, setWorkflowType] = React.useState('');
  const [minAmount, setMinAmount] = React.useState('');
  const [requiredApprovals, setRequiredApprovals] = React.useState('1');
  const [requiredRole, setRequiredRole] = React.useState('');

  const canManage = usePermission(MANAGE_PERMISSION);

  const create = useMutation({
    mutationFn: () =>
      api.createApprovalPolicy({
        workflow_type: workflowType.trim(),
        min_amount: minAmount.trim() || undefined,
        required_approvals: Number(requiredApprovals),
        required_role: requiredRole.trim() || undefined,
      }),
    onSuccess: () => {
      toast.success('Policy created');
      setWorkflowType('');
      setMinAmount('');
      setRequiredApprovals('1');
      setRequiredRole('');
      onCreated();
    },
    onError: (err) =>
      toast.error('Could not create policy', err instanceof SdkError ? err.message : undefined),
  });

  const approvalsNum = Number(requiredApprovals);
  const approvalsValid = Number.isInteger(approvalsNum) && approvalsNum >= 1;
  const ready = workflowType.trim().length > 0 && amountValid(minAmount) && approvalsValid;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New approval policy</DialogTitle>
          <DialogDescription>
            Require approval for a workflow at or above a minimum amount. The strictest matching
            active policy applies when a request is raised.
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (ready && canManage !== false) create.mutate();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="wf">Workflow type</Label>
            <Input
              id="wf"
              placeholder="e.g. central_price"
              required
              value={workflowType}
              onChange={(e) => setWorkflowType(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="min-amount">Minimum amount</Label>
            <Input
              id="min-amount"
              inputMode="decimal"
              placeholder="0.00"
              value={minAmount}
              onChange={(e) => setMinAmount(e.target.value)}
              aria-invalid={!amountValid(minAmount)}
            />
            <span className="text-xs text-muted-foreground">
              The policy applies when the request amount is at or above this. Blank means 0.
            </span>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="approvals">Required approvals</Label>
            <Input
              id="approvals"
              type="number"
              min={1}
              step={1}
              required
              value={requiredApprovals}
              onChange={(e) => setRequiredApprovals(e.target.value)}
              aria-invalid={!approvalsValid}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="role">Required role (optional)</Label>
            <Input
              id="role"
              placeholder="e.g. finance_manager"
              value={requiredRole}
              onChange={(e) => setRequiredRole(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={!ready || canManage === false || create.isPending}
              title={
                canManage === false ? "You don't have permission to manage policies" : undefined
              }
            >
              {create.isPending ? 'Creating…' : 'Create policy'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function EditPolicyDialog({
  policy,
  onOpenChange,
  onSaved,
}: {
  policy: ApprovalPolicy | null;
  onOpenChange: (open: boolean) => void;
  onSaved: () => void;
}) {
  const [workflowType, setWorkflowType] = React.useState('');
  const [minAmount, setMinAmount] = React.useState('');
  const [requiredApprovals, setRequiredApprovals] = React.useState('1');
  const [requiredRole, setRequiredRole] = React.useState('');

  const canManage = usePermission(MANAGE_PERMISSION);

  // Seed the form whenever a different policy is opened for editing.
  React.useEffect(() => {
    if (policy) {
      setWorkflowType(policy.workflow_type);
      setMinAmount(policy.min_amount);
      setRequiredApprovals(String(policy.required_approvals));
      setRequiredRole(policy.required_role ?? '');
    }
  }, [policy]);

  const update = useMutation({
    mutationFn: () =>
      api.updateApprovalPolicy(policy!.id, {
        workflow_type: workflowType.trim(),
        min_amount: minAmount.trim() || undefined,
        required_approvals: Number(requiredApprovals),
        required_role: requiredRole.trim() || undefined,
      }),
    onSuccess: () => {
      toast.success('Policy updated');
      onSaved();
    },
    onError: (err) =>
      toast.error('Could not update policy', err instanceof SdkError ? err.message : undefined),
  });

  const approvalsNum = Number(requiredApprovals);
  const approvalsValid = Number.isInteger(approvalsNum) && approvalsNum >= 1;
  const ready = workflowType.trim().length > 0 && amountValid(minAmount) && approvalsValid;

  return (
    <Dialog open={policy !== null} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Edit approval policy</DialogTitle>
          <DialogDescription>
            Change the workflow, minimum amount, sign-offs or required role. Enabling or disabling
            the policy is done from the policy list.
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (ready && canManage !== false && policy) update.mutate();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="edit-wf">Workflow type</Label>
            <Input
              id="edit-wf"
              required
              value={workflowType}
              onChange={(e) => setWorkflowType(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="edit-min-amount">Minimum amount</Label>
            <Input
              id="edit-min-amount"
              inputMode="decimal"
              placeholder="0.00"
              value={minAmount}
              onChange={(e) => setMinAmount(e.target.value)}
              aria-invalid={!amountValid(minAmount)}
            />
            <span className="text-xs text-muted-foreground">
              The policy applies when the request amount is at or above this. Blank means 0.
            </span>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="edit-approvals">Required approvals</Label>
            <Input
              id="edit-approvals"
              type="number"
              min={1}
              step={1}
              required
              value={requiredApprovals}
              onChange={(e) => setRequiredApprovals(e.target.value)}
              aria-invalid={!approvalsValid}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="edit-role">Required role (optional)</Label>
            <Input
              id="edit-role"
              placeholder="e.g. finance_manager"
              value={requiredRole}
              onChange={(e) => setRequiredRole(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={!ready || canManage === false || update.isPending}
              title={
                canManage === false ? "You don't have permission to manage policies" : undefined
              }
            >
              {update.isPending ? 'Saving…' : 'Save changes'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
