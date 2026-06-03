'use client';

import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Receipt } from 'lucide-react';

import { SdkError, type Sale } from '@fuelgrid/sdk';
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
  DialogHeader,
  DialogTitle,
  EmptyState,
  ErrorState,
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

import { api } from '@/lib/api';
import { usePermission } from '@/hooks/use-permissions';
import { formatLitres, formatMoney } from '@/lib/money';

const PAGE_SIZE = 50;

function money(n?: string) {
  return formatMoney(n);
}

function shortId(id: string) {
  return id.slice(0, 8);
}

export default function SalesPage() {
  // Recognized sales are scoped to a station + operating day on the backend
  // (GET /stations/{id}/sales?operating_day_id=…), paginated with the
  // limit/offset/has_more envelope. The station + day selectors drive the
  // query; shift + product narrow the loaded page client-side (both live on
  // each sale row). Payment method / customer are payment-level facts, not
  // attributes of a recognized sale, so they are not offered here.
  const [stationID, setStationID] = useState('');
  const [dayID, setDayID] = useState('');
  const [shiftFilter, setShiftFilter] = useState('');
  const [productFilter, setProductFilter] = useState('');
  const [offset, setOffset] = useState(0);
  const [detail, setDetail] = useState<Sale | null>(null);

  // Page-level read gate. revenue.read is station-scoped, so the answer is
  // meaningful only once a station is chosen; until then we treat it as the
  // tenant-wide check (null station) which is how the gate degrades.
  const allowed = usePermission('revenue.read', { stationID: stationID || null });

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  useEffect(() => {
    const first = stations.data?.items?.[0];
    if (!stationID && first) setStationID(first.id);
  }, [stationID, stations.data]);

  const days = useQuery({
    queryKey: ['operating-days', stationID],
    queryFn: ({ signal }) => api.listOperatingDays(stationID, signal),
    enabled: !!stationID,
  });
  // Default to the most recent operating day for the chosen station.
  useEffect(() => {
    const list = days.data?.items ?? [];
    if (!stationID) return;
    const stillValid = list.some((d) => d.id === dayID);
    if (!stillValid) setDayID(list[0]?.id ?? '');
  }, [stationID, dayID, days.data]);

  // Reset paging + client filters whenever the server query changes.
  useEffect(() => {
    setOffset(0);
    setShiftFilter('');
    setProductFilter('');
  }, [stationID, dayID]);

  // Reference data for friendlier labels + filter menus.
  const products = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
  });
  const shifts = useQuery({
    queryKey: ['shifts', stationID, dayID],
    queryFn: ({ signal }) => api.listShifts(stationID, { operatingDayID: dayID }, signal),
    enabled: !!stationID && !!dayID,
  });

  const sales = useQuery({
    queryKey: ['station-sales', stationID, dayID, offset],
    queryFn: ({ signal }) =>
      api.listStationSales(stationID, dayID, { limit: PAGE_SIZE, offset }, signal),
    enabled: !!stationID && !!dayID,
  });

  const productName = useMemo(() => {
    const map = new Map<string, string>();
    for (const p of products.data?.items ?? []) map.set(p.id, p.name);
    return (id: string) => map.get(id) ?? shortId(id);
  }, [products.data]);

  const shiftName = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of shifts.data?.items ?? []) map.set(s.id, s.name);
    return (id: string) => map.get(id) ?? shortId(id);
  }, [shifts.data]);

  const rows = useMemo(() => {
    let items = sales.data?.items ?? [];
    if (shiftFilter) items = items.filter((s) => s.shift_id === shiftFilter);
    if (productFilter) items = items.filter((s) => s.product_id === productFilter);
    return items;
  }, [sales.data, shiftFilter, productFilter]);

  const forbidden =
    allowed === false || (sales.error instanceof SdkError && sales.error.status === 403);

  const hasStations = (stations.data?.items?.length ?? 0) > 0;
  const hasDays = (days.data?.items?.length ?? 0) > 0;
  const hasMore = sales.data?.has_more ?? false;
  const pageStart = rows.length === 0 ? 0 : offset + 1;
  const pageEnd = offset + (sales.data?.items?.length ?? 0);

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Commerce"
        title="Sales"
        description="Recognized fuel sales — every metered transaction valued into gross, tax, net, and margin."
        actions={
          hasStations ? (
            <div className="flex flex-wrap items-center gap-3">
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
              <label className="flex items-center gap-2 text-sm">
                <span className="text-muted-foreground">Day</span>
                <select
                  aria-label="Operating day"
                  className="h-9 rounded-md border border-border bg-background px-2 text-sm"
                  value={dayID}
                  onChange={(e) => setDayID(e.target.value)}
                  disabled={!hasDays}
                >
                  {hasDays ? (
                    (days.data?.items ?? []).map((d) => (
                      <option key={d.id} value={d.id}>
                        {d.business_date} ({d.status})
                      </option>
                    ))
                  ) : (
                    <option value="">No operating days</option>
                  )}
                </select>
              </label>
            </div>
          ) : undefined
        }
      />

      {forbidden && allowed === false ? (
        <ErrorState
          title="No access to sales"
          description="You don't have permission to view recognized sales for this station."
        />
      ) : (
        <Card>
          <CardHeader className="flex-row flex-wrap items-center justify-between gap-3 space-y-0">
            <CardTitle>Transactions</CardTitle>
            {/* Client-side narrowing of the loaded page. */}
            <div className="flex flex-wrap items-center gap-3">
              <label className="flex items-center gap-2 text-sm">
                <span className="text-muted-foreground">Shift</span>
                <select
                  aria-label="Shift filter"
                  className="h-9 rounded-md border border-border bg-background px-2 text-sm"
                  value={shiftFilter}
                  onChange={(e) => setShiftFilter(e.target.value)}
                  disabled={(shifts.data?.items?.length ?? 0) === 0}
                >
                  <option value="">All shifts</option>
                  {(shifts.data?.items ?? []).map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="flex items-center gap-2 text-sm">
                <span className="text-muted-foreground">Product</span>
                <select
                  aria-label="Product filter"
                  className="h-9 rounded-md border border-border bg-background px-2 text-sm"
                  value={productFilter}
                  onChange={(e) => setProductFilter(e.target.value)}
                  disabled={(products.data?.items?.length ?? 0) === 0}
                >
                  <option value="">All products</option>
                  {(products.data?.items ?? []).map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </select>
              </label>
            </div>
          </CardHeader>
          <CardContent className="p-0">
            {!hasStations && stations.isPending ? (
              <div className="flex flex-col gap-2 p-4">
                {Array.from({ length: 5 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 rounded-lg" />
                ))}
              </div>
            ) : !hasStations ? (
              <div className="p-4">
                <EmptyState
                  title="No stations yet"
                  description="Add a station to start recognizing sales."
                  icon={<Receipt />}
                />
              </div>
            ) : !hasDays && days.isPending ? (
              <div className="flex flex-col gap-2 p-4">
                {Array.from({ length: 5 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 rounded-lg" />
                ))}
              </div>
            ) : !hasDays ? (
              <div className="p-4">
                <EmptyState
                  title="No operating days"
                  description="Open an operating day for this station to capture and recognize sales."
                  icon={<Receipt />}
                />
              </div>
            ) : sales.isPending ? (
              <div className="flex flex-col gap-2 p-4">
                {Array.from({ length: 5 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 rounded-lg" />
                ))}
              </div>
            ) : sales.isError ? (
              (() => {
                const err = sales.error;
                const isForbidden = err instanceof SdkError && err.status === 403;
                return (
                  <div className="p-4">
                    <ErrorState
                      title={isForbidden ? 'No access to this station' : "Couldn't load sales"}
                      description={
                        isForbidden
                          ? "You don't have permission to view this station's sales."
                          : String((err as Error).message)
                      }
                      onRetry={isForbidden ? undefined : () => sales.refetch()}
                    />
                  </div>
                );
              })()
            ) : rows.length === 0 ? (
              <div className="p-4">
                <EmptyState
                  title={
                    shiftFilter || productFilter
                      ? 'No sales match these filters'
                      : 'No recognized sales for this day'
                  }
                  description={
                    shiftFilter || productFilter
                      ? 'Try clearing the shift or product filter.'
                      : 'Sales appear here once a shift is approved and its metered litres are valued.'
                  }
                  icon={<Receipt />}
                />
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Recorded</TableHead>
                    <TableHead>Product</TableHead>
                    <TableHead>Shift</TableHead>
                    <TableHead className="text-right">Litres</TableHead>
                    <TableHead className="text-right">Unit price</TableHead>
                    <TableHead className="text-right">Gross</TableHead>
                    <TableHead className="text-right">Net</TableHead>
                    <TableHead className="text-right">Margin</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map((s) => (
                    <TableRow
                      key={s.id}
                      className="cursor-pointer"
                      onClick={() => setDetail(s)}
                      tabIndex={0}
                      role="button"
                      aria-label={`Sale ${shortId(s.id)}`}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault();
                          setDetail(s);
                        }
                      }}
                    >
                      <TableCell className="whitespace-nowrap font-mono text-xs">
                        {new Date(s.recorded_at).toLocaleString()}
                      </TableCell>
                      <TableCell>{productName(s.product_id)}</TableCell>
                      <TableCell className="text-muted-foreground">
                        {shiftName(s.shift_id)}
                      </TableCell>
                      <TableCell className="text-right font-mono text-sm tabular-nums">
                        {formatLitres(s.litres)}
                      </TableCell>
                      <TableCell className="text-right font-mono text-sm tabular-nums text-muted-foreground">
                        {money(s.unit_price)}
                      </TableCell>
                      <TableCell className="text-right font-mono text-sm tabular-nums">
                        {money(s.gross_amount)}
                      </TableCell>
                      <TableCell className="text-right font-mono text-sm tabular-nums text-muted-foreground">
                        {money(s.net_amount)}
                      </TableCell>
                      <TableCell className="text-right font-mono text-sm tabular-nums">
                        {s.margin_amount ? money(s.margin_amount) : '—'}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>

          {/* Server-side pagination over the limit/offset/has_more envelope. */}
          {hasStations && hasDays && !sales.isPending && !sales.isError && rows.length > 0 ? (
            <div className="flex items-center justify-between gap-3 border-t border-border px-4 py-3">
              <span className="text-xs text-muted-foreground">
                Showing {pageStart}–{pageEnd}
                {shiftFilter || productFilter ? ` (${rows.length} after filters)` : ''}
              </span>
              <div className="flex items-center gap-2">
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={offset === 0}
                  onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
                >
                  Previous
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={!hasMore}
                  onClick={() => setOffset((o) => o + PAGE_SIZE)}
                >
                  Next
                </Button>
              </div>
            </div>
          ) : null}
        </Card>
      )}

      {/* Sale detail — the row carries the full valued breakdown; there is no
          per-id endpoint, so the detail view reads the selected row. */}
      <Dialog open={!!detail} onOpenChange={(o) => !o && setDetail(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Sale detail</DialogTitle>
            <DialogDescription>
              {detail ? (
                <span className="font-mono text-xs">{detail.id}</span>
              ) : (
                'Recognized sale breakdown.'
              )}
            </DialogDescription>
          </DialogHeader>

          {detail ? (
            <div className="flex flex-col gap-3">
              <div className="flex items-center justify-between gap-3">
                <Badge tone="accent">{productName(detail.product_id)}</Badge>
                <span className="font-mono text-xs text-muted-foreground">
                  {new Date(detail.recorded_at).toLocaleString()}
                </span>
              </div>
              <dl className="grid grid-cols-2 gap-x-4 gap-y-2.5">
                <Field label="Shift" value={shiftName(detail.shift_id)} />
                <Field label="Litres" value={formatLitres(detail.litres)} mono />
                <Field label="Unit price" value={money(detail.unit_price)} mono />
                <Field label="Gross amount" value={money(detail.gross_amount)} mono />
                <Field
                  label="Tax"
                  value={`${money(detail.tax_amount)} (${detail.tax_rate})`}
                  mono
                />
                <Field label="Net amount" value={money(detail.net_amount)} mono />
                <Field
                  label="Unit cost"
                  value={detail.unit_cost ? money(detail.unit_cost) : '—'}
                  mono
                />
                <Field
                  label="COGS"
                  value={detail.cogs_amount ? money(detail.cogs_amount) : '—'}
                  mono
                />
                <Field
                  label="Margin"
                  value={detail.margin_amount ? money(detail.margin_amount) : '—'}
                  mono
                />
                <Field label="Nozzle" value={shortId(detail.nozzle_id)} mono />
                <Field label="Tank" value={shortId(detail.tank_id)} mono />
              </dl>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-0.5">
      <Label className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
        {label}
      </Label>
      <span className={`text-sm text-foreground ${mono ? 'font-mono tabular-nums' : ''}`.trim()}>
        {value}
      </span>
    </div>
  );
}
