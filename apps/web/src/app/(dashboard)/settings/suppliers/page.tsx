'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';

import { SdkError, type Supplier } from '@fuelgrid/sdk';
import {
  Badge,
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

import { api } from '@/lib/api';
import { DocumentActions } from '@/components/document-actions';
import { PermissionGate } from '@/components/permission-gate';

interface FormState {
  code: string;
  name: string;
  contact_name: string;
  contact_email: string;
  contact_phone: string;
  payment_terms_days: string;
  product_ids: string[];
}

const blankForm: FormState = {
  code: '',
  name: '',
  contact_name: '',
  contact_email: '',
  contact_phone: '',
  payment_terms_days: '14',
  product_ids: [],
};

export default function SuppliersPage() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Supplier | null>(null);
  const [form, setForm] = useState<FormState>(blankForm);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const suppliers = useQuery({
    queryKey: ['suppliers'],
    queryFn: ({ signal }) => api.listSuppliers(signal),
  });
  const products = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
  });

  function payload(input: FormState) {
    return {
      code: input.code.trim(),
      name: input.name.trim(),
      contact_name: input.contact_name.trim() || undefined,
      contact_email: input.contact_email.trim() || undefined,
      contact_phone: input.contact_phone.trim() || undefined,
      payment_terms_days: Number(input.payment_terms_days) || 0,
      product_ids: input.product_ids,
    };
  }

  const create = useMutation({
    mutationFn: (input: FormState) => api.createSupplier(payload(input)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['suppliers'] });
      setOpen(false);
      setForm(blankForm);
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  const update = useMutation({
    mutationFn: ({ id, input }: { id: string; input: FormState }) =>
      api.updateSupplier(id, payload(input)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['suppliers'] });
      setOpen(false);
      setEditing(null);
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  const deactivate = useMutation({
    mutationFn: (id: string) => api.deactivateSupplier(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['suppliers'] }),
  });

  function openCreate() {
    setEditing(null);
    setForm(blankForm);
    setSubmitError(null);
    setOpen(true);
  }

  function openEdit(s: Supplier) {
    setEditing(s);
    setForm({
      code: s.code,
      name: s.name,
      contact_name: s.contact_name ?? '',
      contact_email: s.contact_email ?? '',
      contact_phone: s.contact_phone ?? '',
      payment_terms_days: String(s.payment_terms_days),
      product_ids: s.product_ids ?? [],
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
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Settings"
        title="Suppliers"
        description={`Supplier records shared across stations — ${suppliers.data?.count ?? 0} total.`}
        actions={
          <div className="flex items-center gap-2">
            <DocumentActions
              onFetch={() => api.suppliersPdf()}
              filename="suppliers.pdf"
              permission="purchase_order.read"
            />
            <PermissionGate permission="supplier.manage">
              <Button onClick={openCreate}>
                <Plus className="size-4" />
                New supplier
              </Button>
            </PermissionGate>
          </div>
        }
      />

      {suppliers.isPending ? (
        <Card>
          <CardContent className="flex flex-col gap-2 p-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-14 rounded-lg" />
            ))}
          </CardContent>
        </Card>
      ) : suppliers.isError ? (
        <ErrorState
          title="Couldn't load suppliers"
          description={String((suppliers.error as Error).message)}
          onRetry={() => suppliers.refetch()}
        />
      ) : (suppliers.data?.items?.length ?? 0) === 0 ? (
        <EmptyState
          title="No suppliers yet"
          description="Create supplier records before raising purchase orders."
          action={
            <PermissionGate permission="supplier.manage">
              <Button onClick={openCreate}>Create one</Button>
            </PermissionGate>
          }
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Code</TableHead>
                  <TableHead>Products</TableHead>
                  <TableHead className="text-right">Terms</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {suppliers.data!.items.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell className="font-medium">{s.name}</TableCell>
                    <TableCell className="font-mono text-xs tabular-nums">{s.code}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {s.product_ids.length} product{s.product_ids.length === 1 ? '' : 's'}
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums">
                      {s.payment_terms_days} days
                    </TableCell>
                    <TableCell>
                      <Badge tone={s.status === 'active' ? 'success' : 'warning'}>{s.status}</Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-2">
                        <PermissionGate permission="supplier.manage">
                          <Button variant="ghost" size="sm" onClick={() => openEdit(s)}>
                            Edit
                          </Button>
                        </PermissionGate>
                        {s.status !== 'deactivated' ? (
                          <PermissionGate permission="supplier.manage">
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => deactivate.mutate(s.id)}
                              disabled={deactivate.isPending}
                            >
                              Deactivate
                            </Button>
                          </PermissionGate>
                        ) : null}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? 'Edit supplier' : 'New supplier'}</DialogTitle>
            <DialogDescription>
              Supplier records are shared across stations in this tenant.
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
                <Label htmlFor="supplier-name">Name</Label>
                <Input
                  id="supplier-name"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  required
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="supplier-code">Code</Label>
                <Input
                  id="supplier-code"
                  value={form.code}
                  onChange={(e) => setForm({ ...form, code: e.target.value.toUpperCase() })}
                  required
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="supplier-contact">Contact</Label>
                <Input
                  id="supplier-contact"
                  value={form.contact_name}
                  onChange={(e) => setForm({ ...form, contact_name: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="supplier-terms">Payment terms</Label>
                <Input
                  id="supplier-terms"
                  type="number"
                  min="0"
                  value={form.payment_terms_days}
                  onChange={(e) => setForm({ ...form, payment_terms_days: e.target.value })}
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="supplier-email">Email</Label>
                <Input
                  id="supplier-email"
                  type="email"
                  value={form.contact_email}
                  onChange={(e) => setForm({ ...form, contact_email: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="supplier-phone">Phone</Label>
                <Input
                  id="supplier-phone"
                  value={form.contact_phone}
                  onChange={(e) => setForm({ ...form, contact_phone: e.target.value })}
                />
              </div>
            </div>

            <div className="flex flex-col gap-2">
              <Label>Products supplied</Label>
              <div className="grid gap-2 sm:grid-cols-2">
                {(products.data?.items ?? []).map((p) => {
                  const checked = form.product_ids.includes(p.id);
                  return (
                    <label
                      key={p.id}
                      className="flex items-center gap-2 rounded-md border border-border px-3 py-2 text-sm"
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={(e) =>
                          setForm({
                            ...form,
                            product_ids: e.target.checked
                              ? [...form.product_ids, p.id]
                              : form.product_ids.filter((id) => id !== p.id),
                          })
                        }
                      />
                      <span>{p.name}</span>
                    </label>
                  );
                })}
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
                {create.isPending || update.isPending ? 'Saving...' : 'Save'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
