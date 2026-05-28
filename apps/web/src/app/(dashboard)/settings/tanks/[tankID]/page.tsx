'use client';

import { useRef, useState } from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft } from 'lucide-react';

import { SdkError, type CalibrationChart, type CalibrationPreview } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Label,
  LoadingState,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

function fmtDate(s?: string) {
  if (!s) return '—';
  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? s : d.toLocaleDateString();
}

export default function TankCalibrationPage() {
  const params = useParams<{ tankID: string }>();
  const tankID = params.tankID;
  const qc = useQueryClient();

  const fileRef = useRef<HTMLInputElement>(null);
  const [replaceOpen, setReplaceOpen] = useState(false);
  const [chartName, setChartName] = useState('');
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [preview, setPreview] = useState<CalibrationPreview | null>(null);

  const [dip, setDip] = useState('');

  const tank = useQuery({
    queryKey: ['tank', tankID],
    queryFn: ({ signal }) => api.getTank(tankID, signal),
  });

  const active = useQuery({
    queryKey: ['calibration-active', tankID],
    queryFn: async ({ signal }) => {
      try {
        return await api.activeCalibrationChart(tankID, signal);
      } catch (e) {
        if (e instanceof SdkError && e.status === 404) return null;
        throw e;
      }
    },
  });

  const charts = useQuery({
    queryKey: ['calibration-charts', tankID],
    queryFn: ({ signal }) => api.listCalibrationCharts(tankID, signal),
  });

  function selectedFile(): File | null {
    return fileRef.current?.files?.[0] ?? null;
  }

  const previewMut = useMutation({
    mutationFn: () => {
      const file = selectedFile();
      if (!file) throw new Error('Choose a CSV file first');
      return api.uploadCalibrationChart(tankID, {
        file,
        name: chartName || 'preview',
        dryRun: true,
      });
    },
    onSuccess: (res) => {
      setUploadError(null);
      setPreview('preview' in res ? res : null);
    },
    onError: (err) => {
      setPreview(null);
      setUploadError(
        err instanceof SdkError || err instanceof Error ? err.message : 'Preview failed',
      );
    },
  });

  const upload = useMutation({
    mutationFn: () => {
      const file = selectedFile();
      if (!file) throw new Error('Choose a CSV file first');
      if (!chartName.trim()) throw new Error('A chart name is required');
      return api.uploadCalibrationChart(tankID, { file, name: chartName.trim() });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['calibration-active', tankID] });
      qc.invalidateQueries({ queryKey: ['calibration-charts', tankID] });
      setReplaceOpen(false);
      setChartName('');
      setPreview(null);
      setUploadError(null);
      if (fileRef.current) fileRef.current.value = '';
    },
    onError: (err) =>
      setUploadError(
        err instanceof SdkError || err instanceof Error ? err.message : 'Upload failed',
      ),
  });

  const lookup = useMutation({
    mutationFn: (dipMM: number) => api.calibratedVolume(tankID, dipMM),
  });

  function runLookup() {
    const v = Number(dip);
    if (!dip || Number.isNaN(v)) return;
    lookup.mutate(v);
  }

  const activeChart: CalibrationChart | null | undefined = active.data;

  return (
    <div className="flex flex-col gap-5">
      <div>
        <Button variant="ghost" size="sm" asChild>
          <Link href="/settings/tanks">
            <ArrowLeft className="size-4" />
            Back to tanks
          </Link>
        </Button>
      </div>

      <header className="flex flex-col gap-1">
        <h2 className="text-xl font-semibold tracking-tight">
          {tank.data ? `${tank.data.name} (${tank.data.code})` : 'Tank'} — calibration
        </h2>
        <p className="text-sm text-muted-foreground">
          Strapping charts map dipstick millimetres to litres. Replacing a chart keeps the old one
          as history.
        </p>
      </header>

      {/* Active chart */}
      <Card>
        <CardHeader className="flex-row items-start justify-between gap-3 space-y-0">
          <div>
            <CardTitle>Active chart</CardTitle>
            <CardDescription>The chart used for dip-to-volume lookups.</CardDescription>
          </div>
          <Button
            onClick={() => {
              setUploadError(null);
              setPreview(null);
              setChartName('');
              setReplaceOpen(true);
            }}
          >
            Replace chart
          </Button>
        </CardHeader>
        <CardContent>
          {active.isPending ? (
            <LoadingState />
          ) : activeChart ? (
            <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-4">
              <div>
                <dt className="text-muted-foreground">Name</dt>
                <dd className="font-medium">{activeChart.name}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Points</dt>
                <dd className="tabular-nums">{activeChart.entry_count}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Effective from</dt>
                <dd>{fmtDate(activeChart.effective_from)}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Source</dt>
                <dd>{activeChart.source}</dd>
              </div>
            </dl>
          ) : (
            <p className="text-sm text-muted-foreground">
              No active chart yet. Use “Replace chart” to upload a strapping CSV.
            </p>
          )}
        </CardContent>
      </Card>

      {/* Dip → volume tester */}
      <Card>
        <CardHeader>
          <CardTitle>Dip → volume</CardTitle>
          <CardDescription>Interpolate a litre volume for a dip in millimetres.</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex items-end gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="dip">Dip (mm)</Label>
              <Input
                id="dip"
                type="number"
                min="0"
                value={dip}
                onChange={(e) => setDip(e.target.value)}
                className="w-40"
                onKeyDown={(e) => {
                  if (e.key === 'Enter') runLookup();
                }}
              />
            </div>
            <Button onClick={runLookup} disabled={!dip || lookup.isPending}>
              {lookup.isPending ? 'Looking up…' : 'Look up'}
            </Button>
          </div>
          {lookup.data ? (
            <p className="text-sm">
              <span className="text-muted-foreground">Volume: </span>
              <span className="font-semibold tabular-nums">
                {lookup.data.volume_litres.toLocaleString(undefined, {
                  maximumFractionDigits: 3,
                })}{' '}
                L
              </span>
            </p>
          ) : lookup.isError ? (
            <p className="text-sm text-danger" role="alert">
              {lookup.error instanceof SdkError ? lookup.error.message : 'Lookup failed'}
            </p>
          ) : null}
        </CardContent>
      </Card>

      {/* History */}
      <Card>
        <CardHeader>
          <CardTitle>History</CardTitle>
          <CardDescription>Every chart this tank has had.</CardDescription>
        </CardHeader>
        <CardContent>
          {charts.isPending ? (
            <LoadingState />
          ) : (charts.data?.items?.length ?? 0) === 0 ? (
            <p className="text-sm text-muted-foreground">No charts yet.</p>
          ) : (
            <div className="flex flex-col divide-y divide-border">
              {charts.data!.items.map((c) => (
                <div key={c.id} className="flex items-center justify-between gap-3 py-2 text-sm">
                  <div className="flex items-center gap-3">
                    <Badge tone={c.status === 'active' ? 'success' : 'neutral'}>{c.status}</Badge>
                    <span className="font-medium">{c.name}</span>
                    <span className="text-muted-foreground">{c.entry_count} pts</span>
                  </div>
                  <span className="text-muted-foreground">
                    {fmtDate(c.effective_from)}
                    {c.effective_until ? ` → ${fmtDate(c.effective_until)}` : ''}
                  </span>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Replace chart dialog */}
      <Dialog open={replaceOpen} onOpenChange={setReplaceOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Replace calibration chart</DialogTitle>
            <DialogDescription>
              Upload a CSV with header <code>dip_mm,volume_litres</code>. The current chart becomes
              history.
            </DialogDescription>
          </DialogHeader>
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              upload.mutate();
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="chart_name">Chart name</Label>
              <Input
                id="chart_name"
                value={chartName}
                onChange={(e) => setChartName(e.target.value)}
                placeholder="Re-strap 2026"
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="chart_file">CSV file</Label>
              <input
                id="chart_file"
                ref={fileRef}
                type="file"
                accept=".csv,text/csv"
                className="text-sm"
                onChange={() => setPreview(null)}
              />
            </div>

            {preview ? (
              <div className="rounded-md bg-muted px-3 py-2 text-sm">
                <p className="font-medium">Preview OK — {preview.entry_count} points</p>
                <p className="text-muted-foreground">
                  dip {preview.min_dip_mm}–{preview.max_dip_mm} mm · volume {preview.min_volume}–
                  {preview.max_volume} L
                </p>
              </div>
            ) : null}

            {uploadError ? (
              <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                {uploadError}
              </p>
            ) : null}

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => previewMut.mutate()}
                disabled={previewMut.isPending}
              >
                {previewMut.isPending ? 'Validating…' : 'Preview'}
              </Button>
              <Button type="submit" disabled={upload.isPending}>
                {upload.isPending ? 'Uploading…' : 'Upload & activate'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
