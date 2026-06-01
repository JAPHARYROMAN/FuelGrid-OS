'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';

import { SdkError, type Region } from '@fuelgrid/sdk';
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

interface FormState {
  company_id: string;
  name: string;
  code: string;
}

const blankForm: FormState = { company_id: '', name: '', code: '' };

export default function RegionsPage() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Region | null>(null);
  const [form, setForm] = useState<FormState>(blankForm);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const companies = useQuery({
    queryKey: ['companies'],
    queryFn: ({ signal }) => api.listCompanies(signal),
  });

  const list = useQuery({
    queryKey: ['regions'],
    queryFn: ({ signal }) => api.listRegions({}, signal),
  });

  const create = useMutation({
    mutationFn: (input: FormState) =>
      api.createRegion({
        company_id: input.company_id,
        name: input.name,
        code: input.code || undefined,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['regions'] });
      setOpen(false);
      setForm(blankForm);
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  const update = useMutation({
    mutationFn: ({ id, input }: { id: string; input: FormState }) =>
      api.updateRegion(id, {
        name: input.name,
        code: input.code || undefined,
      } as Partial<Region>),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['regions'] });
      setEditing(null);
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  function openCreate() {
    setEditing(null);
    setForm({ ...blankForm, company_id: companies.data?.items[0]?.id ?? '' });
    setSubmitError(null);
    setOpen(true);
  }

  function openEdit(r: Region) {
    setEditing(r);
    setForm({ company_id: r.company_id, name: r.name, code: r.code ?? '' });
    setSubmitError(null);
    setOpen(true);
  }

  function submit() {
    if (!form.name.trim() || !form.company_id) {
      setSubmitError('Name and company are required');
      return;
    }
    if (editing) update.mutate({ id: editing.id, input: form });
    else create.mutate(form);
  }

  const companyLookup = new Map((companies.data?.items ?? []).map((c) => [c.id, c.name]));

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Settings"
        title="Regions"
        description={`Group stations under a company — ${list.data?.count ?? 0} total.`}
        actions={
          <Button onClick={openCreate} disabled={(companies.data?.items?.length ?? 0) === 0}>
            <Plus className="size-4" />
            New region
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
          title="Couldn't load regions"
          description={String((list.error as Error).message)}
          onRetry={() => list.refetch()}
        />
      ) : (list.data?.items?.length ?? 0) === 0 ? (
        <EmptyState
          title="No regions yet"
          description="Regions group stations under a company. Optional — small tenants can skip them."
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Company</TableHead>
                  <TableHead>Code</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.data!.items.map((r) => (
                  <TableRow key={r.id}>
                    <TableCell className="font-medium">{r.name}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {companyLookup.get(r.company_id) ?? r.company_id}
                    </TableCell>
                    <TableCell className="font-mono text-xs tabular-nums">
                      {r.code ?? '—'}
                    </TableCell>
                    <TableCell>
                      <Badge tone={r.status === 'active' ? 'success' : 'warning'}>{r.status}</Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <Button variant="ghost" size="sm" onClick={() => openEdit(r)}>
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
            <DialogTitle>{editing ? 'Edit region' : 'New region'}</DialogTitle>
            <DialogDescription>Regions group stations under a company.</DialogDescription>
          </DialogHeader>

          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              submit();
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="company">Company</Label>
              <select
                id="company"
                className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                value={form.company_id}
                onChange={(e) => setForm({ ...form, company_id: e.target.value })}
                disabled={Boolean(editing)}
              >
                <option value="">Select…</option>
                {(companies.data?.items ?? []).map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
            </div>
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
              <Label htmlFor="code">Code (optional)</Label>
              <Input
                id="code"
                value={form.code}
                onChange={(e) => setForm({ ...form, code: e.target.value })}
                placeholder="e.g. DAR"
              />
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
