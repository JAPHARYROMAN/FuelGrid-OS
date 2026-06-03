'use client';

import * as React from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Wallet } from 'lucide-react';

import { SdkError, type Expense } from '@fuelgrid/sdk';
import {
  Badge,
  type BadgeProps,
  Button,
  Card,
  CardContent,
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
} from '@fuelgrid/ui';

import { DocumentActions } from '@/components/document-actions';
import { PermissionGate } from '@/components/permission-gate';
import { usePermission } from '@/hooks/use-permissions';
import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';
import { toast } from '@/lib/toast';

const STATUS_FILTERS = ['', 'draft', 'submitted', 'approved', 'posted'] as const;

function statusTone(status: string): BadgeProps['tone'] {
  switch (status) {
    case 'posted':
      return 'success';
    case 'approved':
      return 'info';
    case 'submitted':
      return 'warning';
    default:
      return 'neutral';
  }
}

export default function ExpensesPage() {
  const qc = useQueryClient();
  const [status, setStatus] = React.useState<string>('');
  const [createOpen, setCreateOpen] = React.useState(false);

  const canManage = usePermission('expense.manage');
  const canApprove = usePermission('expense.approve');
  const canPost = usePermission('expense.post');

  const list = useQuery({
    queryKey: ['expenses', status],
    queryFn: ({ signal }) => api.listExpenses({ status: status || undefined }, signal),
  });

  function invalidate() {
    void qc.invalidateQueries({ queryKey: ['expenses'] });
  }

  const submit = useMutation({
    mutationFn: (id: string) => api.submitExpense(id),
    onSuccess: () => {
      invalidate();
      toast.success('Expense submitted');
    },
    onError: (err) =>
      toast.error('Could not submit', err instanceof SdkError ? err.message : undefined),
  });

  const approve = useMutation({
    mutationFn: (id: string) => api.approveExpense(id),
    onSuccess: () => {
      invalidate();
      toast.success('Expense approved');
    },
    onError: (err) =>
      toast.error('Could not approve', err instanceof SdkError ? err.message : undefined),
  });

  const post = useMutation({
    mutationFn: (id: string) => api.postExpense(id),
    onSuccess: () => {
      invalidate();
      toast.success('Expense posted');
    },
    onError: (err) =>
      toast.error('Could not post', err instanceof SdkError ? err.message : undefined),
  });

  const items = list.data?.items ?? [];
  const busy = submit.isPending || approve.isPending || post.isPending;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Finance · Expenses"
        title="Expenses"
        description="Operating expenses with their approval state. Submit, approve, and post under separation of duties."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <DocumentActions
              onFetch={() => api.expensesPdf(status || undefined)}
              filename="expenses.pdf"
              permission="finance.read"
            />
            <PermissionGate permission="expense.manage">
              <Button type="button" size="sm" onClick={() => setCreateOpen(true)}>
                New expense
              </Button>
            </PermissionGate>
          </div>
        }
      />

      <div className="flex flex-wrap items-center gap-2">
        {STATUS_FILTERS.map((s) => (
          <Button
            key={s || 'all'}
            type="button"
            size="sm"
            variant={status === s ? 'primary' : 'secondary'}
            onClick={() => setStatus(s)}
          >
            {s === '' ? 'All' : s.charAt(0).toUpperCase() + s.slice(1)}
          </Button>
        ))}
      </div>

      {list.isPending ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-14 rounded-lg" />
          ))}
        </div>
      ) : list.isError ? (
        (() => {
          const forbidden = list.error instanceof SdkError && list.error.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access' : "Couldn't load expenses"}
              description={
                forbidden
                  ? "You don't have permission to view expenses (finance.read)."
                  : String((list.error as Error).message)
              }
              onRetry={forbidden ? undefined : () => list.refetch()}
            />
          );
        })()
      ) : items.length === 0 ? (
        <EmptyState
          title="No expenses"
          description={status ? `No ${status} expenses.` : 'Recorded expenses will appear here.'}
          icon={<Wallet />}
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Date</TableHead>
                  <TableHead>Payee</TableHead>
                  <TableHead>Account</TableHead>
                  <TableHead>Mode</TableHead>
                  <TableHead className="text-right">Amount</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((e: Expense) => (
                  <TableRow key={e.id}>
                    <TableCell className="whitespace-nowrap font-mono text-xs">
                      {e.expense_date}
                    </TableCell>
                    <TableCell>{e.payee ?? '—'}</TableCell>
                    <TableCell className="text-muted-foreground">{e.account_key}</TableCell>
                    <TableCell className="text-muted-foreground">{e.payment_mode}</TableCell>
                    <TableCell className="text-right font-mono font-medium tabular-nums">
                      {formatMoney(e.amount)}
                    </TableCell>
                    <TableCell>
                      <Badge tone={statusTone(e.status)}>{e.status}</Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-2">
                        {e.status === 'draft' ? (
                          <PermissionGate permission="expense.manage">
                            <Button
                              type="button"
                              variant="secondary"
                              size="sm"
                              disabled={busy}
                              onClick={() => submit.mutate(e.id)}
                            >
                              Submit
                            </Button>
                          </PermissionGate>
                        ) : null}
                        {e.status === 'submitted' ? (
                          <PermissionGate permission="expense.approve">
                            <Button
                              type="button"
                              variant="secondary"
                              size="sm"
                              disabled={busy}
                              onClick={() => approve.mutate(e.id)}
                            >
                              Approve
                            </Button>
                          </PermissionGate>
                        ) : null}
                        {e.status === 'approved' ? (
                          <PermissionGate permission="expense.post">
                            <Button
                              type="button"
                              size="sm"
                              disabled={busy}
                              onClick={() => post.mutate(e.id)}
                            >
                              Post
                            </Button>
                          </PermissionGate>
                        ) : null}
                        {e.status === 'posted' ? (
                          <span className="text-xs text-muted-foreground">Journaled</span>
                        ) : null}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <CreateExpenseDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        canManage={canManage === true}
        onCreated={() => {
          setCreateOpen(false);
          invalidate();
        }}
      />

      {/* Surface, for non-privileged users, why approval controls are inert. */}
      {canManage === false && canApprove === false && canPost === false ? (
        <p className="text-xs text-muted-foreground">
          You have read-only access to expenses. Creating and approving requires the relevant
          finance permissions.
        </p>
      ) : null}
    </div>
  );
}

function CreateExpenseDialog({
  open,
  onOpenChange,
  canManage,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  canManage: boolean;
  onCreated: () => void;
}) {
  const [amount, setAmount] = React.useState('');
  const [payee, setPayee] = React.useState('');
  const [accountKey, setAccountKey] = React.useState('operating_expense');
  const [paymentMode, setPaymentMode] = React.useState('cash');

  const create = useMutation({
    mutationFn: () =>
      api.createExpense({
        amount,
        payee: payee || undefined,
        account_key: accountKey || undefined,
        payment_mode: paymentMode || undefined,
      }),
    onSuccess: () => {
      toast.success('Expense created');
      setAmount('');
      setPayee('');
      onCreated();
    },
    onError: (err) =>
      toast.error('Could not create expense', err instanceof SdkError ? err.message : undefined),
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New expense</DialogTitle>
          <DialogDescription>
            Record a draft expense. It then moves through submit → approve → post.
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="amount">Amount</Label>
            <Input
              id="amount"
              inputMode="decimal"
              placeholder="0.00"
              required
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="payee">Payee</Label>
            <Input
              id="payee"
              placeholder="Who was paid"
              value={payee}
              onChange={(e) => setPayee(e.target.value)}
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="account_key">Account key</Label>
              <Input
                id="account_key"
                value={accountKey}
                onChange={(e) => setAccountKey(e.target.value)}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="payment_mode">Payment mode</Label>
              <Input
                id="payment_mode"
                placeholder="cash, bank, petty_cash…"
                value={paymentMode}
                onChange={(e) => setPaymentMode(e.target.value)}
              />
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!canManage || create.isPending || !amount}>
              {create.isPending ? 'Creating…' : 'Create expense'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
