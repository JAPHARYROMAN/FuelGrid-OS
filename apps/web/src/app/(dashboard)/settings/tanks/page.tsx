'use client';

import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';

import { SdkError, type Tank } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
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
  LoadingState,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

interface FormState {
  product_id: string;
  name: string;
  code: string;
  capacity_litres: string;
  safe_min_litres: string;
  safe_max_litres: string;
  dead_stock_litres: string;
  has_water_sensor: boolean;
  has_temp_sensor: boolean;
}

const blankForm: FormState = {
  product_id: '',
  name: '',
  code: '',
  capacity_litres: '',
  safe_min_litres: '0',
  safe_max_litres: '',
  dead_stock_litres: '0',
  has_water_sensor: false,
  has_temp_sensor: false,
};

export default function TanksPage() {
  const qc = useQueryClient();
  const [stationID, setStationID] = useState('');
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Tank | null>(null);
  const [form, setForm] = useState<FormState>(blankForm);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  const products = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
  });

  // Default the station picker to the first station once loaded.
  const effectiveStation = stationID || stations.data?.items[0]?.id || '';

  const list = useQuery({
    queryKey: ['tanks', effectiveStation],
    queryFn: ({ signal }) => api.listTanks({ stationID: effectiveStation }, signal),
    enabled: Boolean(effectiveStation),
  });

  const productLookup = useMemo(
    () => new Map((products.data?.items ?? []).map((p) => [p.id, p])),
    [products.data],
  );

  function buildPayload(input: FormState) {
    return {
      product_id: input.product_id,
      name: input.name.trim(),
      code: input.code.trim(),
      capacity_litres: Number(input.capacity_litres) || 0,
      safe_min_litres: Number(input.safe_min_litres) || 0,
      safe_max_litres: Number(input.safe_max_litres) || 0,
      dead_stock_litres: Number(input.dead_stock_litres) || 0,
      has_water_sensor: input.has_water_sensor,
      has_temp_sensor: input.has_temp_sensor,
    };
  }

  const create = useMutation({
    mutationFn: (input: FormState) =>
      api.createTank({ station_id: effectiveStation, ...buildPayload(input) }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['tanks', effectiveStation] });
      setOpen(false);
      setForm(blankForm);
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  const update = useMutation({
    mutationFn: ({ id, input }: { id: string; input: FormState }) =>
      api.updateTank(id, buildPayload(input)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['tanks', effectiveStation] });
      setOpen(false);
      setEditing(null);
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  function openCreate() {
    setEditing(null);
    setForm({ ...blankForm, product_id: products.data?.items[0]?.id ?? '' });
    setSubmitError(null);
    setOpen(true);
  }

  function openEdit(t: Tank) {
    setEditing(t);
    setForm({
      product_id: t.product_id,
      name: t.name,
      code: t.code,
      capacity_litres: String(t.capacity_litres),
      safe_min_litres: String(t.safe_min_litres),
      safe_max_litres: String(t.safe_max_litres),
      dead_stock_litres: String(t.dead_stock_litres),
      has_water_sensor: t.has_water_sensor,
      has_temp_sensor: t.has_temp_sensor,
    });
    setSubmitError(null);
    setOpen(true);
  }

  function submit() {
    if (!form.product_id || !form.name.trim() || !form.code.trim()) {
      setSubmitError('Product, name, and code are required');
      return;
    }
    const cap = Number(form.capacity_litres);
    const min = Number(form.safe_min_litres);
    const max = Number(form.safe_max_litres);
    if (!(cap > 0)) {
      setSubmitError('Capacity must be greater than zero');
      return;
    }
    if (!(min <= max && max <= cap)) {
      setSubmitError('Require safe min ≤ safe max ≤ capacity');
      return;
    }
    if (editing) update.mutate({ id: editing.id, input: form });
    else create.mutate(form);
  }

  const noStations = (stations.data?.items?.length ?? 0) === 0;
  const noProducts = (products.data?.items?.length ?? 0) === 0;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
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
        <Button onClick={openCreate} disabled={noStations || noProducts || !effectiveStation}>
          <Plus className="size-4" />
          New tank
        </Button>
      </div>

      {noProducts ? (
        <EmptyState
          title="No products yet"
          description="Add at least one product before installing tanks — every tank stores one product."
        />
      ) : noStations ? (
        <EmptyState
          title="No stations yet"
          description="Create a station before installing tanks."
        />
      ) : list.isPending ? (
        <LoadingState />
      ) : list.isError ? (
        <ErrorState
          title="Couldn't load tanks"
          description={String((list.error as Error).message)}
          onRetry={() => list.refetch()}
        />
      ) : (list.data?.items?.length ?? 0) === 0 ? (
        <EmptyState
          title="No tanks at this station"
          description="Attach a tank to this station and bind it to a product."
          action={<Button onClick={openCreate}>Create one</Button>}
        />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Code</TableHead>
              <TableHead>Name</TableHead>
              <TableHead>Product</TableHead>
              <TableHead className="text-right">Capacity (L)</TableHead>
              <TableHead className="text-right">Safe band (L)</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.data!.items.map((t) => {
              const product = productLookup.get(t.product_id);
              return (
                <TableRow key={t.id}>
                  <TableCell className="font-mono text-xs">{t.code}</TableCell>
                  <TableCell className="font-medium">{t.name}</TableCell>
                  <TableCell>
                    <span className="inline-flex items-center gap-2">
                      <span
                        className="inline-block size-3 rounded-full border border-border"
                        style={{ backgroundColor: product?.color ?? '#64748b' }}
                        aria-hidden
                      />
                      {product?.name ?? '—'}
                    </span>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {t.capacity_litres.toLocaleString()}
                  </TableCell>
                  <TableCell className="text-right tabular-nums text-muted-foreground">
                    {t.safe_min_litres.toLocaleString()} – {t.safe_max_litres.toLocaleString()}
                  </TableCell>
                  <TableCell>
                    <Badge tone={t.status === 'active' ? 'success' : 'warning'}>{t.status}</Badge>
                  </TableCell>
                  <TableCell className="text-right">
                    <Button variant="ghost" size="sm" onClick={() => openEdit(t)}>
                      Edit
                    </Button>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? 'Edit tank' : 'New tank'}</DialogTitle>
            <DialogDescription>
              {editing
                ? 'Update this tank.'
                : 'Attach a tank to the selected station and bind it to a product.'}
            </DialogDescription>
          </DialogHeader>

          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              submit();
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="product">Product</Label>
              <select
                id="product"
                className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                value={form.product_id}
                onChange={(e) => setForm({ ...form, product_id: e.target.value })}
              >
                <option value="">Select…</option>
                {(products.data?.items ?? []).map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name} ({p.code})
                  </option>
                ))}
              </select>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="name">Name</Label>
                <Input
                  id="name"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  placeholder="Tank 1"
                  required
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="code">Code</Label>
                <Input
                  id="code"
                  value={form.code}
                  onChange={(e) => setForm({ ...form, code: e.target.value.toUpperCase() })}
                  placeholder="T1"
                  required
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="capacity">Capacity (L)</Label>
                <Input
                  id="capacity"
                  type="number"
                  step="0.001"
                  min="0"
                  value={form.capacity_litres}
                  onChange={(e) => setForm({ ...form, capacity_litres: e.target.value })}
                  required
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="dead_stock">Dead stock (L)</Label>
                <Input
                  id="dead_stock"
                  type="number"
                  step="0.001"
                  min="0"
                  value={form.dead_stock_litres}
                  onChange={(e) => setForm({ ...form, dead_stock_litres: e.target.value })}
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="safe_min">Safe min (L)</Label>
                <Input
                  id="safe_min"
                  type="number"
                  step="0.001"
                  min="0"
                  value={form.safe_min_litres}
                  onChange={(e) => setForm({ ...form, safe_min_litres: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="safe_max">Safe max (L)</Label>
                <Input
                  id="safe_max"
                  type="number"
                  step="0.001"
                  min="0"
                  value={form.safe_max_litres}
                  onChange={(e) => setForm({ ...form, safe_max_litres: e.target.value })}
                />
              </div>
            </div>

            <div className="flex gap-6 pt-1">
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={form.has_water_sensor}
                  onChange={(e) => setForm({ ...form, has_water_sensor: e.target.checked })}
                />
                Water sensor
              </label>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={form.has_temp_sensor}
                  onChange={(e) => setForm({ ...form, has_temp_sensor: e.target.checked })}
                />
                Temp sensor
              </label>
            </div>

            {submitError ? (
              <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                {submitError}
              </p>
            ) : null}

            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={create.isPending || update.isPending}>
                {create.isPending || update.isPending ? 'Saving…' : 'Save'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
