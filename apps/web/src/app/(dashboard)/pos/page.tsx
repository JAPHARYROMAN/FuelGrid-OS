'use client';

import { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Banknote, Plus, Receipt, Trash2 } from 'lucide-react';

import { SdkError, type Customer, type OperationsShift, type Payment } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  Input,
  Label,
  PageHeader,
  Separator,
  Skeleton,
} from '@fuelgrid/ui';

import { triggerDownload } from '@/components/document-actions';
import { PermissionGate } from '@/components/permission-gate';
import { api } from '@/lib/api';
import { usePermission } from '@/hooks/use-permissions';
import { formatLitres, formatMoney, parseDecimal, sumMoney } from '@/lib/money';
import { toast } from '@/lib/toast';

// ---------------------------------------------------------------------------
// SCOPE NOTE (Feature 4.1 / 4.4) — read this before extending the POS.
//
// The backend has NO ad-hoc "POS sale-create" endpoint. Fuel sales are
// RECOGNIZED server-side from metered litres when a shift is approved
// (revenue.RecognizeShiftSales) — they are never POSTed from a till. The only
// supported sale-side write against an open shift is a TENDER record:
//
//     POST /api/v1/shifts/{id}/payments   (permission: payment.record)
//         { tender_type, amount, reference?, customer_id?, notes?, allow_over_limit? }
//
// So this page is an honest point-of-sale for the contract that exists: it
// records what the customer paid against the active shift, splits a sale total
// across tenders (cash / card / mobile money / voucher / credit), reconciles
// the tendered total against recognized revenue (server-authoritative, exact
// decimals), and prints a receipt. The fuel quantity/product is captured for
// the receipt only — the server values metered litres into the recognized sale
// at shift approval, so litres entered here never become a sale record.
//
// GAP (documented): there is no till-driven metered-sale endpoint, no
// server-side server-authoritative unit price for an ad-hoc line (price is
// applied at recognition), and the payments table has no idempotency key, so
// duplicate-submit protection is client-side only (see `submitting` guard).
// ---------------------------------------------------------------------------

const TENDERS = [
  { value: 'cash', label: 'Cash' },
  { value: 'card', label: 'Card' },
  { value: 'mobile_money', label: 'Mobile money' },
  { value: 'voucher', label: 'Voucher' },
  { value: 'credit', label: 'Credit' },
] as const;

type TenderType = (typeof TENDERS)[number]['value'];

interface TenderLine {
  /** Stable client key for list rendering. */
  key: string;
  type: TenderType;
  amount: string;
  reference: string;
}

let lineSeq = 0;
function newLine(type: TenderType = 'cash'): TenderLine {
  lineSeq += 1;
  return { key: `tl-${lineSeq}`, type, amount: '', reference: '' };
}

function money(n?: string | number) {
  return formatMoney(n);
}

/** Maps a recorded payment's tender to a friendly label for the receipt. */
function tenderLabel(t: string): string {
  return TENDERS.find((x) => x.value === t)?.label ?? t;
}

