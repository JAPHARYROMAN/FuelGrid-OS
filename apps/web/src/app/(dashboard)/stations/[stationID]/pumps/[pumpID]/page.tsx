'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft } from 'lucide-react';

import { SdkError } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
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

const pumpStatuses = ['active', 'inactive', 'maintenance', 'decommissioned'];
const calOutcomes = ['passed', 'failed', 'adjusted'];

function fmtTime(s?: string) {
  if (!s) return '—';
  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? s : d.toLocaleString();
}

function calTone(s: string): 'success' | 'danger' | 'info' {
  if (s === 'failed') return 'danger';
  if (s === 'adjusted') return 'info';
  return 'success';
}

export default function PumpDetailPage() {
  const params = useParams<{ stationID: string; pumpID: string }>();
  const { stationID, pumpID } = params;
  const qc = useQueryClient();

  const [calStatus, setCalStatus] = useState('passed');
  const [tolerance, setTolerance] = useState('');
  const [notes, setNotes] = useState('');
  const [calError, setCalError] = useState<string | null>(null);

  const [newStatus, setNewStatus] = useState('');
  const [reason, setReason] = useState('');
  const [statusError, setStatusError] = useState<string | null>(null);

  const pump = useQuery({
    queryKey: ['pump', pumpID],
    queryFn: ({ signal }) => api.getPump(pumpID, signal),
  });

  const calibrations = useQuery({
    queryKey: ['pump-calibrations', pumpID],
    queryFn: ({ signal }) => api.listPumpCalibrations(pumpID, signal),
  });

  const record = useMutation({
    mutationFn: () =>
      api.recordPumpCalibration(pumpID, {
        status: calStatus,
        tolerance_percent: tolerance ? Number(tolerance) : undefined,
        notes: notes.trim() || undefined,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['pump-calibrations', pumpID] });
      setTolerance('');
      setNotes('');
      setCalStatus('passed');
      setCalError(null);
    },
    onError: (err) => setCalError(err instanceof SdkError ? err.message : 'Could not record'),
  });

  const changeStatus = useMutation({
    mutationFn: () => api.updatePumpStatus(pumpID, newStatus, reason.trim() || undefined),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['pump', pumpID] });
      setNewStatus('');
      setReason('');
      setStatusError(null);
    },
    onError: (err) => setStatusError(err instanceof SdkError ? err.message : 'Could not update'),
  });

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow={
          <Link
            href="/settings/pumps"
            className="inline-flex items-center gap-1 hover:text-foreground"
          >
            <ArrowLeft className="size-3.5" />
            Back to pumps
          </Link>
        }
        title={pump.data ? `Pump ${pump.data.number}` : 'Pump'}
        description={pump.data?.name || undefined}
        actions={
          pump.data ? (
            <Badge tone={pump.data.status === 'active' ? 'success' : 'warning'}>
              {pump.data.status}
            </Badge>
          ) : null
        }
      />

      <div className="grid gap-5 lg:grid-cols-2">
        {/* Status toggle */}
        <Card>
          <CardHeader>
            <CardTitle>Status</CardTitle>
            <CardDescription>Transition the pump and capture a reason.</CardDescription>
          </CardHeader>
          <CardContent>
            <form
              className="flex flex-col gap-3"
              onSubmit={(e) => {
                e.preventDefault();
                if (!newStatus) {
                  setStatusError('Pick a status');
                  return;
                }
                changeStatus.mutate();
              }}
            >
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="status">New status</Label>
                <select
                  id="status"
                  className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                  value={newStatus}
                  onChange={(e) => setNewStatus(e.target.value)}
                >
                  <option value="">Select…</option>
                  {pumpStatuses
                    .filter((s) => s !== pump.data?.status)
                    .map((s) => (
                      <option key={s} value={s}>
                        {s}
                      </option>
                    ))}
                </select>
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="reason">Reason</Label>
                <Input
                  id="reason"
                  value={reason}
                  onChange={(e) => setReason(e.target.value)}
                  placeholder="Why is the status changing?"
                />
              </div>
              {statusError ? (
                <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                  {statusError}
                </p>
              ) : null}
              <div>
                <Button type="submit" disabled={changeStatus.isPending || !newStatus}>
                  {changeStatus.isPending ? 'Updating…' : 'Apply status'}
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>

        {/* Record calibration */}
        <Card>
          <CardHeader>
            <CardTitle>Record calibration</CardTitle>
            <CardDescription>Log a calibration event for this pump.</CardDescription>
          </CardHeader>
          <CardContent>
            <form
              className="flex flex-col gap-3"
              onSubmit={(e) => {
                e.preventDefault();
                record.mutate();
              }}
            >
              <div className="grid grid-cols-2 gap-3">
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="outcome">Outcome</Label>
                  <select
                    id="outcome"
                    className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                    value={calStatus}
                    onChange={(e) => setCalStatus(e.target.value)}
                  >
                    {calOutcomes.map((s) => (
                      <option key={s} value={s}>
                        {s}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="tolerance">Tolerance (%)</Label>
                  <Input
                    id="tolerance"
                    type="number"
                    step="0.01"
                    value={tolerance}
                    onChange={(e) => setTolerance(e.target.value)}
                    placeholder="0.50"
                  />
                </div>
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="notes">Notes</Label>
                <Input
                  id="notes"
                  value={notes}
                  onChange={(e) => setNotes(e.target.value)}
                  placeholder="Technician, meter readings…"
                />
              </div>
              {calError ? (
                <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                  {calError}
                </p>
              ) : null}
              <div>
                <Button type="submit" disabled={record.isPending}>
                  {record.isPending ? 'Recording…' : 'Record calibration'}
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>
      </div>

      {/* Calibration history */}
      <Card>
        <CardHeader>
          <CardTitle>Calibration history</CardTitle>
          <CardDescription>Every recorded calibration, newest first.</CardDescription>
        </CardHeader>
        <CardContent>
          {calibrations.isPending ? (
            <div className="flex flex-col gap-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-14 rounded-lg" />
              ))}
            </div>
          ) : (calibrations.data?.items?.length ?? 0) === 0 ? (
            <p className="text-sm text-muted-foreground">No calibrations recorded yet.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Performed at</TableHead>
                  <TableHead>Outcome</TableHead>
                  <TableHead className="text-right">Tolerance %</TableHead>
                  <TableHead>Notes</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {calibrations.data!.items.map((c) => (
                  <TableRow key={c.id}>
                    <TableCell>{fmtTime(c.performed_at)}</TableCell>
                    <TableCell>
                      <Badge tone={calTone(c.status)}>{c.status}</Badge>
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums">
                      {c.tolerance_percent != null ? c.tolerance_percent.toFixed(2) : '—'}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{c.notes ?? '—'}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <p className="text-xs text-muted-foreground">Station: {stationID}</p>
    </div>
  );
}
