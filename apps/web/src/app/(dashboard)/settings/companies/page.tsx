'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';

import { SdkError, type Company } from '@fuelgrid/sdk';
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

interface CompanyFormState {
  name: string;
  legal_name: string;
  currency: string;
  timezone: string;
}

const blankForm: CompanyFormState = { name: '', legal_name: '', currency: 'USD', timezone: 'UTC' };

export default function CompaniesPage() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Company | null>(null);
  const [form, setForm] = useState<CompanyFormState>(blankForm);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const list = useQuery({
    queryKey: ['companies'],
    queryFn: ({ signal }) => api.listCompanies(signal),
  });

  const create = useMutation({
    mutationFn: (input: CompanyFormState) =>
      api.createCompany({
        name: input.name,
        legal_name: input.legal_name || undefined,
        currency: input.currency || undefined,
        timezone: input.timezone || undefined,
      } as Partial<Company> & { name: string }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['companies'] });
      setOpen(false);
      setForm(blankForm);
      setSubmitError(null);
    },
    onError: (err) => {
      setSubmitError(err instanceof SdkError ? err.message : 'Could not save');
    },
  });

  const update = useMutation({
    mutationFn: ({ id, input }: { id: string; input: CompanyFormState }) =>
      api.updateCompany(id, {
        name: input.name,
        legal_name: input.legal_name || undefined,
        currency: input.currency || undefined,
        timezone: input.timezone || undefined,
      } as Partial<Company>),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['companies'] });
      setEditing(null);
      setSubmitError(null);
    },
    onError: (err) => {
      setSubmitError(err instanceof SdkError ? err.message : 'Could not save');
    },
  });

  function openCreate() {
    setEditing(null);
    setForm(blankForm);
    setSubmitError(null);
    setOpen(true);
  }

  function openEdit(c: Company) {
    setEditing(c);
    setForm({
      name: c.name,
      legal_name: c.legal_name ?? '',
      currency: c.currency,
      timezone: c.timezone,
    });
    setSubmitError(null);
    setOpen(true);
  }

  function submit() {
    if (!form.name.trim()) {
      setSubmitError('Name is required');
      return;
    }
    if (editing) {
      update.mutate({ id: editing.id, input: form });
    } else {
      create.mutate(form);
    }
  }

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Settings"
        title="Companies"
        description={`The legal entities that own your stations — ${list.data?.count ?? 0} total.`}
        actions={
          <Button onClick={openCreate}>
            <Plus className="size-4" />
            New company
          </Button>
        }
      />

      {list.isPending ? (
        <Card>
          <CardContent className="flex flex-col gap-2 p-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-14 rounded-lg" />
            ))}
          </CardContent>
        </Card>
      ) : list.isError ? (
        <ErrorState
          title="Couldn't load companies"
          description={String((list.error as Error).message)}
          onRetry={() => list.refetch()}
        />
      ) : (list.data?.items?.length ?? 0) === 0 ? (
        <EmptyState
          title="No companies yet"
          description="A company is the legal entity that owns stations. Most tenants need at least one."
          action={<Button onClick={openCreate}>Create one</Button>}
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Legal name</TableHead>
                  <TableHead>Currency</TableHead>
                  <TableHead>Timezone</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.data!.items.map((c) => (
                  <TableRow key={c.id}>
                    <TableCell className="font-medium">{c.name}</TableCell>
                    <TableCell className="text-muted-foreground">{c.legal_name ?? '—'}</TableCell>
                    <TableCell className="font-mono text-xs tabular-nums">{c.currency}</TableCell>
                    <TableCell className="font-mono text-xs tabular-nums">{c.timezone}</TableCell>
                    <TableCell>
                      <Badge tone={c.status === 'active' ? 'success' : 'warning'}>{c.status}</Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <Button variant="ghost" size="sm" onClick={() => openEdit(c)}>
                        Edit
                      </Button>
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
            <DialogTitle>{editing ? 'Edit company' : 'New company'}</DialogTitle>
            <DialogDescription>
              {editing ? 'Update fields on this company.' : 'Add a legal entity to the tenant.'}
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
              <Label htmlFor="name">Name</Label>
              <Input
                id="name"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="legal_name">Legal name</Label>
              <Input
                id="legal_name"
                value={form.legal_name}
                onChange={(e) => setForm({ ...form, legal_name: e.target.value })}
              />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="currency">Currency</Label>
                <Input
                  id="currency"
                  value={form.currency}
                  onChange={(e) => setForm({ ...form, currency: e.target.value.toUpperCase() })}
                  maxLength={3}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="timezone">Timezone</Label>
                <Input
                  id="timezone"
                  value={form.timezone}
                  onChange={(e) => setForm({ ...form, timezone: e.target.value })}
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