export default function PosPage() {
  const qc = useQueryClient();
  const [stationID, setStationID] = useState('');
  const [shiftID, setShiftID] = useState('');

  // Sale composition (receipt context — quantity/product are NOT a server sale).
  const [productID, setProductID] = useState('');
  const [nozzleRef, setNozzleRef] = useState('');
  const [litres, setLitres] = useState('');
  const [saleTotal, setSaleTotal] = useState('');

  // Split tenders.
  const [lines, setLines] = useState<TenderLine[]>([newLine('cash')]);
  const [customerID, setCustomerID] = useState('');

  const [formError, setFormError] = useState<string | null>(null);
  // Receipt of the just-recorded transaction (the recorded tenders).
  const [receipt, setReceipt] = useState<RecordedReceipt | null>(null);
  // Client-side double-submit guard (the payments table has no idempotency key).
  const submitting = useRef(false);

  // Page-level write gate. payment.record is station-scoped, so it is only
  // meaningful once a station is chosen; the gate degrades to "no" otherwise.
  const canRecord = usePermission('payment.record', { stationID: stationID || null });

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  useEffect(() => {
    const first = stations.data?.items?.[0];
    if (!stationID && first) setStationID(first.id);
  }, [stationID, stations.data]);

  // Operations overview drives the active operating day + open shifts. The POS
  // can only record against an OPEN shift on an OPEN day.
  const overview = useQuery({
    queryKey: ['operations-overview', stationID],
    queryFn: ({ signal }) => api.getOperationsOverview(stationID, signal),
    enabled: !!stationID,
  });

  const openShifts = useMemo<OperationsShift[]>(
    () => (overview.data?.shifts ?? []).filter((s) => s.status === 'open'),
    [overview.data],
  );

  // Default to the first open shift; clear the selection when it disappears.
  useEffect(() => {
    const stillOpen = openShifts.some((s) => s.id === shiftID);
    if (!stillOpen) setShiftID(openShifts[0]?.id ?? '');
  }, [openShifts, shiftID]);

  const products = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
  });

  // Customers are needed only for a credit tender; fetch when a credit line exists.
  const hasCreditLine = lines.some((l) => l.type === 'credit');
  const customers = useQuery({
    queryKey: ['customers'],
    queryFn: ({ signal }) => api.listCustomers(signal),
    enabled: hasCreditLine,
  });

  // Server-authoritative reconciliation for the active shift: tendered so far vs
  // recognized revenue, computed in SQL numeric. Shown as the figure to settle.
  const reconciliation = useQuery({
    queryKey: ['shift-payment-reconciliation', shiftID],
    queryFn: ({ signal }) => api.getShiftPaymentReconciliation(shiftID, signal),
    enabled: !!shiftID,
  });

  // Recorded tenders for the active shift (running list + receipt source).
  const payments = useQuery({
    queryKey: ['shift-payments', shiftID],
    queryFn: ({ signal }) => api.listShiftPayments(shiftID, { limit: 100 }, signal),
    enabled: !!shiftID,
  });

  const productName = useMemo(() => {
    const map = new Map<string, string>();
    for (const p of products.data?.items ?? []) map.set(p.id, p.name);
    return (id: string) => map.get(id) ?? '';
  }, [products.data]);

  const tenderedTotal = useMemo(() => sumMoney(lines.map((l) => l.amount)), [lines]);
  const remaining = useMemo(
    () => sumMoney([saleTotal, `-${tenderedTotal}`]),
    [saleTotal, tenderedTotal],
  );
  const balanced = saleTotal.trim() !== '' && parseDecimal(remaining) === 0;

  const record = useMutation({
    mutationFn: async () => {
      // Record each tender as a discrete payment against the shift. Done
      // sequentially so a mid-batch failure surfaces with the lines already
      // posted intact (each is an append-only record).
      const recorded: Payment[] = [];
      for (const line of lines) {
        const p = await api.recordPayment(shiftID, {
          tender_type: line.type,
          amount: line.amount,
          reference: line.reference.trim() || undefined,
          customer_id: line.type === 'credit' ? customerID || undefined : undefined,
        });
        recorded.push(p);
      }
      return recorded;
    },
    onSuccess: (recorded) => {
      const station = stations.data?.items.find((s) => s.id === stationID);
      const shift = openShifts.find((s) => s.id === shiftID);
      setReceipt({
        recordedAt: new Date().toISOString(),
        stationName: station ? `${station.name} (${station.code})` : stationID,
        shiftName: shift?.name ?? shiftID,
        productName: productID ? productName(productID) : '',
        nozzleRef: nozzleRef.trim(),
        litres: litres.trim(),
        saleTotal,
        lines: recorded.map((p) => ({
          type: p.tender_type,
          amount: p.amount,
          reference: p.reference ?? '',
        })),
      });
      // Reset the till for the next customer.
      setLines([newLine('cash')]);
      setCustomerID('');
      setProductID('');
      setNozzleRef('');
      setLitres('');
      setSaleTotal('');
      setFormError(null);
      void qc.invalidateQueries({ queryKey: ['shift-payments', shiftID] });
      void qc.invalidateQueries({ queryKey: ['shift-payment-reconciliation', shiftID] });
      toast.success('Sale recorded', `${recorded.length} tender(s) posted to the shift.`);
    },
    onError: (e) => setFormError(e instanceof SdkError ? e.message : 'Could not record the sale'),
    onSettled: () => {
      submitting.current = false;
    },
  });

  function validate(): string | null {
    if (!shiftID) return 'Select an open shift first.';
    if (saleTotal.trim() === '' || parseDecimal(saleTotal) <= 0)
      return 'Enter a sale total greater than zero.';
    if (lines.length === 0) return 'Add at least one tender.';
    for (const l of lines) {
      if (l.amount.trim() === '' || parseDecimal(l.amount) <= 0)
        return 'Each tender needs an amount greater than zero.';
    }
    if (!balanced) return 'Tenders must sum exactly to the sale total.';
    if (hasCreditLine && !customerID) return 'Select a customer for the credit tender.';
    return null;
  }

  function submit() {
    // Client-side double-submit protection (no server idempotency key).
    if (submitting.current || record.isPending) return;
    const err = validate();
    if (err) {
      setFormError(err);
      return;
    }
    setFormError(null);
    submitting.current = true;
    record.mutate();
  }

  function setLine(key: string, patch: Partial<TenderLine>) {
    setLines((prev) => prev.map((l) => (l.key === key ? { ...l, ...patch } : l)));
  }

  function autoBalanceLast() {
    // Convenience: set the last tender to the outstanding remainder so it sums.
    if (saleTotal.trim() === '' || lines.length === 0) return;
    const others = sumMoney(lines.slice(0, -1).map((l) => l.amount));
    const last = sumMoney([saleTotal, `-${others}`]);
    if (parseDecimal(last) <= 0) return;
    setLine(lines[lines.length - 1]!.key, { amount: last });
  }

  const hasStations = (stations.data?.items?.length ?? 0) > 0;
  const day = overview.data?.day ?? null;
  const dayOpen = day?.status === 'open';
  const forbidden =
    canRecord === false || (overview.error instanceof SdkError && overview.error.status === 403);

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Commerce"
        title="Point of sale"
        description="Record a fuel sale's tenders against the active shift, split across payment methods, and print a receipt."
        actions={
          hasStations ? (
            <label className="flex items-center gap-2 text-sm">
              <span className="text-muted-foreground">Station</span>
              <select
                aria-label="Station"
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
          ) : undefined
        }
      />

      {forbidden && canRecord === false ? (
        <ErrorState
          title="No access to the till"
          description="You don't have permission to record payments for this station (payment.record)."
        />
      ) : !hasStations && stations.isPending ? (
        <Skeleton className="h-64 rounded-xl" />
      ) : !hasStations ? (
        <EmptyState
          title="No stations"
          description="You don't have access to any stations yet."
          icon={<Banknote />}
        />
      ) : overview.isPending ? (
        <Skeleton className="h-64 rounded-xl" />
      ) : overview.isError ? (
        <ErrorState
          title="Couldn't load the station"
          description={String((overview.error as Error).message)}
          onRetry={() => overview.refetch()}
        />
      ) : !day || !dayOpen ? (
        <EmptyState
          title="No active operating day"
          description="The till can only record against an open operating day. Open the day under Operations first."
          icon={<Banknote />}
        />
      ) : openShifts.length === 0 ? (
        <EmptyState
          title="No open shift"
          description="Open a shift for this station under Operations before recording sales at the till."
          icon={<Banknote />}
        />
      ) : (
        <div className="grid gap-6 lg:grid-cols-[1.4fr_1fr]">
          {/* ---- Composer ---- */}
          <Card>
            <CardHeader className="gap-1">
              <CardTitle>New sale</CardTitle>
              <CardDescription>
                Active day {day.business_date} · select the open shift and split the total across
                tenders.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-5">
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="pos-shift">Shift</Label>
                  <select
                    id="pos-shift"
                    aria-label="Shift"
                    className="h-9 rounded-md border border-border bg-background px-2 text-sm"
                    value={shiftID}
                    onChange={(e) => setShiftID(e.target.value)}
                  >
                    {openShifts.map((s) => (
                      <option key={s.id} value={s.id}>
                        {s.name}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="pos-product">Product (for receipt)</Label>
                  <select
                    id="pos-product"
                    aria-label="Product"
                    className="h-9 rounded-md border border-border bg-background px-2 text-sm"
                    value={productID}
                    onChange={(e) => setProductID(e.target.value)}
                    disabled={(products.data?.items?.length ?? 0) === 0}
                  >
                    <option value="">—</option>
                    {(products.data?.items ?? []).map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="pos-nozzle">Pump / nozzle (optional)</Label>
                  <Input
                    id="pos-nozzle"
                    value={nozzleRef}
                    onChange={(e) => setNozzleRef(e.target.value)}
                    placeholder="e.g. P2 N1"
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="pos-litres">Litres (optional)</Label>
                  <Input
                    id="pos-litres"
                    inputMode="decimal"
                    className="font-mono tabular-nums"
                    value={litres}
                    onChange={(e) => setLitres(e.target.value)}
                    placeholder="0.000"
                  />
                </div>
              </div>

              <div className="flex flex-col gap-1.5">
                <Label htmlFor="pos-total">Sale total</Label>
                <Input
                  id="pos-total"
                  inputMode="decimal"
                  className="font-mono text-lg tabular-nums"
                  value={saleTotal}
                  onChange={(e) => setSaleTotal(e.target.value)}
                  placeholder="0.00"
                />
              </div>

              <Separator />

              {/* Split tenders */}
              <div className="flex flex-col gap-3">
                <div className="flex items-center justify-between gap-2">
                  <Label className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                    Tenders
                  </Label>
                  <div className="flex items-center gap-2">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={autoBalanceLast}
                      disabled={saleTotal.trim() === ''}
                    >
                      Balance last
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => setLines((prev) => [...prev, newLine('cash')])}
                    >
                      <Plus className="size-4" />
                      Add tender
                    </Button>
                  </div>
                </div>

                <ul className="flex flex-col gap-2">
                  {lines.map((line, i) => (
                    <li
                      key={line.key}
                      className="grid grid-cols-[8rem_1fr_1fr_auto] items-end gap-2"
                      data-testid="tender-line"
                    >
                      <div className="flex flex-col gap-1">
                        <Label
                          htmlFor={`tender-type-${line.key}`}
                          className="text-[11px] text-muted-foreground"
                        >
                          Method
                        </Label>
                        <select
                          id={`tender-type-${line.key}`}
                          aria-label={`Tender ${i + 1} method`}
                          className="h-9 rounded-md border border-border bg-background px-2 text-sm"
                          value={line.type}
                          onChange={(e) =>
                            setLine(line.key, { type: e.target.value as TenderType })
                          }
                        >
                          {TENDERS.map((t) => (
                            <option key={t.value} value={t.value}>
                              {t.label}
                            </option>
                          ))}
                        </select>
                      </div>
                      <div className="flex flex-col gap-1">
                        <Label
                          htmlFor={`tender-amount-${line.key}`}
                          className="text-[11px] text-muted-foreground"
                        >
                          Amount
                        </Label>
                        <Input
                          id={`tender-amount-${line.key}`}
                          aria-label={`Tender ${i + 1} amount`}
                          inputMode="decimal"
                          className="font-mono tabular-nums"
                          value={line.amount}
                          onChange={(e) => setLine(line.key, { amount: e.target.value })}
                          placeholder="0.00"
                        />
                      </div>
                      <div className="flex flex-col gap-1">
                        <Label
                          htmlFor={`tender-ref-${line.key}`}
                          className="text-[11px] text-muted-foreground"
                        >
                          Reference
                        </Label>
                        <Input
                          id={`tender-ref-${line.key}`}
                          aria-label={`Tender ${i + 1} reference`}
                          value={line.reference}
                          onChange={(e) => setLine(line.key, { reference: e.target.value })}
                          placeholder="optional"
                        />
                      </div>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        aria-label={`Remove tender ${i + 1}`}
                        disabled={lines.length === 1}
                        onClick={() => setLines((prev) => prev.filter((l) => l.key !== line.key))}
                      >
                        <Trash2 className="size-4" />
                      </Button>
                    </li>
                  ))}
                </ul>

                {hasCreditLine ? (
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="pos-customer">Credit customer</Label>
                    {customers.isPending ? (
                      <Skeleton className="h-9 rounded-md" />
                    ) : (
                      <select
                        id="pos-customer"
                        aria-label="Credit customer"
                        className="h-9 rounded-md border border-border bg-background px-2 text-sm"
                        value={customerID}
                        onChange={(e) => setCustomerID(e.target.value)}
                      >
                        <option value="">Select customer…</option>
                        {(customers.data?.items ?? []).map((c: Customer) => (
                          <option key={c.id} value={c.id}>
                            {c.name} ({c.code})
                          </option>
                        ))}
                      </select>
                    )}
                    <p className="text-xs text-muted-foreground">
                      A credit tender posts an accounts-receivable charge to the customer.
                    </p>
                  </div>
                ) : null}
              </div>

              <Separator />

              {/* Totals + balance */}
              <dl className="flex flex-col gap-1.5 text-sm">
                <div className="flex items-center justify-between">
                  <dt className="text-muted-foreground">Sale total</dt>
                  <dd className="font-mono tabular-nums">{money(saleTotal || '0')}</dd>
                </div>
                <div className="flex items-center justify-between">
                  <dt className="text-muted-foreground">Tendered</dt>
                  <dd className="font-mono tabular-nums">{money(tenderedTotal)}</dd>
                </div>
                <div className="flex items-center justify-between">
                  <dt className="text-muted-foreground">Remaining</dt>
                  <dd
                    className={`font-mono tabular-nums ${
                      balanced
                        ? 'text-success'
                        : parseDecimal(remaining) < 0
                          ? 'text-danger'
                          : 'text-foreground'
                    }`}
                    data-testid="pos-remaining"
                  >
                    {money(remaining)}
                  </dd>
                </div>
              </dl>

              {!balanced && saleTotal.trim() !== '' ? (
                <p className="text-xs text-muted-foreground" role="status">
                  {parseDecimal(remaining) > 0
                    ? 'Tenders are under the sale total.'
                    : 'Tenders exceed the sale total.'}
                </p>
              ) : null}

              {formError ? (
                <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                  {formError}
                </p>
              ) : null}

              <PermissionGate permission="payment.record" stationId={stationID}>
                <Button
                  type="button"
                  className="h-11"
                  disabled={!balanced || record.isPending}
                  onClick={submit}
                >
                  <Receipt className="size-4" />
                  {record.isPending ? 'Recording…' : 'Record sale'}
                </Button>
              </PermissionGate>
            </CardContent>
          </Card>

          {/* ---- Sidebar: shift status + recent tenders ---- */}
          <div className="flex flex-col gap-6">
            <Card>
              <CardHeader className="gap-1">
                <CardTitle className="text-base">Shift settlement</CardTitle>
                <CardDescription>Tendered vs recognized revenue (server-computed).</CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-2 text-sm">
                {reconciliation.isPending ? (
                  <Skeleton className="h-20 rounded-lg" />
                ) : reconciliation.isError ? (
                  <p className="text-sm text-muted-foreground">Reconciliation unavailable.</p>
                ) : reconciliation.data ? (
                  <>
                    <Row label="Recognized" value={money(reconciliation.data.recognized)} />
                    <Row label="Tendered" value={money(reconciliation.data.tendered)} />
                    <Row
                      label="Variance"
                      value={money(reconciliation.data.variance)}
                      tone={reconciliation.data.over_threshold ? 'danger' : 'success'}
                    />
                    {reconciliation.data.over_threshold ? (
                      <p className="text-xs text-danger" role="status">
                        Tendered total is out of tolerance with recognized revenue.
                      </p>
                    ) : null}
                  </>
                ) : null}
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
                <CardTitle className="text-base">Recent tenders</CardTitle>
                {payments.data ? <Badge tone="neutral">{payments.data.items.length}</Badge> : null}
              </CardHeader>
              <CardContent className="flex flex-col gap-2 p-0">
                {payments.isPending ? (
                  <div className="flex flex-col gap-2 p-4">
                    {Array.from({ length: 3 }).map((_, i) => (
                      <Skeleton key={i} className="h-10 rounded-lg" />
                    ))}
                  </div>
                ) : payments.isError ? (
                  <div className="p-4">
                    <ErrorState
                      title="Couldn't load tenders"
                      description={String((payments.error as Error).message)}
                      onRetry={() => payments.refetch()}
                    />
                  </div>
                ) : (payments.data?.items.length ?? 0) === 0 ? (
                  <div className="p-4">
                    <EmptyState
                      title="No tenders yet"
                      description="Recorded tenders for this shift appear here."
                      icon={<Receipt />}
                    />
                  </div>
                ) : (
                  <ul className="divide-y divide-border">
                    {payments.data!.items.map((p) => (
                      <li
                        key={p.id}
                        className="flex items-center justify-between gap-3 px-4 py-2.5 text-sm"
                      >
                        <div className="flex items-center gap-2">
                          <Badge tone={p.tender_type === 'credit' ? 'warning' : 'neutral'}>
                            {tenderLabel(p.tender_type)}
                          </Badge>
                          <span className="text-xs text-muted-foreground">
                            {new Date(p.received_at).toLocaleTimeString()}
                          </span>
                        </div>
                        <span className="font-mono text-sm tabular-nums">{money(p.amount)}</span>
                      </li>
                    ))}
                  </ul>
                )}
              </CardContent>
            </Card>
          </div>
        </div>
      )}

      {receipt ? <ReceiptDialog receipt={receipt} onClose={() => setReceipt(null)} /> : null}
    </div>
  );
}

function Row({
  label,
  value,
  tone,
}: {
  label: string;
  value: string;
  tone?: 'danger' | 'success';
}) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-muted-foreground">{label}</span>
      <span
        className={
          'font-mono text-sm font-medium tabular-nums' +
          (tone === 'danger' ? ' text-danger' : tone === 'success' ? ' text-success' : '')
        }
      >
        {value}
      </span>
    </div>
  );
}

