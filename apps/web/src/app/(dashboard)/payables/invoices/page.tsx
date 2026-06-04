'use client';

import * as React from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { FileText } from 'lucide-react';

import {
  SdkError,
  type Product,
  type PurchaseOrderLine,
  type Station,
  type Supplier,
  type SupplierInvoice,
  type SupplierInvoiceLine,
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
import { formatLitres, formatMoney, parseDecimal } from '@/lib/money';
import { toast } from '@/lib/toast';

function money(v?: string) {
  return formatMoney(v, { fallback: '0.00' });
}

function litres(v: number | string) {
  return formatLitres(v, { fallback: '0' });
}

function statusTone(status: string): BadgeProps['tone'] {
  switch (status) {
    case 'approved':
    case 'matched':
      return 'success';
    case 'discrepancy':
      return 'danger';
    default:
      return 'neutral';
  }
}

export default function PayablesInvoicesPage() {
  const [stationID, setStationID] = React.useState('');
  const [supplierID, setSupplierID] = React.useState('');
  const [detailID, setDetailID] = React.useState<string | null>(null);

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  const suppliers = useQuery({
    queryKey: ['suppliers'],
    queryFn: ({ signal }) => api.listSuppliers(signal),
  });

  const list = useQuery({
    queryKey: ['supplier-invoices', stationID, supplierID],
    queryFn: ({ signal }) =>
      api.listSupplierInvoices(
        { stationID: stationID || undefined, supplierID: supplierID || undefined },
        signal,
      ),
  });

  const supplierName = React.useCallback(
    (id: string) =>
      suppliers.data?.items.find((s: Supplier) => s.id === id)?.name ?? id.slice(0, 8),
    [suppliers.data],
  );

  const items = list.data?.items ?? [];
  const stationList = stations.data?.items ?? [];
  const supplierList = suppliers.data?.items ?? [];

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Finance · Payables"
        title="Supplier invoices"
        description="Invoices matched against purchase orders and goods receipts. Open one to see line-by-line variance between invoiced, ordered, and received quantities, then approve a matched invoice to post it as a payable."
      />

      <div className="flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">Station</span>
          <select
            className="h-9 rounded-md border border-border bg-background px-2 text-sm"
            value={stationID}
            onChange={(e) => setStationID(e.target.value)}
          >
            <option value="">All stations</option>
            {stationList.map((s: Station) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </label>
        <label className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">Supplier</span>
          <select
            className="h-9 rounded-md border border-border bg-background px-2 text-sm"
            value={supplierID}
            onChange={(e) => setSupplierID(e.target.value)}
          >
            <option value="">All suppliers</option>
            {supplierList.map((s: Supplier) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </label>
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
              title={forbidden ? 'No access' : "Couldn't load supplier invoices"}
              description={
                forbidden
                  ? "You don't have permission to view supplier invoices (invoice.manage)."
                  : String((list.error as Error).message)
              }
              onRetry={forbidden ? undefined : () => list.refetch()}
            />
          );
        })()
      ) : items.length === 0 ? (
        <EmptyState
          title="No supplier invoices"
          description={
            stationID || supplierID
              ? 'No invoices match the current filters.'
              : 'Supplier invoices appear here once recorded against a purchase order.'
          }
          icon={<FileText />}
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Invoice</TableHead>
                  <TableHead>Supplier</TableHead>
                  <TableHead>Received</TableHead>
                  <TableHead>Due</TableHead>
                  <TableHead className="text-right">Amount</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((inv: SupplierInvoice) => (
                  <TableRow key={inv.id}>
                    <TableCell className="font-mono text-xs">{inv.invoice_number}</TableCell>
                    <TableCell>{supplierName(inv.supplier_id)}</TableCell>
                    <TableCell className="whitespace-nowrap font-mono text-xs">
                      {inv.received_at.slice(0, 10)}
                    </TableCell>
                    <TableCell className="whitespace-nowrap font-mono text-xs">
                      {inv.due_date ?? '—'}
                    </TableCell>
                    <TableCell className="text-right font-mono font-medium tabular-nums">
                      {money(inv.total_amount)}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Badge tone={statusTone(inv.status)}>{inv.status}</Badge>
                        {inv.discrepancies.length > 0 ? (
                          <span className="text-xs text-muted-foreground">
                            {inv.discrepancies.filter((d) => d.status === 'open').length} open
                          </span>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => setDetailID(inv.id)}
                      >
                        Details
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <InvoiceDetailDialog
        invoiceID={detailID}
        onClose={() => setDetailID(null)}
        supplierName={supplierName}
      />
    </div>
  );
}

function InvoiceDetailDialog({
  invoiceID,
  onClose,
  supplierName,
}: {
  invoiceID: string | null;
  onClose: () => void;
  supplierName: (id: string) => string;
}) {
  const qc = useQueryClient();

  const detail = useQuery({
    queryKey: ['supplier-invoice', invoiceID],
    queryFn: ({ signal }) => api.getSupplierInvoice(invoiceID as string, signal),
    enabled: invoiceID !== null,
  });

  const inv = detail.data;

  // The PO carries the ordered/received litres + agreed price per line, which
  // the invoice line is compared against. Fetched lazily once the invoice loads.
  const po = useQuery({
    queryKey: ['purchase-order', inv?.purchase_order_id],
    queryFn: ({ signal }) => api.getPurchaseOrder(inv!.purchase_order_id, signal),
    enabled: !!inv?.purchase_order_id,
  });

  const products = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
    enabled: invoiceID !== null,
  });

  const productName = React.useCallback(
    (id: string) => products.data?.items.find((p: Product) => p.id === id)?.name ?? id.slice(0, 8),
    [products.data],
  );

  // Authoritative approve permission, scoped to the invoice's station.
  const canApprove = usePermission('invoice.approve', { stationID: inv?.station_id });

  const approve = useMutation({
    mutationFn: (id: string) => api.approveSupplierInvoice(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['supplier-invoices'] });
      void qc.invalidateQueries({ queryKey: ['supplier-invoice', invoiceID] });
      toast.success('Invoice approved', 'The invoice was posted as a payable.');
    },
    onError: (err) =>
      toast.error('Could not approve invoice', err instanceof SdkError ? err.message : undefined),
  });

  const poLineByID = React.useMemo(() => {
    const map = new Map<string, PurchaseOrderLine>();
    for (const ln of po.data?.lines ?? []) map.set(ln.id, ln);
    return map;
  }, [po.data]);

  const openDiscrepancies = inv?.discrepancies.filter((d) => d.status === 'open') ?? [];

  return (
    <Dialog open={invoiceID !== null} onOpenChange={(o) => (o ? undefined : onClose())}>
      <DialogContent className="w-[min(820px,calc(100%-2rem))]">
        <DialogHeader>
          <DialogTitle>Supplier invoice</DialogTitle>
          <DialogDescription>
            {inv?.invoice_number ? `Invoice ${inv.invoice_number}` : 'Invoice details'}
          </DialogDescription>
        </DialogHeader>

        {detail.isPending && invoiceID ? (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-6 rounded" />
            <Skeleton className="h-24 rounded" />
          </div>
        ) : detail.isError ? (
          <ErrorState
            title="Couldn't load invoice"
            description={
              detail.error instanceof SdkError ? detail.error.message : 'Please try again.'
            }
            onRetry={() => detail.refetch()}
          />
        ) : inv ? (
          <div className="flex flex-col gap-5">
            <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm sm:grid-cols-3">
              <Field label="Supplier" value={supplierName(inv.supplier_id)} />
              <Field label="Status" value={inv.status} />
              <Field label="Total" value={money(inv.total_amount)} mono />
              <Field label="Received" value={inv.received_at.slice(0, 10)} mono />
              <Field label="Due date" value={inv.due_date ?? '—'} mono />
            </dl>

            <section className="flex flex-col gap-2">
              <h3 className="text-sm font-medium">Line variance</h3>
              <p className="text-xs text-muted-foreground">
                Invoiced litres and amount compared against the purchase order line. A line
                attributed to a goods receipt is matched against that receipt; otherwise against the
                ordered quantity.
              </p>
              {po.isError ? (
                <p className="text-xs text-danger">
                  Couldn&apos;t load the purchase order to compute ordered/received variance.
                </p>
              ) : null}
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Product</TableHead>
                      <TableHead className="text-right">Invoiced L</TableHead>
                      <TableHead className="text-right">Ordered L</TableHead>
                      <TableHead className="text-right">Received L</TableHead>
                      <TableHead className="text-right">Variance L</TableHead>
                      <TableHead className="text-right">Amount</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {inv.lines.map((ln: SupplierInvoiceLine) => {
                      const poLine = poLineByID.get(ln.po_line_id);
                      const ordered = poLine ? parseDecimal(poLine.ordered_litres) : null;
                      const received = poLine ? parseDecimal(poLine.received_litres) : null;
                      // Variance against the per-line reference: the attributed
                      // receipt when one is set, else the ordered quantity. The
                      // backend uses the same reference for its discrepancies.
                      const reference = ln.delivery_id != null ? received : ordered;
                      const variance = reference != null ? ln.invoiced_litres - reference : null;
                      return (
                        <TableRow key={ln.id}>
                          <TableCell>{productName(ln.product_id)}</TableCell>
                          <TableCell className="text-right font-mono tabular-nums">
                            {litres(ln.invoiced_litres)}
                          </TableCell>
                          <TableCell className="text-right font-mono tabular-nums">
                            {ordered != null ? litres(ordered) : '—'}
                          </TableCell>
                          <TableCell className="text-right font-mono tabular-nums">
                            {received != null ? litres(received) : '—'}
                          </TableCell>
                          <TableCell
                            className={`text-right font-mono tabular-nums ${
                              variance != null && Math.abs(variance) > 0.0005 ? 'text-danger' : ''
                            }`}
                          >
                            {variance != null
                              ? `${variance > 0 ? '+' : ''}${litres(variance)}`
                              : '—'}
                          </TableCell>
                          <TableCell className="text-right font-mono tabular-nums">
                            {money(ln.amount)}
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>
            </section>

            {openDiscrepancies.length > 0 ? (
              <section className="flex flex-col gap-2">
                <h3 className="text-sm font-medium text-danger">Open discrepancies</h3>
                <ul className="flex flex-col gap-1.5 text-sm">
                  {openDiscrepancies.map((d) => (
                    <li
                      key={d.id}
                      className="flex items-center justify-between gap-3 rounded-md border border-danger/30 bg-danger/5 px-3 py-2"
                    >
                      <span>
                        <Badge tone="danger">{d.type}</Badge>{' '}
                        <span className="text-muted-foreground">{d.detail}</span>
                      </span>
                      <span className="font-mono tabular-nums">
                        {d.variance_amount != null
                          ? money(d.variance_amount)
                          : d.variance_litres != null
                            ? `${litres(d.variance_litres)} L`
                            : ''}
                      </span>
                    </li>
                  ))}
                </ul>
                <p className="text-xs text-muted-foreground">
                  Resolve discrepancies (in Procurement) before the invoice can be approved.
                </p>
              </section>
            ) : null}
          </div>
        ) : null}

        <DialogFooter>
          {inv && inv.status === 'matched' ? (
            <PermissionGate permission="invoice.approve" stationId={inv.station_id}>
              <Button
                type="button"
                disabled={approve.isPending || canApprove === false}
                onClick={() => approve.mutate(inv.id)}
              >
                {approve.isPending ? 'Approving…' : 'Approve invoice'}
              </Button>
            </PermissionGate>
          ) : null}
          <Button type="button" variant="ghost" onClick={onClose}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={mono ? 'font-mono tabular-nums' : undefined}>{value}</dd>
    </div>
  );
}
