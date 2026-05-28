'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';

import { SdkError, type Product } from '@fuelgrid/sdk';
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
  code: string;
  name: string;
  category: string;
  unit: string;
  default_price: string;
  tax_rate: string;
  density_kg_m3: string;
  loss_tolerance_percent: string;
  color: string;
}

const blankForm: FormState = {
  code: '',
  name: '',
  category: 'fuel',
  unit: 'litre',
  default_price: '0',
  tax_rate: '0',
  density_kg_m3: '',
  loss_tolerance_percent: '0',
  color: '#f97316',
};

const categories = ['fuel', 'gas', 'lubricant', 'additive', 'other'];
const units = ['litre', 'kg', 'unit'];

export default function ProductsPage() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Product | null>(null);
  const [form, setForm] = useState<FormState>(blankForm);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const list = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
  });

  function buildPayload(input: FormState) {
    return {
      code: input.code.trim(),
      name: input.name.trim(),
      category: input.category,
      unit: input.unit,
      default_price: Number(input.default_price) || 0,
      tax_rate: Number(input.tax_rate) || 0,
      density_kg_m3: input.density_kg_m3 ? Number(input.density_kg_m3) : undefined,
      loss_tolerance_percent: Number(input.loss_tolerance_percent) || 0,
      color: input.color,
    };
  }

  const create = useMutation({
    mutationFn: (input: FormState) => api.createProduct(buildPayload(input)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['products'] });
      setOpen(false);
      setForm(blankForm);
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  const update = useMutation({
    mutationFn: ({ id, input }: { id: string; input: FormState }) =>
      api.updateProduct(id, buildPayload(input)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['products'] });
      setOpen(false);
      setEditing(null);
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  function openCreate() {
    setEditing(null);
    setForm(blankForm);
    setSubmitError(null);
    setOpen(true);
  }

  function openEdit(p: Product) {
    setEditing(p);
    setForm({
      code: p.code,
      name: p.name,
      category: p.category,
      unit: p.unit,
      default_price: String(p.default_price),
      tax_rate: String(p.tax_rate),
      density_kg_m3: p.density_kg_m3 != null ? String(p.density_kg_m3) : '',
      loss_tolerance_percent: String(p.loss_tolerance_percent),
      color: p.color,
    });
    setSubmitError(null);
    setOpen(true);
  }

  function submit() {
    if (!form.code.trim() || !form.name.trim()) {
      setSubmitError('Code and name are required');
      return;
    }
    if (editing) update.mutate({ id: editing.id, input: form });
    else create.mutate(form);
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">{list.data?.count ?? 0} products</p>
        <Button onClick={openCreate}>
          <Plus className="size-4" />
          New product
        </Button>
      </div>

      {list.isPending ? (
        <LoadingState />
      ) : list.isError ? (
        <ErrorState
          title="Couldn't load products"
          description={String((list.error as Error).message)}
          onRetry={() => list.refetch()}
        />
      ) : (list.data?.items?.length ?? 0) === 0 ? (
        <EmptyState
          title="No products yet"
          description="Add the fuels and products this tenant sells before installing tanks and pumps."
          action={<Button onClick={openCreate}>Create one</Button>}
        />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-10" />
              <TableHead>Name</TableHead>
              <TableHead>Code</TableHead>
              <TableHead>Category</TableHead>
              <TableHead>Unit</TableHead>
              <TableHead className="text-right">Default price</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.data!.items.map((p) => (
              <TableRow key={p.id}>
                <TableCell>
                  <span
                    className="inline-block size-4 rounded-full border border-border"
                    style={{ backgroundColor: p.color }}
                    title={p.color}
                    aria-label={`${p.name} colour ${p.color}`}
                  />
                </TableCell>
                <TableCell className="font-medium">{p.name}</TableCell>
                <TableCell className="font-mono text-xs">{p.code}</TableCell>
                <TableCell className="text-muted-foreground capitalize">{p.category}</TableCell>
                <TableCell className="text-muted-foreground">{p.unit}</TableCell>
                <TableCell className="text-right tabular-nums">
                  {p.default_price.toFixed(2)}
                </TableCell>
                <TableCell>
                  <Badge tone={p.status === 'active' ? 'success' : 'warning'}>{p.status}</Badge>
                </TableCell>
                <TableCell className="text-right">
                  <Button variant="ghost" size="sm" onClick={() => openEdit(p)}>
                    Edit
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? 'Edit product' : 'New product'}</DialogTitle>
            <DialogDescription>
              {editing
                ? 'Update fields on this product.'
                : 'Add a fuel or product to the tenant catalogue.'}
            </DialogDescription>
          </DialogHeader>

          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              submit();
            }}
          >
            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="name">Name</Label>
                <Input
                  id="name"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  placeholder="PMS"
                  required
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="code">Code</Label>
                <Input
                  id="code"
                  value={form.code}
                  onChange={(e) => setForm({ ...form, code: e.target.value.toUpperCase() })}
                  placeholder="PMS"
                  required
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="category">Category</Label>
                <select
                  id="category"
                  className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                  value={form.category}
                  onChange={(e) => setForm({ ...form, category: e.target.value })}
                >
                  {categories.map((c) => (
                    <option key={c} value={c}>
                      {c}
                    </option>
                  ))}
                </select>
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="unit">Unit</Label>
                <select
                  id="unit"
                  className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                  value={form.unit}
                  onChange={(e) => setForm({ ...form, unit: e.target.value })}
                >
                  {units.map((u) => (
                    <option key={u} value={u}>
                      {u}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="default_price">Default price</Label>
                <Input
                  id="default_price"
                  type="number"
                  step="0.01"
                  min="0"
                  value={form.default_price}
                  onChange={(e) => setForm({ ...form, default_price: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="tax_rate">Tax rate (%)</Label>
                <Input
                  id="tax_rate"
                  type="number"
                  step="0.01"
                  min="0"
                  max="100"
                  value={form.tax_rate}
                  onChange={(e) => setForm({ ...form, tax_rate: e.target.value })}
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="density">Density (kg/m³)</Label>
                <Input
                  id="density"
                  type="number"
                  step="0.001"
                  min="0"
                  value={form.density_kg_m3}
                  onChange={(e) => setForm({ ...form, density_kg_m3: e.target.value })}
                  placeholder="optional"
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="loss_tol">Loss tolerance (%)</Label>
                <Input
                  id="loss_tol"
                  type="number"
                  step="0.01"
                  min="0"
                  value={form.loss_tolerance_percent}
                  onChange={(e) => setForm({ ...form, loss_tolerance_percent: e.target.value })}
                />
              </div>
            </div>

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="color">Colour</Label>
              <div className="flex items-center gap-3">
                <input
                  id="color"
                  type="color"
                  className="h-10 w-14 cursor-pointer rounded-md border border-border bg-background"
                  value={form.color}
                  onChange={(e) => setForm({ ...form, color: e.target.value })}
                />
                <Input
                  aria-label="Colour hex"
                  className="font-mono"
                  value={form.color}
                  onChange={(e) => setForm({ ...form, color: e.target.value })}
                  placeholder="#f97316"
                />
              </div>
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