interface RecordedReceipt {
  recordedAt: string;
  stationName: string;
  shiftName: string;
  productName: string;
  nozzleRef: string;
  litres: string;
  saleTotal: string;
  lines: Array<{ type: string; amount: string; reference: string }>;
}

/** Render the receipt as printable plain text for view + download. */
function receiptText(r: RecordedReceipt): string {
  const lines: string[] = [];
  lines.push('FUELGRID OS — SALE RECEIPT');
  lines.push('='.repeat(34));
  lines.push(`Date    : ${new Date(r.recordedAt).toLocaleString()}`);
  lines.push(`Station : ${r.stationName}`);
  lines.push(`Shift   : ${r.shiftName}`);
  if (r.productName) lines.push(`Product : ${r.productName}`);
  if (r.nozzleRef) lines.push(`Nozzle  : ${r.nozzleRef}`);
  if (r.litres.trim())
    lines.push(`Litres  : ${formatLitres(r.litres, { maximumFractionDigits: 3 })}`);
  lines.push('-'.repeat(34));
  for (const l of r.lines) {
    const ref = l.reference ? ` (${l.reference})` : '';
    lines.push(`${tenderLabel(l.type).padEnd(14)} ${formatMoney(l.amount).padStart(14)}${ref}`);
  }
  lines.push('-'.repeat(34));
  lines.push(`${'TOTAL'.padEnd(14)} ${formatMoney(r.saleTotal).padStart(14)}`);
  lines.push('='.repeat(34));
  lines.push('Thank you.');
  return lines.join('\n');
}

