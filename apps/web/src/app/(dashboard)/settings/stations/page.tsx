'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';

import { SdkError, type Station } from '@fuelgrid/sdk';
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
import { PermissionGate } from '@/components/permission-gate';

interface FormState {
  company_id: string;
  region_id: string;
  name: string;
  code: string;
  city: string;
  country: string;
}

const blankForm: FormState = {
  company_id: '',
  region_id: '',
  name: '',
  code: '',
  city: '',
  country: '',
};

export default function StationsPage() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Station | null>(null);
  const [form, setForm] = useState<FormState>(blankForm);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const companies = useQuery({
    queryKey: ['companies'],
    queryFn: ({ signal }) => api.listCompanies(signal),
  });
  const regions = useQuery({
    queryKey: ['regions'],
    queryFn: ({ signal }) => api.listRegions({}, signal),
  });
  const list = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });

  const create = useMutation({
    mutationFn: (input: FormState) =>
      api.createStation({
        company_id: input.company_id,
        region_id: input.region_id || undefined,
        name: input.name,
        code: input.code,
        city: input.city || undefined,
        country: input.country || undefined,
      } as Partial<Station> & { company_id: string; name: string; code: string }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['stations'] });
      setOpen(false);
      setForm(blankForm);
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  const update = useMutation({
    mutationFn: ({ id, input }: { id: string; input: FormState }) =>
      api.updateStation(id, {
        region_id: input.region_id || undefined,
        name: input.name,
        code: input.code,
        city: input.city || undefined,
        country: input.country || undefined,
      } as Partial<Station>),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['stations'] });
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

  function openEdit(s: Station) {
    setEditing(s);
    setForm({
      company_id: s.company_id,
      region_id: s.region_id ?? '',
      name: s.name,
      code: s.code,
      city: s.city ?? '',
      country: s.country ?? '',
    });
    setSubmitError(null);
    setOpen(true);
  }

  function submit() {
    if (!form.name.trim() || !form.code.trim() || !form.company_id) {
      setSubmitError('Company, name, and code are required');
      return;
    }
    if (editing) update.mutate({ id: editing.id, input: form });
    else create.mutate(form);
  }

  const regionLookup = new Map((regions.data?.items ?? []).map((r) => [r.id, r.name]));

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Settings"
        title="Stations"
        description={`Fueling locations under your companies — ${list.data?.count ?? 0} total.`}
        actions={
          <PermissionGate permission="station.manage">
            <Button onClick={openCreate} disabled={(companies.data?.items?.length ?? 0) === 0}>
              <Plus className="size-4" />
              New station
            </Button>
          </PermissionGate>
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
          title="Couldn't load stations"
          description={String((list.error as Error).message)}
          onRetry={() => list.refetch()}
        />
      ) : (list.data?.items?.length ?? 0) === 0 ? (
        <EmptyState
          title="No stations yet"
          description="At least one station is required before Phase 2 can install tanks and pumps."
          action={
            <PermissionGate permission="station.manage" mode="hide">
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
                  <TableHead>Region</TableHead>
                  <TableHead>City / Country</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.data!.items.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell className="font-medium">{s.name}</TableCell>
                    <TableCell className="font-mono text-xs tabular-nums">{s.code}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {s.region_id ? (regionLookup.get(s.region_id) ?? '—') : '—'}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {[s.city, s.country].filter(Boolean).join(', ') || '—'}
                    </TableCell>
                    <TableCell>
                      <Badge tone={s.status === 'active' ? 'success' : 'warning'}>{s.status}</Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <PermissionGate permission="station.manage">
                        <Button variant="ghost" size="sm" onClick={() => openEdit(s)}>
                          Edit
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

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? 'Edit station' : 'New station'}</DialogTitle>
            <DialogDescription>
              {editing ? 'Update fields on this station.' : 'Add a fueling location to a company.'}
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
              <Label htmlFor="region">Region (optional)</Label>
              <select
                id="region"
                className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                value={form.region_id}
                onChange={(e) => setForm({ ...form, region_id: e.target.value })}
              >
                <option value="">None</option>
                {(regions.data?.items ?? [])
                  .filter((r) => !form.company_id || r.company_id === form.company_id)
                  .map((r) => (
                    <option key={r.id} value={r.id}>
                      {r.name}
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
                  required
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="code">Code</Label>
                <Input
                  id="code"
                  value={form.code}
                  onChange={(e) => setForm({ ...form, code: e.target.value.toUpperCase() })}
                  placeholder="MIK-01"
                  required
                />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="city">City</Label>
                <Input
                  id="city"
                  value={form.city}
                  onChange={(e) => setForm({ ...form, city: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="country">Country</Label>
                <Input
                  id="country"
                  value={form.country}
                  onChange={(e) => setForm({ ...form, country: e.target.value })}
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
