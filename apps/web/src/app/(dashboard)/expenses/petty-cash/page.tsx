'use client';

import * as React from 'react';
import Link from 'next/link';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Wallet } from 'lucide-react';

import {
  SdkError,
  type PettyCashFloat,
  type PettyCashTransaction,
  type Station,
} from '@fuelgrid/sdk';
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

import { PermissionGate } from '@/components/permission-gate';
import { usePermission } from '@/hooks/use-permissions';
import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';
import { toast } from '@/lib/toast';

const TXN_TYPES = ['topup', 'spend', 'reimbursement', 'adjustment', 'transfer'] as const;
type TxnType = (typeof TXN_TYPES)[number];

function floatStatusTone(status: string): BadgeProps['tone'] {
  switch (status) {
    case 'active':
      return 'success';
    case 'suspended':
      return 'warning';
    default:
      return 'neutral';
  }
}

export default function PettyCashPage() {
  const qc = useQueryClient();
  const [createOpen, setCreateOpen] = React.useState(false);
  const [movementFor, setMovementFor] = React.useState<PettyCashFloat | null>(null);
  const [reconcileFor, setReconcileFor] = React.useState<PettyCashFloat | null>(null);
  const [activeFloat, setActiveFloat] = React.useState<PettyCashFloat | null>(null);

  const canManage = usePermission('petty_cash.manage');
  const canReconcile = usePermission('petty_cash.reconcile');

  const floats = useQuery({
    queryKey: ['petty-cash-floats'],
    queryFn: ({ signal }) => api.listPettyCashFloats(signal),
  });

  function invalidate() {
    void qc.invalidateQueries({ queryKey: ['petty-cash-floats'] });
    void qc.invalidateQueries({ queryKey: ['petty-cash-transactions'] });
  }

  const items = floats.data?.items ?? [];

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Finance · Expenses"
        title="Petty cash"
        description="Open floats, record issue / return / spend movements, and reconcile against a physical count."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Button asChild variant="ghost">
              <Link href="/expenses">
                <ArrowLeft className="size-4" />
                Expenses
              </Link>
            </Button>
            <PermissionGate permission="petty_cash.manage">
              <Button type="button" size="sm" onClick={() => setCreateOpen(true)}>
                New float
              </Button>
            </PermissionGate>
          </div>
        }
      />

      {floats.isPending ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-14 rounded-lg" />
          ))}
        </div>
      ) : floats.isError ? (
        (() => {
          const forbidden = floats.error instanceof SdkError && floats.error.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access' : "Couldn't load floats"}
              description={
                forbidden
                  ? "You don't have permission to view petty cash (finance.read)."
                  : String((floats.error as Error).message)
              }
              onRetry={forbidden ? undefined : () => floats.refetch()}
            />
          );
        })()
      ) : items.length === 0 ? (
        <EmptyState
          title="No petty cash floats"
          description="Open a float to start recording small cash movements at a station."
          icon={<Wallet />}
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Float</TableHead>
                  <TableHead className="text-right">Balance</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((f: PettyCashFloat) => (
                  <TableRow key={f.id}>
                    <TableCell className="font-medium">{f.name}</TableCell>
                    <TableCell className="text-right font-mono font-medium tabular-nums">
                      {formatMoney(f.balance)}
                    </TableCell>
                    <TableCell>
                      <Badge tone={floatStatusTone(f.status)}>{f.status}</Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-2">
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          onClick={() => setActiveFloat(f)}
                        >
                          Transactions
                        </Button>
                        <PermissionGate permission="petty_cash.manage">
                          <Button
                            type="button"
                            variant="secondary"
                            size="sm"
                            disabled={f.status !== 'active'}
                            onClick={() => setMovementFor(f)}
                          >
                            Record movement
                          </Button>
                        </PermissionGate>
                        <PermissionGate permission="petty_cash.reconcile">
                          <Button
                            type="button"
                            variant="secondary"
                            size="sm"
                            onClick={() => setReconcileFor(f)}
                          >
                            Reconcile
                          </Button>
                        </PermissionGate>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      {canManage === false && canReconcile === false ? (
        <p className="text-xs text-muted-foreground">
          You have read-only access to petty cash. Recording movements requires petty_cash.manage
          and reconciling requires petty_cash.reconcile.
        </p>
      ) : null}

      <CreateFloatDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        canManage={canManage === true}
        onCreated={() => {
          setCreateOpen(false);
          invalidate();
        }}
      />
      <MovementDialog
        float={movementFor}
        onOpenChange={(open) => {
          if (!open) setMovementFor(null);
        }}
        canManage={canManage === true}
        onSaved={() => {
          setMovementFor(null);
          invalidate();
        }}
      />
      <ReconcileDialog
        float={reconcileFor}
        onOpenChange={(open) => {
          if (!open) setReconcileFor(null);
        }}
        canReconcile={canReconcile === true}
        onSaved={() => {
          setReconcileFor(null);
          invalidate();
        }}
      />
      <TransactionsDialog
        float={activeFloat}
        onOpenChange={(open) => {
          if (!open) setActiveFloat(null);
        }}
      />
    </div>
  );
}

function CreateFloatDialog({
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
  const [name, setName] = React.useState('');
  const [stationID, setStationID] = React.useState('');

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
    enabled: open,
  });

  React.useEffect(() => {
    if (open) {
      setName('');
      setStationID('');
    }
  }, [open]);

  const effectiveStation = stationID || stations.data?.items[0]?.id || '';

  const create = useMutation({
    mutationFn: () => api.createPettyCashFloat({ station_id: effectiveStation, name: name.trim() }),
    onSuccess: () => {
      toast.success('Float opened');
      onCreated();
    },
    onError: (err) =>
      toast.error('Could not open float', err instanceof SdkError ? err.message : undefined),
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New petty cash float</DialogTitle>
          <DialogDescription>Open a float at a station to record cash movements.</DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="float-name">Name</Label>
            <Input
              id="float-name"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Front desk float"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="float-station">Station</Label>
            <select
              id="float-station"
              className="h-10 rounded-md border border-border bg-background px-3 text-sm"
              value={effectiveStation}
              onChange={(e) => setStationID(e.target.value)}
            >
              {(stations.data?.items ?? []).map((st: Station) => (
                <option key={st.id} value={st.id}>
                  {st.name} ({st.code})
                </option>
              ))}
            </select>
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={!canManage || create.isPending || !name.trim() || !effectiveStation}
            >
              {create.isPending ? 'Opening…' : 'Open float'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function MovementDialog({
  float,
  onOpenChange,
  canManage,
  onSaved,
}: {
  float: PettyCashFloat | null;
  onOpenChange: (open: boolean) => void;
  canManage: boolean;
  onSaved: () => void;
}) {
  const [txnType, setTxnType] = React.useState<TxnType>('topup');
  const [amount, setAmount] = React.useState('');
  const [description, setDescription] = React.useState('');
  const [accountKey, setAccountKey] = React.useState('');
  const [overdraw, setOverdraw] = React.useState(false);

  const open = float !== null;
  React.useEffect(() => {
    if (open) {
      setTxnType('topup');
      setAmount('');
      setDescription('');
      setAccountKey('');
      setOverdraw(false);
    }
  }, [open]);

  const record = useMutation({
    mutationFn: () =>
      api.recordPettyCashTransaction(float!.id, {
        txn_type: txnType,
        amount: amount.trim(),
        description: description.trim() || undefined,
        account_key: txnType === 'spend' && accountKey.trim() ? accountKey.trim() : undefined,
        overdraw: overdraw || undefined,
      }),
    onSuccess: () => {
      toast.success('Movement recorded');
      onSaved();
    },
    onError: (err) =>
      toast.error('Could not record movement', err instanceof SdkError ? err.message : undefined),
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Record movement</DialogTitle>
          <DialogDescription>
            {float ? `${float.name} · balance ${formatMoney(float.balance)}` : ''}
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            record.mutate();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="txn-type">Type</Label>
            <select
              id="txn-type"
              className="h-10 rounded-md border border-border bg-background px-3 text-sm"
              value={txnType}
              onChange={(e) => setTxnType(e.target.value as TxnType)}
            >
              {TXN_TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="txn-amount">Amount</Label>
            <Input
              id="txn-amount"
              inputMode="decimal"
              required
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              placeholder="0.00"
            />
          </div>
          {txnType === 'spend' ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="txn-account">Expense account (optional)</Label>
              <Input
                id="txn-account"
                value={accountKey}
                onChange={(e) => setAccountKey(e.target.value)}
                placeholder="operating_expense"
              />
            </div>
          ) : null}
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="txn-desc">Description</Label>
            <Input
              id="txn-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional"
            />
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={overdraw}
              onChange={(e) => setOverdraw(e.target.checked)}
            />
            Allow overdraw (record an authorized negative balance)
          </label>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!canManage || record.isPending || !amount.trim()}>
              {record.isPending ? 'Recording…' : 'Record movement'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ReconcileDialog({
  float,
  onOpenChange,
  canReconcile,
  onSaved,
}: {
  float: PettyCashFloat | null;
  onOpenChange: (open: boolean) => void;
  canReconcile: boolean;
  onSaved: () => void;
}) {
  const [counted, setCounted] = React.useState('');
  const [reason, setReason] = React.useState('');

  const open = float !== null;
  React.useEffect(() => {
    if (open) {
      setCounted('');
      setReason('');
    }
  }, [open]);

  const reconcile = useMutation({
    mutationFn: () =>
      api.reconcilePettyCash(float!.id, {
        counted_cash: counted.trim(),
      }),
    onSuccess: (res) => {
      const variance = res.variance ?? '0';
      toast.success('Float reconciled', `Variance ${formatMoney(variance)}`);
      onSaved();
    },
    onError: (err) =>
      toast.error('Could not reconcile', err instanceof SdkError ? err.message : undefined),
  });

  const expected = float?.balance ?? '0';
  const countedValid = counted.trim() !== '' && !Number.isNaN(Number(counted));
  // Variance is informational only; the server computes the authoritative figure.
  const variancePreview =
    countedValid && float ? (Number(counted) - Number(expected)).toFixed(2) : null;
  const hasVariance = variancePreview != null && Number(variancePreview) !== 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Reconcile float</DialogTitle>
          <DialogDescription>
            {float ? `${float.name} · expected ${formatMoney(expected)}` : ''}
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            reconcile.mutate();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="rec-counted">Counted cash</Label>
            <Input
              id="rec-counted"
              inputMode="decimal"
              required
              value={counted}
              onChange={(e) => setCounted(e.target.value)}
              placeholder="0.00"
            />
          </div>
          {variancePreview != null ? (
            <p className="text-sm text-muted-foreground">
              Variance:{' '}
              <span className="font-mono tabular-nums">{formatMoney(variancePreview)}</span>
            </p>
          ) : null}
          {hasVariance ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rec-reason">Variance reason</Label>
              <Input
                id="rec-reason"
                required
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                placeholder="Explain the over/short"
              />
            </div>
          ) : null}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={
                !canReconcile ||
                reconcile.isPending ||
                !countedValid ||
                (hasVariance && !reason.trim())
              }
            >
              {reconcile.isPending ? 'Reconciling…' : 'Reconcile'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function TransactionsDialog({
  float,
  onOpenChange,
}: {
  float: PettyCashFloat | null;
  onOpenChange: (open: boolean) => void;
}) {
  const open = float !== null;
  const txns = useQuery({
    queryKey: ['petty-cash-transactions', float?.id],
    enabled: open && Boolean(float),
    queryFn: ({ signal }) => api.listPettyCashTransactions(float!.id, signal),
  });

  const rows = txns.data?.items ?? [];

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Transactions</DialogTitle>
          <DialogDescription>{float ? float.name : ''}</DialogDescription>
        </DialogHeader>
        {txns.isPending ? (
          <div className="flex flex-col gap-2">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-10 rounded-lg" />
            ))}
          </div>
        ) : txns.isError ? (
          <ErrorState
            title="Couldn't load transactions"
            description={String((txns.error as Error).message)}
            onRetry={() => txns.refetch()}
          />
        ) : rows.length === 0 ? (
          <EmptyState title="No transactions" description="Movements will appear here." />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Type</TableHead>
                <TableHead className="text-right">Amount</TableHead>
                <TableHead className="text-right">Balance</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((t: PettyCashTransaction) => (
                <TableRow key={t.id}>
                  <TableCell>
                    {t.txn_type}
                    {t.overdraw ? <Badge tone="warning">overdraw</Badge> : null}
                  </TableCell>
                  <TableCell className="text-right font-mono tabular-nums">
                    {formatMoney(t.amount)}
                  </TableCell>
                  <TableCell className="text-right font-mono tabular-nums">
                    {formatMoney(t.balance_after)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </DialogContent>
    </Dialog>
  );
}