function ReceiptDialog({ receipt, onClose }: { receipt: RecordedReceipt; onClose: () => void }) {
  const text = useMemo(() => receiptText(receipt), [receipt]);

  function download() {
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' });
    triggerDownload(blob, `receipt-${new Date(receipt.recordedAt).getTime()}.txt`);
  }

  function print() {
    const win = window.open('', '_blank', 'noopener,noreferrer,width=380,height=600');
    if (!win) {
      toast.error('Allow pop-ups to print', 'Your browser blocked the print window.');
      return;
    }
    win.document.write(
      `<pre style="font-family:monospace;font-size:12px">${text.replace(/</g, '&lt;')}</pre>`,
    );
    win.document.close();
    win.focus();
    win.print();
  }

  return (
    <Card
      className="fixed inset-x-0 bottom-0 z-50 mx-auto max-w-md rounded-b-none border-x border-t shadow-2xl"
      role="dialog"
      aria-label="Sale receipt"
    >
      <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
        <CardTitle className="text-base">Receipt</CardTitle>
        <Badge tone="success">recorded</Badge>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <pre className="max-h-72 overflow-auto rounded-lg border border-border bg-muted/40 p-3 text-[11px] leading-relaxed">
          {text}
        </pre>
        <div className="flex items-center justify-end gap-2">
          <Button type="button" variant="ghost" size="sm" onClick={onClose}>
            Close
          </Button>
          <Button type="button" variant="secondary" size="sm" onClick={download}>
            Download
          </Button>
          <Button type="button" size="sm" onClick={print}>
            Print
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
