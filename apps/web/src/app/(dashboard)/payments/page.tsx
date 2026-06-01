'use client';

import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Smartphone } from 'lucide-react';

import { SdkError, type MpesaTransaction } from '@fuelgrid/sdk';
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
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';

function money(n?: string) {
  return formatMoney(n);
}

/** Maps an M-Pesa transaction status to a Badge tone. */
function statusTone(status: string): 'neutral' | 'success' | 'warning' | 'danger' {
  switch (status) {
    case 'paid':
      return 'success';
    case 'pending':
      return 'warning';
    case 'failed':
    case 'cancelled':
      return 'danger';
    default:
      return 'neutral';
  }
}

interface PushForm {
  phone: string;
  amount: string;
  account_reference: string;
  description: string;
}

const blankPush: PushForm = { phone: '', amount: '', account_reference: '', description: '' };

export default function PaymentsPage() {
  const qc = useQueryClient();
  const [stationID, setStationID] = useState('');

  const [pushOpen, setPushOpen] = useState(false);
  const [pushForm, setPushForm] = useState<PushForm>(blankPush);
  const [pushError, setPushError] = useState<string | null>(null);
  const [pushNotice, setPushNotice] = useState<string | null>(null);

  const [reconcileTxn, setReconcileTxn] = useState<MpesaTransaction | null>(null);
  const [reconcileDayID, setReconcileDayID] = useState('');
  const [reconcileError, setReconcileError] = useState<string | null>(null);

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  useEffect(() => {
    const first = stations.data?.items?.[0];
    if (!stationID && first) setStationID(first.id);
  }, [stationID, stations.data]);

  const txnsKey = ['mpesa-transactions', stationID];
  const txns = useQuery({
    queryKey: txnsKey,
    queryFn: ({ signal }) => api.listMpesaTransactions({ stationID }, signal),
    enabled: !!stationID,
  });

  // Recent revenue days for the reconcile picker — sourced from the station's
  // revenue overview, only fetched once a transaction is being reconciled.
  const revenueDays = useQuery({
    queryKey: ['revenue-overview', stationID, 'for-reconcile'],
    queryFn: ({ signal }) => api.getRevenueOverview(stationID, signal),
    enabled: !!stationID && !!reconcileTxn,
  });

  const initiate = useMutation({
    mutationFn: (form: PushForm) =>
      api.initiateMpesaStkPush({
        station_id: stationID,
        phone: form.phone,
        amount: form.amount,
        account_reference: form.account_reference || undefined,
        description: form.description || undefined,
      }),
    onSuccess: (res) => {
      setPushOpen(false);
      setPushForm(blankPush);
      setPushError(null);
      setPushNotice(res.customer_message || 'STK push sent — ask the customer to enter their PIN.');
      qc.invalidateQueries({ queryKey: txnsKey });
    },
    onError: (e) =>
      setPushError(e instanceof SdkError ? e.message : 'Could not initiate the STK push'),
  });

  const reconcile = useMutation({
    mutationFn: ({ id, revenue_day_id }: { id: string; revenue_day_id: string }) =>
      api.reconcileMpesaTransaction(id, { revenue_day_id }),
    onSuccess: () => {
      setReconcileTxn(null);
      setReconcileDayID('');
      setReconcileError(null);
      qc.invalidateQueries({ queryKey: txnsKey });
    },
    onError: (e) =>
      setReconcileError(e instanceof SdkError ? e.message : 'Could not reconcile the transaction'),
  });

  const recentDays = useMemo(() => revenueDays.data?.recent_days ?? [], [revenueDays.data]);

  function submitPush() {
    if (!pushForm.phone.trim()) {
      setPushError('Phone is required');
      return;
    }
    if (!pushForm.amount.trim()) {
      setPushError('Amount is required');
      return;
    }
    initiate.mutate(pushForm);
  }

  function openReconcile(txn: MpesaTransaction) {
    setReconcileTxn(txn);
    setReconcileDayID(txn.reconciled_revenue_day_id ?? '');
    setReconcileError(null);
  }

  function submitReconcile() {
    if (!reconcileTxn) return;
    if (!reconcileDayID) {
      setReconcileError('Select a revenue day');
      return;
    }
    reconcile.mutate({ id: reconcileTxn.id, revenue_day_id: reconcileDayID });
  }

  const hasStations = (stations.data?.items?.length ?? 0) > 0;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Commerce"
        title="Payments"
        description="M-Pesa (Safaricom Daraja) mobile-money collections and reconciliation."
        actions={
          <div className="flex items-center gap-3">
            {hasStations ? (
              <label className="flex items-center gap-2 text-sm">
                <span className="text-muted-foreground">Station</span>
                <select
                  className="h-9 rounded-md border border-border bg-background px-2 text-sm"
                  value={stationID}
                  onChange={(e) => setStationID(e.target.value)}
                >
                  {stations.data!.items.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name} ({s.code})
                    </option>
                  ))}
                </select>
              </label>
            ) : null}
            <PermissionGate permission="payment.mpesa.manage" stationId={stationID || null}>
              <Button
                size="sm"
                disabled={!stationID}
                onClick={() => {
                  setPushForm(blankPush);
                  setPushError(null);
                  setPushNotice(null);
                  setPushOpen(true);
                }}
              >
                <Smartphone className="size-4" />
                Initiate STK push
              </Button>
            </PermissionGate>
          </div>
        }
      />

      {pushNotice ? (
        <p className="rounded-md bg-success/10 px-3 py-2 text-sm text-success" role="status">
          {pushNotice}
        </p>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Transactions</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {!stationID || txns.isPending ? (
            <div className="flex flex-col gap-2 p-4">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-12 rounded-lg" />
              ))}
            </div>
          ) : txns.isError ? (
            (() => {
              const err = txns.error;
              const forbidden = err instanceof SdkError && err.status === 403;
              return (
                <div className="p-4">
                  <ErrorState
                    title={forbidden ? 'No access to this station' : "Couldn't load transactions"}
                    description={
                      forbidden
                        ? "You don't have permission to view this station's payments."
                        : String((err as Error).message)
                    }
                    onRetry={forbidden ? undefined : () => txns.refetch()}
                  />
                </div>
              );
            })()
          ) : (txns.data?.items?.length ?? 0) === 0 ? (
            <div className="p-4">
              <EmptyState
                title="No M-Pesa transactions yet"
                description="Initiate an STK push to collect a mobile-money payment."
                icon={<Smartphone />}
              />
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Phone</TableHead>
                  <TableHead className="text-right">Amount</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Receipt</TableHead>
                  <TableHead>Reconciled</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {txns.data!.items.map((t: MpesaTransaction) => (
                  <TableRow key={t.id}>
                    <TableCell className="font-mono text-xs tabular-nums">{t.phone}</TableCell>
                    <TableCell className="text-right font-mono text-sm tabular-nums">
                      {money(t.amount)}
                    </TableCell>
                    <TableCell>
                      <Badge tone={statusTone(t.status)}>{t.status}</Badge>
                    </TableCell>
                    <TableCell className="font-mono text-xs tabular-nums text-muted-foreground">
                      {t.mpesa_receipt ?? '—'}
                    </TableCell>
                    <TableCell>
                      {t.reconciled_revenue_day_id ? (
                        <Badge tone="success">linked</Badge>
                      ) : (
                        <span className="text-sm text-muted-foreground">—</span>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      {t.status === 'paid' ? (
                        <PermissionGate permission="payment.mpesa.manage" stationId={t.station_id}>
                          <Button variant="ghost" size="sm" onClick={() => openReconcile(t)}>
                            {t.reconciled_revenue_day_id ? 'Re-link' : 'Reconcile'}
                          </Button>
                        </PermissionGate>
                      ) : (
                        <span className="text-sm text-muted-foreground">—</span>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* Initiate STK push */}
      <Dialog open={pushOpen} onOpenChange={setPushOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Initiate STK push</DialogTitle>
            <DialogDescription>
              Prompt a customer&apos;s phone to authorise an M-Pesa payment.
            </DialogDescription>
          </DialogHeader>

          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              submitPush();
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="phone">Phone</Label>
              <Input
                id="phone"
                value={pushForm.phone}
                onChange={(e) => setPushForm({ ...pushForm, phone: e.target.value })}
                placeholder="07XX XXX XXX"
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="amount">Amount</Label>
              <Input
                id="amount"
                inputMode="decimal"
                value={pushForm.amount}
                onChange={(e) => setPushForm({ ...pushForm, amount: e.target.value })}
                placeholder="0.00"
                className="font-mono tabular-nums"
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="reference">Account reference (optional)</Label>
              <Input
                id="reference"
                value={pushForm.account_reference}
                onChange={(e) => setPushForm({ ...pushForm, account_reference: e.target.value })}
                placeholder="e.g. pump or attendant"
                maxLength={12}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="description">Description (optional)</Label>
              <Input
                id="description"
                value={pushForm.description}
                onChange={(e) => setPushForm({ ...pushForm, description: e.target.value })}
                placeholder="Fuel payment"
              />
            </div>

            {pushError ? (
              <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                {pushError}
              </p>
            ) : null}

            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setPushOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={initiate.isPending}>
                {initiate.isPending ? 'Sending…' : 'Send prompt'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Reconcile */}
      <Dialog open={!!reconcileTxn} onOpenChange={(o) => !o && setReconcileTxn(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Reconcile transaction</DialogTitle>
            <DialogDescription>
              Match this paid collection to a revenue day&apos;s mobile-money tender.
            </DialogDescription>
          </DialogHeader>

          {reconcileTxn ? (
            <div className="flex flex-col gap-3">
              <div className="rounded-lg border border-border/80 bg-muted/40 px-3 py-2.5">
                <div className="flex items-center justify-between gap-3">
                  <span className="font-mono text-xs tabular-nums text-muted-foreground">
                    {reconcileTxn.phone}
                  </span>
                  <span className="font-mono text-sm font-semibold tabular-nums text-foreground">
                    {money(reconcileTxn.amount)}
                  </span>
                </div>
                {reconcileTxn.mpesa_receipt ? (
                  <span className="font-mono text-xs tabular-nums text-muted-foreground">
                    receipt {reconcileTxn.mpesa_receipt}
                  </span>
                ) : null}
              </div>

              <div className="flex flex-col gap-1.5">
                <Label htmlFor="revenue-day">Revenue day</Label>
                {revenueDays.isPending ? (
                  <Skeleton className="h-10 rounded-md" />
                ) : recentDays.length === 0 ? (
                  <p className="text-sm text-muted-foreground">
                    No revenue days for this station yet. Close an operating day first.
                  </p>
                ) : (
                  <select
                    id="revenue-day"
                    className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                    value={reconcileDayID}
                    onChange={(e) => setReconcileDayID(e.target.value)}
                  >
                    <option value="">Select…</option>
                    {recentDays.map((d) => (
                      <option key={d.id} value={d.id}>
                        {d.business_date} · momo {money(d.mobile_money_total)} ({d.status})
                      </option>
                    ))}
                  </select>
                )}
              </div>

              {reconcileError ? (
                <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                  {reconcileError}
                </p>
              ) : null}

              <DialogFooter>
                <Button type="button" variant="ghost" onClick={() => setReconcileTxn(null)}>
                  Cancel
                </Button>
                <Button
                  type="button"
                  disabled={reconcile.isPending || recentDays.length === 0}
                  onClick={submitReconcile}
                >
                  {reconcile.isPending ? 'Reconciling…' : 'Reconcile'}
                </Button>
              </DialogFooter>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}
