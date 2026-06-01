'use client';

import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertTriangle, Plus } from 'lucide-react';

import { SdkError, type Incident } from '@fuelgrid/sdk';
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

const severities = ['low', 'medium', 'high', 'critical'];
const statuses = ['open', 'investigating', 'resolved', 'closed'];
const types = ['equipment', 'leak', 'variance', 'safety', 'calibration', 'other'];

function severityTone(s: string): 'neutral' | 'info' | 'warning' | 'danger' {
  switch (s) {
    case 'critical':
      return 'danger';
    case 'high':
      return 'warning';
    case 'medium':
      return 'info';
    default:
      return 'neutral';
  }
}

const nextStatus: Record<string, string | null> = {
  open: 'investigating',
  investigating: 'resolved',
  resolved: 'closed',
  closed: null,
};

interface FormState {
  station_id: string;
  type: string;
  severity: string;
  description: string;
}

export default function IncidentsPage() {
  const qc = useQueryClient();
  const [severityFilter, setSeverityFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState('open');
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<FormState>({
    station_id: '',
    type: 'other',
    severity: 'medium',
    description: '',
  });
  const [submitError, setSubmitError] = useState<string | null>(null);

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });

  const list = useQuery({
    queryKey: ['incidents', statusFilter, severityFilter],
    queryFn: ({ signal }) =>
      api.listIncidents(
        { status: statusFilter || undefined, severity: severityFilter || undefined },
        signal,
      ),
  });

  const stationLookup = useMemo(
    () => new Map((stations.data?.items ?? []).map((s) => [s.id, s])),
    [stations.data],
  );

  function invalidate() {
    qc.invalidateQueries({ queryKey: ['incidents'] });
  }

  const create = useMutation({
    mutationFn: (input: FormState) =>
      api.createIncident({
        station_id: input.station_id,
        type: input.type,
        severity: input.severity,
        description: input.description.trim(),
      }),
    onSuccess: () => {
      invalidate();
      setOpen(false);
      setSubmitError(null);
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  const transition = useMutation({
    mutationFn: ({ id, status }: { id: string; status: string }) =>
      api.updateIncidentStatus(id, status),
    onSuccess: invalidate,
  });

  function openCreate() {
    setForm({
      station_id: stations.data?.items[0]?.id ?? '',
      type: 'other',
      severity: 'medium',
      description: '',
    });
    setSubmitError(null);
    setOpen(true);
  }

  function submit() {
    if (!form.station_id || !form.description.trim()) {
      setSubmitError('Station and description are required');
      return;
    }
    create.mutate(form);
  }

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Monitor"
        title="Incidents"
        description="Operational issues raised against stations and their equipment."
        actions={
          <Button onClick={openCreate} disabled={(stations.data?.items?.length ?? 0) === 0}>
            <Plus className="size-4" />
            Open incident
          </Button>
        }
      />

      <div className="flex flex-wrap gap-3">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="statusFilter">Status</Label>
          <select
            id="statusFilter"
            className="h-9 rounded-md border border-border bg-background px-3 text-sm"
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value)}
          >
            <option value="">All</option>
            {statuses.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="severityFilter">Severity</Label>
          <select
            id="severityFilter"
            className="h-9 rounded-md border border-border bg-background px-3 text-sm"
            value={severityFilter}
            onChange={(e) => setSeverityFilter(e.target.value)}
          >
            <option value="">All</option>
            {severities.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </div>
      </div>

      {list.isPending ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-14 rounded-lg" />
          ))}
        </div>
      ) : list.isError ? (
        <ErrorState
          title="Couldn't load incidents"
          description={String((list.error as Error).message)}
          onRetry={() => list.refetch()}
        />
      ) : (list.data?.items?.length ?? 0) === 0 ? (
        <EmptyState
          title="No incidents"
          description="Nothing matches these filters. Open one if something needs attention."
          icon={<AlertTriangle />}
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Severity</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Station</TableHead>
                  <TableHead>Description</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Action</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.data!.items.map((inc: Incident) => {
                  const advance = nextStatus[inc.status];
                  const station = stationLookup.get(inc.station_id);
                  return (
                    <TableRow key={inc.id}>
                      <TableCell>
                        <Badge tone={severityTone(inc.severity)}>{inc.severity}</Badge>
                      </TableCell>
                      <TableCell className="capitalize text-muted-foreground">{inc.type}</TableCell>
                      <TableCell className="text-muted-foreground">
                        {station ? `${station.name} (${station.code})` : '—'}
                      </TableCell>
                      <TableCell className="max-w-md truncate">{inc.description}</TableCell>
                      <TableCell>
                        <Badge tone={inc.status === 'open' ? 'warning' : 'neutral'}>
                          {inc.status}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        {advance ? (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => transition.mutate({ id: inc.id, status: advance })}
                            disabled={transition.isPending}
                          >
                            Mark {advance}
                          </Button>
                        ) : (
                          <span className="text-xs text-muted-foreground">—</span>
                        )}
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
            <DialogTitle>Open incident</DialogTitle>
            <DialogDescription>Raise an issue against a station.</DialogDescription>
          </DialogHeader>
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              submit();
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="station">Station</Label>
              <select
                id="station"
                className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                value={form.station_id}
                onChange={(e) => setForm({ ...form, station_id: e.target.value })}
              >
                <option value="">Select…</option>
                {(stations.data?.items ?? []).map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name} ({s.code})
                  </option>
                ))}
              </select>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="type">Type</Label>
                <select
                  id="type"
                  className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                  value={form.type}
                  onChange={(e) => setForm({ ...form, type: e.target.value })}
                >
                  {types.map((t) => (
                    <option key={t} value={t}>
                      {t}
                    </option>
                  ))}
                </select>
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="severity">Severity</Label>
                <select
                  id="severity"
                  className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                  value={form.severity}
                  onChange={(e) => setForm({ ...form, severity: e.target.value })}
                >
                  {severities.map((s) => (
                    <option key={s} value={s}>
                      {s}
                    </option>
                  ))}
                </select>
              </div>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="description">Description</Label>
              <textarea
                id="description"
                className="min-h-20 rounded-md border border-border bg-background px-3 py-2 text-sm"
                value={form.description}
                onChange={(e) => setForm({ ...form, description: e.target.value })}
                placeholder="What happened?"
                required
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
              <Button type="submit" disabled={create.isPending}>
                {create.isPending ? 'Saving…' : 'Open incident'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
