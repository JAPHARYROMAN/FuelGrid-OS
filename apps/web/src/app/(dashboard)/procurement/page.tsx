'use client';

import { useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { Truck } from 'lucide-react';

import { SdkError, type PurchaseOrder } from '@fuelgrid/sdk';
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

function money(v?: string) {
  const n = Number(v ?? 0);
  return n.toLocaleString(undefined, { maximumFractionDigits: 2 });
}

function litres(v: number) {
  return v.toLocaleString(undefined, { maximumFractionDigits: 0 });
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
    <div className="flex flex-col gap-5">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold">Procurement</h1>
          <p className="text-sm text-muted-foreground">
            Purchase orders, expected receipts, landed costs, and supplier exposure.
          </p>
        </div>
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
        </div>
      </header>

      {stations.isPending ? (
        <LoadingState />
      ) : stations.isError ? (
        <ErrorState
          title="Couldn't load stations"
          description={String((stations.error as Error).message)}
          onRetry={() => stations.refetch()}
        />
      ) : (stations.data?.items?.length ?? 0) === 0 ? (
        <EmptyState title="No stations" description="You don't have access to any stations yet." />
      ) : overview.isPending ? (
        <LoadingState />
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
          <div className="grid gap-4 lg:grid-cols-3">
            <MetricCard label="Open POs" value={overview.data.open_purchase_orders.length} />
            <MetricCard label="Recent receipts" value={overview.data.recent_receipts.length} />
            <MetricCard
              label="Outstanding supplier balance"
              value={money(
                String(
                  overview.data.supplier_balances.reduce(
                    (sum, b) => sum + Number(b.outstanding_amount || 0),
                    0,
                  ),
                ),
              )}
            />
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Open purchase orders</CardTitle>
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
                      <TableHead>Supplier</TableHead>
                      <TableHead>Product</TableHead>
                      <TableHead className="text-right">Ordered</TableHead>
                      <TableHead className="text-right">Received</TableHead>
                      <TableHead>Status</TableHead>
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
                <CardTitle className="text-base">Recent receipts</CardTitle>
              </CardHeader>
              <CardContent>
                {overview.data.recent_receipts.length === 0 ? (
                  <EmptyState
                    title="No receipts"
                    description="PO-backed goods receipts appear here."
                  />
                ) : (
                  <div className="flex flex-col divide-y divide-border">
                    {overview.data.recent_receipts.map((r) => (
                      <div
                        key={r.id}
                        className="flex items-center justify-between gap-3 py-3 text-sm"
                      >
                        <div className="min-w-0">
                          <p className="font-medium">{litres(r.volume_litres)} L</p>
                          <p className="truncate text-muted-foreground">
                            {new Date(r.received_at).toLocaleString()}
                          </p>
                        </div>
                        <div className="text-right">
                          <p className="tabular-nums">
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
                <CardTitle className="text-base">Supplier balances</CardTitle>
              </CardHeader>
              <CardContent>
                {overview.data.supplier_balances.length === 0 ? (
                  <EmptyState
                    title="No approved payables"
                    description="Approved supplier invoices appear here."
                  />
                ) : (
                  <div className="flex flex-col divide-y divide-border">
                    {overview.data.supplier_balances.map((b) => (
                      <div
                        key={b.supplier_id}
                        className="flex items-center justify-between gap-3 py-3 text-sm"
                      >
                        <div>
                          <p className="font-medium">{b.supplier_name}</p>
                          <p className="text-muted-foreground">
                            {b.invoice_count} invoice{b.invoice_count === 1 ? '' : 's'}
                          </p>
                        </div>
                        <p className="font-medium tabular-nums">{money(b.outstanding_amount)}</p>
                      </div>
                    ))}
                  </div>
                )}
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Landed cost trend</CardTitle>
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
                      className="rounded-md border border-border p-3"
                    >
                      <p className="text-sm font-medium">{p.product_name}</p>
                      <p className="text-xs text-muted-foreground">{p.supplier_name}</p>
                      <p className="mt-2 text-lg font-semibold tabular-nums">
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

function MetricCard({ label, value }: { label: string; value: string | number }) {
  return (
    <Card>
      <CardContent className="flex flex-col gap-1 p-4">
        <span className="text-xs font-semibold uppercase text-muted-foreground">{label}</span>
        <span className="text-2xl font-semibold tabular-nums">{value}</span>
      </CardContent>
    </Card>
  );
}

function PORow({
  po,
  supplierName,
  productName,
}: {
  po: PurchaseOrder;
  supplierName: string;
  productName: Map<string, string>;
}) {
  const firstLine = po.lines[0];
  const ordered = po.lines.reduce((sum, ln) => sum + ln.ordered_litres, 0);
  const received = po.lines.reduce((sum, ln) => sum + ln.received_litres, 0);
  return (
    <TableRow>
      <TableCell className="font-medium">{supplierName}</TableCell>
      <TableCell className="text-muted-foreground">
        {firstLine
          ? (productName.get(firstLine.product_id) ?? firstLine.product_id.slice(0, 8))
          : '-'}
      </TableCell>
      <TableCell className="text-right tabular-nums">{litres(ordered)} L</TableCell>
      <TableCell className="text-right tabular-nums">{litres(received)} L</TableCell>
      <TableCell>
        <Badge tone={statusTone(po.status)}>{po.status.replace('_', ' ')}</Badge>
      </TableCell>
    </TableRow>
  );
}
