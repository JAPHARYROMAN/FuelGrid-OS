'use client';

import { Fragment, useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { ChevronDown, ChevronRight, FileStack, PackageCheck, Truck, Wallet } from 'lucide-react';

import { SdkError, type Delivery, type PurchaseOrder } from '@fuelgrid/sdk';
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
} from '@fuelgrid/ui';

import { DocumentActions } from '@/components/document-actions';
import { api } from '@/lib/api';
import { formatLitres, formatMoney, parseDecimal, sumMoney } from '@/lib/money';

function money(v?: string) {
  return formatMoney(v, { fallback: '0.00' });
}

function litres(v: number | string) {
  return formatLitres(v, { fallback: '0' });
}

function statusTone(status: string): 'success' | 'warning' | 'danger' | 'neutral' {
  if (status === 'received' || status === 'approved' || status === 'matched') return 'success';
  if (status === 'cancelled' || status === 'discrepancy') return 'danger';
  if (status === 'draft' || status === 'submitted' || status === 'partially_received') {
    return 'warning';
  }
  return 'neutral';
}

export default function ProcurementPage() {
  const [stationID, setStationID] = useState('');

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  const suppliers = useQuery({
    queryKey: ['suppliers'],
    queryFn: ({ signal }) => api.listSuppliers(signal),
  });
  const products = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
  });

  useEffect(() => {
    const first = stations.data?.items?.[0];
    if (!stationID && first) setStationID(first.id);
  }, [stationID, stations.data]);

  const overview = useQuery({
    queryKey: ['procurement-overview', stationID],
    queryFn: ({ signal }) => api.getProcurementOverview(stationID, signal),
    enabled: !!stationID,
  });

  // Station deliveries (goods receipts) drive the PO -> receipt linkage. Group
  // them by purchase_order_id so each PO row can surface its receipts without a
  // per-row request. A receipt without a PO link is omitted from the grouping.
  const deliveries = useQuery({
    queryKey: ['station-deliveries', stationID],
    queryFn: ({ signal }) => api.listStationDeliveries(stationID, signal),
    enabled: !!stationID,
  });

  const deliveriesByPO = useMemo(() => {
    const map = new Map<string, Delivery[]>();
    for (const d of deliveries.data?.items ?? []) {
      if (!d.purchase_order_id) continue;
      const arr = map.get(d.purchase_order_id) ?? [];
      arr.push(d);
      map.set(d.purchase_order_id, arr);
    }
    return map;
  }, [deliveries.data]);

  const supplierName = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of suppliers.data?.items ?? []) map.set(s.id, s.name);
    return map;
  }, [suppliers.data]);

  const productName = useMemo(() => {
    const map = new Map<string, string>();
    for (const p of products.data?.items ?? []) map.set(p.id, p.name);
    return map;
  }, [products.data]);

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Commerce"
        title="Procurement"
        description="Purchase orders, expected receipts, landed costs, and supplier exposure."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            {(stations.data?.items?.length ?? 0) > 0 ? (
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
            <Button asChild variant="outline" size="sm">
              <Link href="/procurement/receiving">
                <Truck className="size-4" />
                Receiving
              </Link>
            </Button>
            <DocumentActions
              onFetch={() => api.purchaseOrdersPdf()}
              filename="purchase-orders.pdf"
              permission="purchase_order.read"
              permissionMode="held"
              viewLabel="View POs"
              downloadLabel="POs PDF"
            />
            {stationID ? (
              <DocumentActions
                onFetch={() => api.stationDeliveriesPdf(stationID)}
                filename="deliveries.pdf"
                permission="inventory.read"
                stationId={stationID}
                viewLabel="View GRNs"
                downloadLabel="GRNs PDF"
              />
            ) : null}
          </div>
        }
      />

      {stations.isPending ? (
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-[120px] rounded-xl" />
          ))}
        </section>
      ) : stations.isError ? (
        <ErrorState
          title="Couldn't load stations"
          description={String((stations.error as Error).message)}
          onRetry={() => stations.refetch()}
        />
      ) : (stations.data?.items?.length ?? 0) === 0 ? (
        <EmptyState title="No stations" description="You don't have access to any stations yet." />
      ) : overview.isPending ? (
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-[120px] rounded-xl" />
          ))}
        </section>
      ) : overview.isError ? (
        (() => {
          const err = overview.error;
          const forbidden = err instanceof SdkError && err.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access to procurement' : "Couldn't load procurement"}
              description={
                forbidden
                  ? "You don't have permission to view purchase orders for this station."
                  : String((err as Error).message)
              }
              onRetry={forbidden ? undefined : () => overview.refetch()}
            />
          );
        })()
      ) : (
        <>
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <Stat
              label="Open POs"
              value={overview.data.open_purchase_orders.length}
              icon={<FileStack />}
            />
            <Stat
              label="Recent receipts"
              value={overview.data.recent_receipts.length}
              icon={<PackageCheck />}
            />
            <Stat
              label="Outstanding supplier balance"
              // Decimal-safe sum of the per-supplier outstanding strings (integer
              // cents) — Number()+reduce drifts across a long column (PAGE-002).
              value={money(
                sumMoney(overview.data.supplier_balances.map((b) => b.outstanding_amount)),
              )}
              icon={<Wallet />}
            />
          </section>

          <Card>
            <CardHeader>
              <CardTitle>Open purchase orders</CardTitle>
            </CardHeader>
            <CardContent>
              {overview.data.open_purchase_orders.length === 0 ? (
                <EmptyState
                  title="No open purchase orders"
                  description="Confirmed orders appear here."
                />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-8" />
                      <TableHead>Supplier</TableHead>
                      <TableHead>Product</TableHead>
                      <TableHead className="text-right">Ordered</TableHead>
                      <TableHead className="text-right">Received</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Linked</TableHead>
                      <TableHead className="text-right">Document</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {overview.data.open_purchase_orders.map((po) => (
                      <PORow
                        key={po.id}
                        po={po}
                        supplierName={
                          supplierName.get(po.supplier_id) ?? po.supplier_id.slice(0, 8)
                        }
                        productName={productName}
                        deliveries={deliveriesByPO.get(po.id) ?? []}
                      />
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>

          <div className="grid gap-4 xl:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Recent receipts</CardTitle>
              </CardHeader>
              <CardContent>
                {overview.data.recent_receipts.length === 0 ? (
                  <EmptyState
                    title="No receipts"
                    description="PO-backed goods receipts appear here."
                    icon={<PackageCheck />}
                  />
                ) : (
                  <div className="flex flex-col divide-y divide-border">
                    {overview.data.recent_receipts.map((r) => (
                      <div
                        key={r.id}
                        className="flex items-center justify-between gap-3 py-3 text-sm"
                      >
                        <div className="min-w-0">
                          <p className="font-mono font-medium tabular-nums text-foreground">
                            {litres(r.volume_litres)} L
                          </p>
                          <p className="truncate text-muted-foreground">
                            {new Date(r.received_at).toLocaleString()}
                          </p>
                        </div>
                        <div className="flex flex-col items-end gap-1">
                          <p className="font-mono tabular-nums text-foreground">
                            {r.landed_cost_per_litre ? money(r.landed_cost_per_litre) : '-'} / L
                          </p>
                          <Badge tone={statusTone(r.match_status)}>{r.match_status}</Badge>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Supplier balances</CardTitle>
              </CardHeader>
              <CardContent>
                {overview.data.supplier_balances.length === 0 ? (
                  <EmptyState
                    title="No approved payables"
                    description="Approved supplier invoices appear here."
                    icon={<Wallet />}
                  />
                ) : (
                  <div className="flex flex-col divide-y divide-border">
                    {overview.data.supplier_balances.map((b) => (
                      <div
                        key={b.supplier_id}
                        className="flex items-center justify-between gap-3 py-3 text-sm"
                      >
                        <div>
                          <p className="font-medium text-foreground">{b.supplier_name}</p>
                          <p className="text-muted-foreground">
                            {b.invoice_count} invoice{b.invoice_count === 1 ? '' : 's'}
                          </p>
                        </div>
                        <p className="font-mono font-medium tabular-nums text-foreground">
                          {money(b.outstanding_amount)}
                        </p>
                      </div>
                    ))}
                  </div>
                )}
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Landed cost trend</CardTitle>
            </CardHeader>
            <CardContent>
              {overview.data.price_trend.length === 0 ? (
                <EmptyState
                  title="No price history"
                  description="Priced receipts build this trend."
                />
              ) : (
                <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
                  {overview.data.price_trend.map((p) => (
                    <div
                      key={`${p.received_at}-${p.product_id}`}
                      className="rounded-lg border border-border/80 bg-card p-3"
                    >
                      <p className="text-sm font-medium text-foreground">{p.product_name}</p>
                      <p className="text-xs text-muted-foreground">{p.supplier_name}</p>
                      <p className="mt-2 font-mono text-lg font-semibold tabular-nums text-foreground">
                        {money(p.landed_cost_per_litre)}
                      </p>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}

function PORow({
  po,
  supplierName,
  productName,
  deliveries,
}: {
  po: PurchaseOrder;
  supplierName: string;
  productName: Map<string, string>;
  deliveries: Delivery[];
}) {
  const [open, setOpen] = useState(false);
  const firstLine = po.lines[0];
  // ordered/received litres are decimal strings; parse each for a display-only
  // per-PO total (formatLitres rounds for the column).
  const ordered = po.lines.reduce((sum, ln) => sum + parseDecimal(ln.ordered_litres), 0);
  const received = po.lines.reduce((sum, ln) => sum + parseDecimal(ln.received_litres), 0);

  // Supplier invoices linked to this PO are fetched lazily on expand via the
  // purchase_order_id filter — never duplicating the invoice CRUD here.
  const invoices = useQuery({
    queryKey: ['supplier-invoices', 'po', po.id],
    queryFn: ({ signal }) => api.listSupplierInvoices({ purchaseOrderID: po.id }, signal),
    enabled: open,
  });
  const invoiceItems = invoices.data?.items ?? [];

  return (
    <Fragment>
      <TableRow>
        <TableCell className="align-middle">
          <button
            type="button"
            aria-label={open ? 'Collapse linkage' : 'Expand linkage'}
            aria-expanded={open}
            onClick={() => setOpen((v) => !v)}
            className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            {open ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
          </button>
        </TableCell>
        <TableCell className="font-medium">{supplierName}</TableCell>
        <TableCell className="text-muted-foreground">
          {firstLine
            ? (productName.get(firstLine.product_id) ?? firstLine.product_id.slice(0, 8))
            : '-'}
        </TableCell>
        <TableCell className="text-right font-mono tabular-nums">{litres(ordered)} L</TableCell>
        <TableCell className="text-right font-mono tabular-nums">{litres(received)} L</TableCell>
        <TableCell>
          <Badge tone={statusTone(po.status)}>{po.status.replace('_', ' ')}</Badge>
        </TableCell>
        <TableCell>
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className="inline-flex items-center gap-1">
              <PackageCheck className="size-3.5" />
              {deliveries.length}
            </span>
            <span className="inline-flex items-center gap-1">
              <Wallet className="size-3.5" />
              {open ? invoiceItems.length : '·'}
            </span>
          </div>
        </TableCell>
        <TableCell>
          <div className="flex justify-end">
            <DocumentActions
              onFetch={() => api.purchaseOrderPdf(po.id)}
              filename={`purchase-order-${po.id.slice(0, 8)}.pdf`}
              permission="purchase_order.read"
              stationId={po.station_id}
            />
          </div>
        </TableCell>
      </TableRow>
      {open ? (
        <TableRow>
          <TableCell colSpan={8} className="bg-muted/30">
            <div className="grid gap-4 p-2 md:grid-cols-2">
              <div className="flex flex-col gap-2">
                <p className="text-xs font-medium text-foreground">Goods receipts (deliveries)</p>
                {deliveries.length === 0 ? (
                  <p className="text-xs text-muted-foreground">
                    No goods receipts linked to this purchase order yet.
                  </p>
                ) : (
                  <ul className="flex flex-col gap-1.5 text-sm">
                    {deliveries.map((d) => (
                      <li
                        key={d.id}
                        className="flex items-center justify-between gap-3 rounded-md border border-border bg-card px-3 py-2"
                      >
                        <span className="font-mono tabular-nums">{litres(d.volume_litres)} L</span>
                        <span className="flex items-center gap-2">
                          <Badge tone={statusTone(d.match_status)}>{d.match_status}</Badge>
                          <span className="text-xs text-muted-foreground">
                            {new Date(d.received_at).toLocaleDateString()}
                          </span>
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
              <div className="flex flex-col gap-2">
                <p className="text-xs font-medium text-foreground">Supplier invoices</p>
                {invoices.isPending ? (
                  <Skeleton className="h-12 rounded" />
                ) : invoices.isError ? (
                  <p className="text-xs text-muted-foreground">
                    {invoices.error instanceof SdkError && invoices.error.status === 403
                      ? "You don't have permission to view supplier invoices (invoice.manage)."
                      : "Couldn't load linked supplier invoices."}
                  </p>
                ) : invoiceItems.length === 0 ? (
                  <p className="text-xs text-muted-foreground">
                    No supplier invoices recorded against this purchase order yet.
                  </p>
                ) : (
                  <ul className="flex flex-col gap-1.5 text-sm">
                    {invoiceItems.map((inv) => (
                      <li
                        key={inv.id}
                        className="flex items-center justify-between gap-3 rounded-md border border-border bg-card px-3 py-2"
                      >
                        <Link
                          href="/payables/invoices"
                          className="font-mono text-xs underline-offset-2 hover:underline"
                        >
                          {inv.invoice_number}
                        </Link>
                        <span className="flex items-center gap-2">
                          <span className="font-mono tabular-nums">{money(inv.total_amount)}</span>
                          <Badge tone={statusTone(inv.status)}>{inv.status}</Badge>
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            </div>
          </TableCell>
        </TableRow>
      ) : null}
    </Fragment>
  );
}
