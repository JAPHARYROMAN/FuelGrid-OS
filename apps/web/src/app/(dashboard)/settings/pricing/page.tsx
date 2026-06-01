'use client';

import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Pencil, Tag } from 'lucide-react';

import { SdkError, type PriceBoardEntry } from '@fuelgrid/sdk';
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

interface PriceFormState {
  productID: string;
  productName: string;
  unit_price: string;
  effective_from: string;
  reason: string;
}

export default function PricingPage() {
  const qc = useQueryClient();
  const [stationID, setStationID] = useState('');
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<PriceFormState | null>(null);
  const [formError, setFormError] = useState<string | null>(null);

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });

  const effectiveStation = stationID || stations.data?.items[0]?.id || '';

  const board = useQuery({
    queryKey: ['price-board', effectiveStation],
    queryFn: ({ signal }) => api.getPriceBoard(effectiveStation, signal),
    enabled: Boolean(effectiveStation),
  });

  const setPrice = useMutation({
    mutationFn: (input: PriceFormState) =>
      api.setPrice(effectiveStation, {
        product_id: input.productID,
        unit_price: input.unit_price.trim(),
        // effective_from is optional; empty means "now" on the backend.
        effective_from: input.effective_from.trim() ? input.effective_from.trim() : undefined,
        reason: input.reason.trim() ? input.reason.trim() : undefined,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['price-board', effectiveStation] });
      setOpen(false);
      setForm(null);
    },
    onError: (err) => setFormError(err instanceof SdkError ? err.message : 'Could not set price'),
  });

  const noStations = (stations.data?.items?.length ?? 0) === 0;
  const entries = useMemo(() => board.data?.items ?? [], [board.data]);

  function openEditor(entry: PriceBoardEntry) {
    setForm({
      productID: entry.product_id,
      productName: entry.product_name,
      // Prefill with the current active price so an edit is a small delta.
      unit_price: entry.active_price ?? '',
      effective_from: '',
      reason: '',
    });
    setFormError(null);
    setOpen(true);
  }

  function submit() {
    if (!form) return;
    const price = Number(form.unit_price);
    if (!form.unit_price.trim() || Number.isNaN(price) || price < 0) {
      setFormError('Enter a valid, non-negative price');
      return;
    }
    setPrice.mutate(form);
  }

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Settings"
        title="Pricing"
        description="The selling price book per station. Set a price now or schedule it for a future effective date."
        actions={
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="station">Station</Label>
            <select
              id="station"
              className="h-10 min-w-56 rounded-md border border-border bg-background px-3 text-sm"
              value={effectiveStation}
              onChange={(e) => setStationID(e.target.value)}
              disabled={noStations}
            >
              {(stations.data?.items ?? []).map((st) => (
                <option key={st.id} value={st.id}>
                  {st.name} ({st.code})
                </option>
              ))}
            </select>
          </div>
        }
      />

      {noStations ? (
        <EmptyState title="No stations yet" description="Create a station before setting prices." />
      ) : board.isPending ? (
        <Skeleton className="h-64 rounded-xl" />
      ) : board.isError ? (
        <ErrorState
          title="Couldn't load the price board"
          description={String((board.error as Error).message)}
          onRetry={() => board.refetch()}
        />
      ) : entries.length === 0 ? (
        <EmptyState
          title="No products to price"
          description="Add products to the catalogue first — each priced grade shows here."
        />
      ) : (
        <Card>
          <CardHeader className="flex-row items-center gap-2 space-y-0">
            <Tag className="size-4 text-muted-foreground" />
            <CardTitle>Price board</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Product</TableHead>
                  <TableHead className="text-right">Active price</TableHead>
                  <TableHead className="text-right">Next price</TableHead>
                  <TableHead>Effective from</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {entries.map((e) => (
                  <TableRow key={e.product_id}>
                    <TableCell>
                      <span className="inline-flex items-center gap-2">
                        <span
                          className="inline-block size-3 rounded-full border border-border"
                          style={{ backgroundColor: e.product_color }}
                          aria-hidden
                        />
                        <span className="font-medium">{e.product_name}</span>
                        <span className="font-mono text-xs text-muted-foreground">
                          {e.product_code}
                        </span>
                      </span>
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums">
                      {e.active_price ? (
                        formatMoney(e.active_price)
                      ) : (
                        <Badge tone="warning">Unpriced</Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums">
                      {e.next_price ? formatMoney(e.next_price) : '—'}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {e.next_effective_from
                        ? new Date(e.next_effective_from).toLocaleString()
                        : '—'}
                    </TableCell>
                    <TableCell className="text-right">
                      <PermissionGate permission="price.change" stationId={effectiveStation}>
                        <Button variant="ghost" size="sm" onClick={() => openEditor(e)}>
                          <Pencil className="size-4" />
                          {e.active_price ? 'Change' : 'Set price'}
                        </Button>
                      </PermissionGate>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      {/* Set / change price dialog */}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Set price{form ? ` — ${form.productName}` : ''}</DialogTitle>
            <DialogDescription>
              Leave “effective from” blank to apply immediately, or pick a future time to schedule
              the change.
            </DialogDescription>
          </DialogHeader>
          {form ? (
            <form
              className="flex flex-col gap-3"
              onSubmit={(ev) => {
                ev.preventDefault();
                submit();
              }}
            >
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="unit_price">Unit price</Label>
                <Input
                  id="unit_price"
                  type="number"
                  step="0.01"
                  min="0"
                  value={form.unit_price}
                  onChange={(e) => setForm({ ...form, unit_price: e.target.value })}
                  required
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="effective_from">Effective from (optional)</Label>
                <Input
                  id="effective_from"
                  type="datetime-local"
                  value={form.effective_from}
                  onChange={(e) => setForm({ ...form, effective_from: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="reason">Reason (optional)</Label>
                <Input
                  id="reason"
                  value={form.reason}
                  onChange={(e) => setForm({ ...form, reason: e.target.value })}
                  placeholder="e.g. supplier price update"
                />
              </div>
              {formError ? (
                <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                  {formError}
                </p>
              ) : null}
              <DialogFooter>
                <Button type="button" variant="ghost" onClick={() => setOpen(false)}>
                  Cancel
                </Button>
                <Button type="submit" disabled={setPrice.isPending}>
                  {setPrice.isPending ? 'Saving…' : 'Save price'}
                </Button>
              </DialogFooter>
            </form>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}
