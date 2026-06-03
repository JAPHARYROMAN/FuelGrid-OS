'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';

import { SdkError, type Customer } from '@fuelgrid/sdk';
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
import { formatMoney } from '@/lib/money';
import { DocumentActions } from '@/components/document-actions';
import { PermissionGate } from '@/components/permission-gate';

interface FormState {
  code: string;
  name: string;
  contact_name: string;
  contact_email: string;
  contact_phone: string;
  credit_limit: string;
}

const blankForm: FormState = {
  code: '',
  name: '',
  contact_name: '',
  contact_email: '',
  contact_phone: '',
  credit_limit: '0',
};

export default function CustomersSettingsPage() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Customer | null>(null);
  const [form, setForm] = useState<FormState>(blankForm);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const customers = useQuery({
    queryKey: ['customers'],
    queryFn: ({ signal }) => api.listCustomers(signal),
  });

  const create = useMutation({
    mutationFn: (input: FormState) =>
      api.createCustomer({
        code: input.code.trim(),
        name: input.name.trim(),
        contact_name: input.contact_name.trim() || undefined,
        contact_email: input.contact_email.trim() || undefined,
        contact_phone: input.contact_phone.trim() || undefined,
        credit_limit: input.credit_limit.trim() || '0',
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['customers'] });
      setOpen(false);
      setForm(blankForm);
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  const update = useMutation({
    mutationFn: ({ id, input }: { id: string; input: FormState }) =>
      api.updateCustomer(id, {
        name: input.name.trim(),
        contact_name: input.contact_name.trim() || undefined,
        contact_email: input.contact_email.trim() || undefined,
        contact_phone: input.contact_phone.trim() || undefined,
        credit_limit: input.credit_limit.trim() || '0',
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['customers'] });
      setOpen(false);
      setEditing(null);
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  const setStatus = useMutation({
    mutationFn: ({ id, status }: { id: string; status: string }) =>
      api.setCustomerStatus(id, status),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['customers'] }),
  });

  function openCreate() {
    setEditing(null);
    setForm(blankForm);
    setSubmitError(null);
    setOpen(true);
  }

  function openEdit(c: Customer) {
    setEditing(c);
    setForm({
      code: c.code,
      name: c.name,
      contact_name: c.contact_name ?? '',
      contact_email: c.contact_email ?? '',
      contact_phone: c.contact_phone ?? '',
      credit_limit: c.credit_limit ?? '0',
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

  const items = customers.data?.items ?? [];

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Settings"
        title="Customers"
        description={`Credit and account customers shared across stations — ${customers.data?.count ?? items.length} total.`}
        actions={
          <div className="flex items-center gap-2">
            <DocumentActions
              onFetch={() => api.customersPdf()}
              filename="customers.pdf"
              permission="customer.read"
            />
            <PermissionGate permission="credit.manage">
              <Button onClick={openCreate}>
                <Plus className="size-4" />
                New customer
              </Button>
            </PermissionGate>
          </div>
        }
      />

      {customers.isPending ? (
        <Card>
          <CardContent className="flex flex-col gap-2 p-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-14 rounded-lg" />
            ))}
          </CardContent>
        </Card>
      ) : customers.isError ? (
        <ErrorState
          title="Couldn't load customers"
          description={String((customers.error as Error).message)}
          onRetry={() => customers.refetch()}
        />
      ) : items.length === 0 ? (
        <EmptyState
          title="No customers yet"
          description="Create customer accounts before issuing credit sales or invoices."
          action={
            <PermissionGate permission="credit.manage">
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
                  <TableHead>Contact</TableHead>
                  <TableHead className="text-right">Credit limit</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((c) => {
                  const suspended = c.status !== 'active';
                  return (
                    <TableRow key={c.id}>
                      <TableCell className="font-medium">{c.name}</TableCell>
                      <TableCell className="font-mono text-xs tabular-nums">{c.code}</TableCell>
                      <TableCell className="text-muted-foreground">
                        {c.contact_name || c.contact_email || c.contact_phone || '—'}
                      </TableCell>
                      <TableCell className="text-right font-mono tabular-nums">
                        {formatMoney(c.credit_limit)}
                      </TableCell>
                      <TableCell>
                        <Badge tone={c.status === 'active' ? 'success' : 'warning'}>
                          {c.status}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-2">
                          <PermissionGate permission="credit.manage">
                            <Button variant="ghost" size="sm" onClick={() => openEdit(c)}>
                              Edit
                            </Button>
                          </PermissionGate>
                          <PermissionGate permission="customer.manage">
                            <Button
                              variant="ghost"
                              size="sm"
                              disabled={setStatus.isPending}
                              onClick={() =>
                                setStatus.mutate({
                                  id: c.id,
                                  status: suspended ? 'active' : 'suspended',
                                })
                              }
                            >
                              {suspended ? 'Reactivate' : 'Suspend'}
                            </Button>
                          </PermissionGate>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? 'Edit customer' : 'New customer'}</DialogTitle>
            <DialogDescription>
              Customer accounts are shared across stations in this tenant.
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
                <Label htmlFor="customer-name">Name</Label>
                <Input
                  id="customer-name"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  required
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="customer-code">Code</Label>
                <Input
                  id="customer-code"
                  value={form.code}
                  onChange={(e) => setForm({ ...form, code: e.target.value.toUpperCase() })}
                  disabled={!!editing}
                  required
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="customer-contact">Contact name</Label>
                <Input
                  id="customer-contact"
                  value={form.contact_name}
                  onChange={(e) => setForm({ ...form, contact_name: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="customer-credit">Credit limit</Label>
                <Input
                  id="customer-credit"
                  inputMode="decimal"
                  value={form.credit_limit}
                  onChange={(e) => setForm({ ...form, credit_limit: e.target.value })}
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="customer-email">Email</Label>
                <Input
                  id="customer-email"
                  type="email"
                  value={form.contact_email}
                  onChange={(e) => setForm({ ...form, contact_email: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="customer-phone">Phone</Label>
                <Input
                  id="customer-phone"
                  value={form.contact_phone}
                  onChange={(e) => setForm({ ...form, contact_phone: e.target.value })}
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
                {create.isPending || update.isPending ? 'Saving...' : 'Save'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
