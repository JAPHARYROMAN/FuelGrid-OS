'use client';

import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CheckCircle2, FileText, Truck } from 'lucide-react';

import { SdkError, type Delivery, type PurchaseOrder, type SupplierInvoice } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  Input,
  Label,
  PageHeader,
  Skeleton,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatLitres, formatMoney, parseDecimal } from '@/lib/money';

// Litres arrive as decimal strings (PO lines) or display numbers (computed
// remaining/variance); formatLitres handles both.
function fmtLitres(n: number | string) {
  return formatLitres(n, { fallback: '0' });
}

function fmtMoney(v?: string) {
  return formatMoney(v, { fallback: '0.00' });
}

function openForReceiving(po: PurchaseOrder) {
  return po.status === 'confirmed' || po.status === 'partially_received';
}

export default function ReceivingPage() {
  const qc = useQueryClient();
  const [stationID, setStationID] = useState('');
  const [poID, setPOID] = useState('');
  const [lineID, setLineID] = useState('');
  const [tankID, setTankID] = useState('');
  const [volume, setVolume] = useState('');
  const [freight, setFreight] = useState('0');
  const [duty, setDuty] = useState('0');
  const [levies, setLevies] = useState('0');
  const [invoiceNumber, setInvoiceNumber] = useState('');
  const [invoiceQty, setInvoiceQty] = useState('');
  const [invoicePrice, setInvoicePrice] = useState('');
  const [lastReceipt, setLastReceipt] = useState<Delivery | null>(null);
  const [invoice, setInvoice] = useState<SupplierInvoice | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  const tanks = useQuery({
    queryKey: ['tanks', stationID],
    queryFn: ({ signal }) => api.listTanks({ stationID }, signal),
    enabled: !!stationID,
  });
  const products = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
  });
  const suppliers = useQuery({
    queryKey: ['suppliers'],
    queryFn: ({ signal }) => api.listSuppliers(signal),
  });
  const orders = useQuery({
    queryKey: ['purchase-orders', stationID],
    queryFn: ({ signal }) => api.listPurchaseOrders({ stationID }, signal),
    enabled: !!stationID,
  });

  useEffect(() => {
    const first = stations.data?.items?.[0];
    if (!stationID && first) setStationID(first.id);
  }, [stationID, stations.data]);

  const receivablePOs = useMemo(
    () => (orders.data?.items ?? []).filter(openForReceiving),
    [orders.data],
  );

  useEffect(() => {
    const first = receivablePOs[0];
    if (!poID && first) setPOID(first.id);
  }, [poID, receivablePOs]);

  const selectedPO = receivablePOs.find((po) => po.id === poID) ?? null;

  useEffect(() => {
    const firstLine = selectedPO?.lines?.[0];
    if (selectedPO && (!lineID || !selectedPO.lines.some((ln) => ln.id === lineID))) {
      setLineID(firstLine?.id ?? '');
      setInvoicePrice(firstLine?.unit_price ?? '');
    }
  }, [lineID, selectedPO]);

  const selectedLine = selectedPO?.lines.find((ln) => ln.id === lineID) ?? null;

  useEffect(() => {
    if (!selectedLine || tankID) return;
    const tank = (tanks.data?.items ?? []).find((t) => t.product_id === selectedLine.product_id);
    if (tank) setTankID(tank.id);
  }, [selectedLine, tankID, tanks.data]);

  const productName = useMemo(() => {
    const map = new Map<string, string>();
    for (const p of products.data?.items ?? []) map.set(p.id, p.name);
    return map;
  }, [products.data]);

  const supplierName = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of suppliers.data?.items ?? []) map.set(s.id, s.name);
    return map;
  }, [suppliers.data]);

  const receive = useMutation({
    mutationFn: () => {
      if (!selectedPO || !selectedLine) throw new Error('Select a purchase order line');
      return api.receivePurchaseOrderReceipt(selectedPO.id, {
        tank_id: tankID,
        po_line_id: selectedLine.id,
        // volume is the raw form string; send it as a decimal string, no Number().
        volume_litres: volume,
        freight_amount: freight || '0',
        duty_amount: duty || '0',
        levies_amount: levies || '0',
      });
    },
    onSuccess: (res) => {
      setLastReceipt(res.delivery);
      setInvoiceQty(res.delivery.volume_litres);
      setInvoicePrice(res.delivery.line_unit_price ?? selectedLine?.unit_price ?? '');
      setSubmitError(null);
      qc.invalidateQueries({ queryKey: ['purchase-orders', stationID] });
      qc.invalidateQueries({ queryKey: ['procurement-overview', stationID] });
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : String(err)),
  });

  const recordInvoice = useMutation({
    mutationFn: () => {
      if (!selectedPO || !selectedLine) throw new Error('Select a purchase order line');
      return api.recordSupplierInvoice({
        purchase_order_id: selectedPO.id,
        invoice_number: invoiceNumber.trim(),
        lines: [
          {
            po_line_id: selectedLine.id,
            delivery_id: lastReceipt?.id,
            invoiced_litres: Number(invoiceQty),
            unit_price: invoicePrice || selectedLine.unit_price,
          },
        ],
      });
    },
    onSuccess: (inv) => {
      setInvoice(inv);
      setSubmitError(null);
      qc.invalidateQueries({ queryKey: ['procurement-overview', stationID] });
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : String(err)),
  });

  const resolveFirst = useMutation({
    mutationFn: async () => {
      const open = invoice?.discrepancies.find((d) => d.status === 'open');
      if (!open || !invoice) throw new Error('No open discrepancy');
      await api.resolveProcurementDiscrepancy(open.id);
      return api.getSupplierInvoice(invoice.id);
    },
    onSuccess: (inv) => setInvoice(inv),
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : String(err)),
  });

  const approve = useMutation({
    mutationFn: async () => {
      if (!invoice) throw new Error('No invoice selected');
      return api.approveSupplierInvoice(invoice.id);
    },
    onSuccess: (inv) => {
      setInvoice(inv);
      setSubmitError(null);
      qc.invalidateQueries({ queryKey: ['procurement-overview', stationID] });
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : String(err)),
  });

  const projectedReceipt = Number(volume || 0);
  // PO-line litres are decimal strings; parse for these display-only figures.
  const orderedLitres = selectedLine ? parseDecimal(selectedLine.ordered_litres) : 0;
  const receivedLitres = selectedLine ? parseDecimal(selectedLine.received_litres) : 0;
  const remaining = selectedLine ? Math.max(0, orderedLitres - receivedLitres) : 0;
  const projectedVariance = selectedLine ? receivedLitres + projectedReceipt - orderedLitres : 0;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Procurement"
        title="Receiving"
        description="Receive against confirmed orders, capture landed cost, match invoices, and approve payables."
        actions={
          (stations.data?.items?.length ?? 0) > 0 ? (
            <label className="flex items-center gap-2 text-sm">
              <span className="text-muted-foreground">Station</span>
              <select
                className="h-9 rounded-md border border-border bg-background px-2 text-sm"
                value={stationID}
                onChange={(e) => {
                  setStationID(e.target.value);
                  setPOID('');
                  setLineID('');
                  setTankID('');
                  setLastReceipt(null);
                  setInvoice(null);
                }}
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

      {stations.isPending || (stationID && orders.isPending) ? (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_380px]">
          <div className="flex flex-col gap-4">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-44 rounded-xl" />
            ))}
          </div>
          <Skeleton className="h-64 rounded-xl" />
        </div>
      ) : stations.isError || orders.isError ? (
        <ErrorState
          title="Couldn't load receiving data"
          description={String(((stations.error || orders.error) as Error).message)}
          onRetry={() => {
            stations.refetch();
            orders.refetch();
          }}
        />
      ) : receivablePOs.length === 0 ? (
        <EmptyState
          title="No receivable purchase orders"
          description="Confirmed or partially received orders for this station appear here."
        />
      ) : (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_380px]">
          <div className="flex flex-col gap-4">
            <Card>
              <CardHeader>
                <CardTitle>Purchase order</CardTitle>
              </CardHeader>
              <CardContent className="grid gap-3 md:grid-cols-2">
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="po">Order</Label>
                  <select
                    id="po"
                    className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                    value={poID}
                    onChange={(e) => {
                      setPOID(e.target.value);
                      setLineID('');
                      setLastReceipt(null);
                      setInvoice(null);
                    }}
                  >
                    {receivablePOs.map((po) => (
                      <option key={po.id} value={po.id}>
                        {supplierName.get(po.supplier_id) ?? po.supplier_id.slice(0, 8)} -{' '}
                        {po.status}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="line">Line</Label>
                  <select
                    id="line"
                    className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                    value={lineID}
                    onChange={(e) => {
                      setLineID(e.target.value);
                      const line = selectedPO?.lines.find((ln) => ln.id === e.target.value);
                      setInvoicePrice(line?.unit_price ?? '');
                    }}
                  >
                    {(selectedPO?.lines ?? []).map((ln) => (
                      <option key={ln.id} value={ln.id}>
                        {productName.get(ln.product_id) ?? ln.product_id.slice(0, 8)} -{' '}
                        {fmtLitres(ln.ordered_litres)} L
                      </option>
                    ))}
                  </select>
                </div>
                <div className="rounded-lg border border-border/80 bg-muted/40 p-3 text-sm">
                  <p className="text-muted-foreground">Ordered</p>
                  <p className="font-mono font-medium tabular-nums text-foreground">
                    {selectedLine ? fmtLitres(selectedLine.ordered_litres) : '0'} L
                  </p>
                </div>
                <div className="rounded-lg border border-border/80 bg-muted/40 p-3 text-sm">
                  <p className="text-muted-foreground">Remaining</p>
                  <p className="font-mono font-medium tabular-nums text-foreground">
                    {fmtLitres(remaining)} L
                  </p>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Goods receipt</CardTitle>
              </CardHeader>
              <CardContent className="grid gap-3 md:grid-cols-2">
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="tank">Tank</Label>
                  <select
                    id="tank"
                    className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                    value={tankID}
                    onChange={(e) => setTankID(e.target.value)}
                  >
                    {(tanks.data?.items ?? [])
                      .filter((t) => !selectedLine || t.product_id === selectedLine.product_id)
                      .map((t) => (
                        <option key={t.id} value={t.id}>
                          {t.name} ({t.code})
                        </option>
                      ))}
                  </select>
                </div>
                <Field label="Received litres" value={volume} onChange={setVolume} step="0.001" />
                <Field label="Freight" value={freight} onChange={setFreight} step="0.01" />
                <Field label="Duty" value={duty} onChange={setDuty} step="0.01" />
                <Field label="Levies" value={levies} onChange={setLevies} step="0.01" />
                <div className="flex items-end">
                  <Button
                    onClick={() => receive.mutate()}
                    disabled={!tankID || !lineID || !volume || receive.isPending}
                  >
                    <Truck className="size-4" />
                    {receive.isPending ? 'Receiving...' : 'Receive'}
                  </Button>
                </div>
                <div className="md:col-span-2">
                  <Badge tone={projectedVariance === 0 ? 'success' : 'warning'}>
                    Projected variance {fmtLitres(projectedVariance)} L
                  </Badge>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Supplier invoice</CardTitle>
              </CardHeader>
              <CardContent className="grid gap-3 md:grid-cols-2">
                <Field label="Invoice number" value={invoiceNumber} onChange={setInvoiceNumber} />
                <Field
                  label="Invoice litres"
                  value={invoiceQty}
                  onChange={setInvoiceQty}
                  step="0.001"
                />
                <Field
                  label="Unit price"
                  value={invoicePrice}
                  onChange={setInvoicePrice}
                  step="0.01"
                />
                <div className="flex items-end">
                  <Button
                    onClick={() => recordInvoice.mutate()}
                    disabled={
                      !invoiceNumber.trim() ||
                      !invoiceQty ||
                      !invoicePrice ||
                      recordInvoice.isPending
                    }
                  >
                    <FileText className="size-4" />
                    {recordInvoice.isPending ? 'Recording...' : 'Record invoice'}
                  </Button>
                </div>
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Match result</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-4 text-sm">
              {lastReceipt ? (
                <div className="flex flex-col gap-2 rounded-lg border border-border/80 bg-muted/40 p-3">
                  <p className="font-mono font-medium tabular-nums text-foreground">
                    {fmtLitres(lastReceipt.volume_litres)} L received
                  </p>
                  <p className="text-muted-foreground">
                    Landed cost{' '}
                    <span className="font-mono tabular-nums text-foreground">
                      {fmtMoney(lastReceipt.landed_cost_per_litre)}
                    </span>{' '}
                    / L
                  </p>
                  <Badge tone={lastReceipt.match_status === 'matched' ? 'success' : 'warning'}>
                    {lastReceipt.match_status}
                  </Badge>
                </div>
              ) : (
                <EmptyState
                  title="No receipt yet"
                  description="Receipt status appears after posting."
                />
              )}

              {invoice ? (
                <div className="flex flex-col gap-3 rounded-lg border border-border/80 bg-muted/40 p-3">
                  <div className="flex items-center justify-between">
                    <div>
                      <p className="font-medium text-foreground">{invoice.invoice_number}</p>
                      <p className="font-mono tabular-nums text-muted-foreground">
                        {fmtMoney(invoice.total_amount)}
                      </p>
                    </div>
                    <Badge
                      tone={
                        invoice.status === 'approved' || invoice.status === 'matched'
                          ? 'success'
                          : invoice.status === 'discrepancy'
                            ? 'danger'
                            : 'warning'
                      }
                    >
                      {invoice.status}
                    </Badge>
                  </div>

                  {invoice.discrepancies.filter((d) => d.status === 'open').length > 0 ? (
                    <div className="flex flex-col gap-2">
                      {invoice.discrepancies
                        .filter((d) => d.status === 'open')
                        .map((d) => (
                          <div key={d.id} className="rounded-md bg-danger/10 px-3 py-2 text-danger">
                            {d.type}: {d.detail}
                          </div>
                        ))}
                      <Button
                        onClick={() => resolveFirst.mutate()}
                        disabled={resolveFirst.isPending}
                      >
                        <CheckCircle2 className="size-4" />
                        Resolve discrepancy
                      </Button>
                    </div>
                  ) : null}

                  <Button
                    onClick={() => approve.mutate()}
                    disabled={invoice.status !== 'matched' || approve.isPending}
                  >
                    <CheckCircle2 className="size-4" />
                    {approve.isPending ? 'Approving...' : 'Approve invoice'}
                  </Button>
                </div>
              ) : (
                <EmptyState
                  title="No invoice yet"
                  description="Invoice match status appears after recording."
                />
              )}

              {submitError ? (
                <p className="rounded-md bg-danger/10 px-3 py-2 text-danger" role="alert">
                  {submitError}
                </p>
              ) : null}
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  );
}

function Field({
  label,
  value,
  onChange,
  step,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  step?: string;
}) {
  const id = label.toLowerCase().replaceAll(' ', '-');
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        type={step ? 'number' : 'text'}
        min={step ? '0' : undefined}
        step={step}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </div>
  );
}
