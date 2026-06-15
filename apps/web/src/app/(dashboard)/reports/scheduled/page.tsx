'use client';

import * as React from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CalendarClock, Pencil, Play, Plus, Power, Trash2 } from 'lucide-react';

import {
  type ScheduledReport,
  type ScheduledReportChannel,
  type ScheduledReportRecipient,
  type ScheduledReportRequest,
  type ScheduleFrequency,
} from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  EmptyState,
  ErrorState,
  Input,
  Label,
  LoadingState,
  PageHeader,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { toast } from '@/lib/toast';
import { usePermission } from '@/hooks/use-permissions';

/**
 * Scheduled reports (Reports Center Phase 12 — blueprint §8). The real per-tenant
 * CRUD surface: a catalog report re-run on a recurrence and delivered to recipients
 * over a channel (in-app / email / webhook). Gated by reports.schedule; the backend
 * additionally re-checks the underlying report's own run permission on every write
 * AND at every delivery (a revoked recipient gets nothing). Replaces the old
 * read-only "digests" placeholder honestly.
 */

const selectClasses =
  'h-9 w-full rounded-md border border-border bg-background px-2.5 text-sm text-foreground ' +
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50';

// The report keys the scheduler can run (mirrors the backend reportSpecFor set).
// stationScoped reports require a station_id filter.
const REPORTS: { key: string; label: string; stationScoped: boolean }[] = [
  { key: 'station-close', label: 'Daily Station Close', stationScoped: true },
  { key: 'inventory-reconciliation', label: 'Inventory Reconciliation', stationScoped: true },
  { key: 'financials', label: 'Financial Statement', stationScoped: false },
  { key: 'ar-aging', label: 'Receivables Aging', stationScoped: false },
];

const FREQUENCIES: { value: ScheduleFrequency; label: string }[] = [
  { value: 'daily', label: 'Daily' },
  { value: 'weekly', label: 'Weekly' },
  { value: 'monthly', label: 'Monthly' },
  { value: 'cron', label: 'Custom (cron)' },
];

const CHANNELS: { value: ScheduledReportChannel; label: string }[] = [
  { value: 'in_app', label: 'In-app notification' },
  { value: 'email', label: 'Email' },
  { value: 'webhook', label: 'Webhook' },
];

const WEEKDAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

function reportLabel(key: string): string {
  return REPORTS.find((r) => r.key === key)?.label ?? key;
}

function isStationScoped(key: string): boolean {
  return REPORTS.find((r) => r.key === key)?.stationScoped ?? false;
}

/** Compact, human description of a recurrence for the list. */
function describeSchedule(s: ScheduledReport['schedule']): string {
  const hm = `${String(s.hour ?? 0).padStart(2, '0')}:${String(s.minute ?? 0).padStart(2, '0')}`;
  switch (s.frequency) {
    case 'daily':
      return `Daily at ${hm}`;
    case 'weekly':
      return `Weekly on ${WEEKDAYS[s.day_of_week ?? 0]} at ${hm}`;
    case 'monthly':
      return `Monthly on day ${s.day_of_month ?? 1} at ${hm}`;
    case 'cron':
      return `Cron: ${s.cron ?? ''}`;
    default:
      return s.frequency;
  }
}

function describeRecipients(rs: ScheduledReportRecipient[]): string {
  if (rs.length === 0) return '—';
  const users = rs.filter((r) => r.type === 'user').length;
  const emails = rs.filter((r) => r.type === 'email').length;
  const parts: string[] = [];
  if (users) parts.push(`${users} user${users > 1 ? 's' : ''}`);
  if (emails) parts.push(`${emails} email${emails > 1 ? 's' : ''}`);
  return parts.join(', ');
}

function statusTone(status: ScheduledReport['status']): 'success' | 'neutral' | 'danger' {
  if (status === 'active') return 'success';
  if (status === 'error') return 'danger';
  return 'neutral';
}

// The editable form state, kept as strings for the simple inputs and parsed on save.
interface FormState {
  report_key: string;
  name: string;
  station_id: string;
  period: string;
  frequency: ScheduleFrequency;
  hour: string;
  minute: string;
  day_of_week: string;
  day_of_month: string;
  cron: string;
  delivery_channel: ScheduledReportChannel;
  format: 'csv' | 'pdf' | 'xlsx';
  webhook_url: string;
  recipients: string; // newline/comma-separated user-ids or emails
}

function emptyForm(): FormState {
  return {
    report_key: 'station-close',
    name: '',
    station_id: '',
    period: '',
    frequency: 'daily',
    hour: '6',
    minute: '0',
    day_of_week: '1',
    day_of_month: '1',
    cron: '0 6 * * *',
    delivery_channel: 'in_app',
    format: 'csv',
    webhook_url: '',
    recipients: '',
  };
}

function formFromSchedule(s: ScheduledReport): FormState {
  return {
    report_key: s.report_key,
    name: s.name,
    station_id: s.filters.station_id ?? '',
    period: s.filters.period ?? '',
    frequency: s.schedule.frequency,
    hour: String(s.schedule.hour ?? 6),
    minute: String(s.schedule.minute ?? 0),
    day_of_week: String(s.schedule.day_of_week ?? 1),
    day_of_month: String(s.schedule.day_of_month ?? 1),
    cron: s.schedule.cron ?? '0 6 * * *',
    delivery_channel: s.delivery_channel,
    format: s.format,
    webhook_url: s.webhook_url ?? '',
    recipients: s.recipients.map((r) => r.value).join('\n'),
  };
}

/** Parse a form into the API request body, or throw a user-facing error. */
function buildRequest(f: FormState): ScheduledReportRequest {
  const filters: Record<string, string> = {};
  if (isStationScoped(f.report_key)) {
    if (!f.station_id.trim()) throw new Error('This report needs a station id.');
    filters.station_id = f.station_id.trim();
  }
  if (f.period.trim()) filters.period = f.period.trim();

  const schedule: ScheduledReportRequest['schedule'] = { frequency: f.frequency };
  if (f.frequency === 'cron') {
    schedule.cron = f.cron.trim();
  } else {
    schedule.hour = Number(f.hour);
    schedule.minute = Number(f.minute);
    if (f.frequency === 'weekly') schedule.day_of_week = Number(f.day_of_week);
    if (f.frequency === 'monthly') schedule.day_of_month = Number(f.day_of_month);
  }

  const recipients: ScheduledReportRecipient[] = f.recipients
    .split(/[\n,]/)
    .map((v) => v.trim())
    .filter(Boolean)
    .map((v) => (v.includes('@') ? { type: 'email', value: v } : { type: 'user', value: v }));

  if (f.delivery_channel !== 'webhook' && recipients.length === 0) {
    throw new Error('Add at least one recipient (a user id or an email).');
  }

  const req: ScheduledReportRequest = {
    report_key: f.report_key,
    name: f.name.trim(),
    filters,
    schedule,
    recipients,
    delivery_channel: f.delivery_channel,
    format: f.format,
  };
  if (f.delivery_channel === 'webhook') {
    if (!f.webhook_url.trim()) throw new Error('A webhook URL is required (https only).');
    req.webhook_url = f.webhook_url.trim();
  }
  return req;
}

function ScheduleDialog({
  open,
  onOpenChange,
  editing,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  editing: ScheduledReport | null;
}) {
  const qc = useQueryClient();
  const [form, setForm] = React.useState<FormState>(emptyForm);

  React.useEffect(() => {
    if (open) setForm(editing ? formFromSchedule(editing) : emptyForm());
  }, [open, editing]);

  const save = useMutation({
    mutationFn: (req: ScheduledReportRequest) =>
      editing ? api.updateScheduledReport(editing.id, req) : api.createScheduledReport(req),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['scheduled-reports'] });
      void qc.invalidateQueries({ queryKey: ['reports-hub', 'scheduled'] });
      toast.success(editing ? 'Schedule updated' : 'Schedule created');
      onOpenChange(false);
    },
    onError: (e: Error) => toast.error(e.message || 'Could not save the schedule'),
  });

  function set<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((f) => ({ ...f, [key]: value }));
  }

  function submit() {
    let req: ScheduledReportRequest;
    try {
      req = buildRequest(form);
    } catch (e) {
      toast.error((e as Error).message);
      return;
    }
    save.mutate(req);
  }

  const stationScoped = isStationScoped(form.report_key);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? 'Edit scheduled report' : 'New scheduled report'}</DialogTitle>
        </DialogHeader>

        <div className="flex flex-col gap-4 py-2">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="sr-name">Name</Label>
            <Input
              id="sr-name"
              value={form.name}
              onChange={(e) => set('name', e.target.value)}
              placeholder="e.g. Daily close — Mikocheni"
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="sr-report">Report</Label>
            <select
              id="sr-report"
              className={selectClasses}
              value={form.report_key}
              disabled={!!editing}
              onChange={(e) => set('report_key', e.target.value)}
            >
              {REPORTS.map((r) => (
                <option key={r.key} value={r.key}>
                  {r.label}
                </option>
              ))}
            </select>
            {editing ? (
              <p className="text-xs text-muted-foreground">
                The report can&apos;t be changed; create a new schedule instead.
              </p>
            ) : null}
          </div>

          {stationScoped ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="sr-station">Station id</Label>
              <Input
                id="sr-station"
                value={form.station_id}
                onChange={(e) => set('station_id', e.target.value)}
                placeholder="station UUID"
              />
            </div>
          ) : null}

          <div className="grid grid-cols-2 gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="sr-freq">Frequency</Label>
              <select
                id="sr-freq"
                className={selectClasses}
                value={form.frequency}
                onChange={(e) => set('frequency', e.target.value as ScheduleFrequency)}
              >
                {FREQUENCIES.map((f) => (
                  <option key={f.value} value={f.value}>
                    {f.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="sr-format">Format</Label>
              <select
                id="sr-format"
                className={selectClasses}
                value={form.format}
                onChange={(e) => set('format', e.target.value as FormState['format'])}
              >
                <option value="csv">CSV</option>
                <option value="pdf">PDF</option>
                <option value="xlsx">Excel</option>
              </select>
            </div>
          </div>

          {form.frequency === 'cron' ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="sr-cron">Cron (min hour dom mon dow)</Label>
              <Input
                id="sr-cron"
                value={form.cron}
                onChange={(e) => set('cron', e.target.value)}
                placeholder="0 6 * * 1"
              />
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="sr-hour">Hour (0–23)</Label>
                <Input
                  id="sr-hour"
                  type="number"
                  min={0}
                  max={23}
                  value={form.hour}
                  onChange={(e) => set('hour', e.target.value)}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="sr-minute">Minute (0–59)</Label>
                <Input
                  id="sr-minute"
                  type="number"
                  min={0}
                  max={59}
                  value={form.minute}
                  onChange={(e) => set('minute', e.target.value)}
                />
              </div>
            </div>
          )}

          {form.frequency === 'weekly' ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="sr-dow">Day of week</Label>
              <select
                id="sr-dow"
                className={selectClasses}
                value={form.day_of_week}
                onChange={(e) => set('day_of_week', e.target.value)}
              >
                {WEEKDAYS.map((d, i) => (
                  <option key={d} value={String(i)}>
                    {d}
                  </option>
                ))}
              </select>
            </div>
          ) : null}

          {form.frequency === 'monthly' ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="sr-dom">Day of month (1–31)</Label>
              <Input
                id="sr-dom"
                type="number"
                min={1}
                max={31}
                value={form.day_of_month}
                onChange={(e) => set('day_of_month', e.target.value)}
              />
            </div>
          ) : null}

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="sr-channel">Delivery channel</Label>
            <select
              id="sr-channel"
              className={selectClasses}
              value={form.delivery_channel}
              onChange={(e) => set('delivery_channel', e.target.value as ScheduledReportChannel)}
            >
              {CHANNELS.map((c) => (
                <option key={c.value} value={c.value}>
                  {c.label}
                </option>
              ))}
            </select>
          </div>

          {form.delivery_channel === 'webhook' ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="sr-webhook">Webhook URL (https only)</Label>
              <Input
                id="sr-webhook"
                value={form.webhook_url}
                onChange={(e) => set('webhook_url', e.target.value)}
                placeholder="https://hooks.example.com/…"
              />
              <p className="text-xs text-muted-foreground">
                Private, loopback and link-local hosts are rejected (SSRF protection).
              </p>
            </div>
          ) : (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="sr-recipients">Recipients (one per line)</Label>
              <textarea
                id="sr-recipients"
                className={`${selectClasses} h-20 py-2`}
                value={form.recipients}
                onChange={(e) => set('recipients', e.target.value)}
                placeholder={'user UUID\nops@example.com'}
              />
              <p className="text-xs text-muted-foreground">
                A user id is permission-checked individually at every delivery; an email is anchored
                on you.{' '}
                {form.delivery_channel === 'in_app' ? 'In-app delivery needs user ids.' : ''}
              </p>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={save.isPending || !form.name.trim()}>
            {save.isPending ? 'Saving…' : editing ? 'Save changes' : 'Create schedule'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function RunsPanel({ schedule }: { schedule: ScheduledReport }) {
  const runs = useQuery({
    queryKey: ['scheduled-reports', schedule.id, 'runs'],
    queryFn: ({ signal }) => api.listScheduledReportRuns(schedule.id, { limit: 5 }, signal),
    retry: false,
  });
  const items = runs.data?.items ?? [];
  if (runs.isPending) return <LoadingState title="Loading runs…" />;
  if (items.length === 0) {
    return (
      <p className="px-1 py-2 text-xs text-muted-foreground">
        No runs yet. Use “Run now” or wait for the next scheduled time.
      </p>
    );
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs">
        <thead>
          <tr className="border-b border-border text-left uppercase tracking-wide text-muted-foreground">
            <th className="py-1.5 pr-3 font-medium">When</th>
            <th className="py-1.5 pr-3 font-medium">Period</th>
            <th className="py-1.5 pr-3 font-medium">Status</th>
            <th className="py-1.5 pr-3 font-medium">Delivered</th>
            <th className="py-1.5 pr-3 font-medium">Skipped</th>
          </tr>
        </thead>
        <tbody>
          {items.map((r) => (
            <tr key={r.id} className="border-b border-border/60 last:border-0">
              <td className="py-1.5 pr-3">{new Date(r.run_at).toLocaleString()}</td>
              <td className="py-1.5 pr-3 text-muted-foreground">{r.period_key}</td>
              <td className="py-1.5 pr-3">
                <Badge
                  tone={
                    r.status === 'success'
                      ? 'success'
                      : r.status === 'failed'
                        ? 'danger'
                        : 'neutral'
                  }
                >
                  {r.status}
                </Badge>
              </td>
              <td className="py-1.5 pr-3">{r.delivered_count}</td>
              <td className="py-1.5 pr-3">{r.skipped_count}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ScheduleRow({
  schedule,
  onEdit,
}: {
  schedule: ScheduledReport;
  onEdit: (s: ScheduledReport) => void;
}) {
  const qc = useQueryClient();
  const [showRuns, setShowRuns] = React.useState(false);

  function invalidate() {
    void qc.invalidateQueries({ queryKey: ['scheduled-reports'] });
    void qc.invalidateQueries({ queryKey: ['reports-hub', 'scheduled'] });
  }

  const toggle = useMutation({
    mutationFn: () => api.setScheduledReportEnabled(schedule.id, !schedule.enabled),
    onSuccess: () => {
      invalidate();
      toast.success(schedule.enabled ? 'Schedule paused' : 'Schedule enabled');
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const runNow = useMutation({
    mutationFn: () => api.runScheduledReportNow(schedule.id),
    onSuccess: (res) => {
      void qc.invalidateQueries({ queryKey: ['scheduled-reports', schedule.id, 'runs'] });
      toast.success(
        `Run ${res.status} — delivered ${res.delivered_count}, skipped ${res.skipped_count}`,
      );
      setShowRuns(true);
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const remove = useMutation({
    mutationFn: () => api.deleteScheduledReport(schedule.id),
    onSuccess: () => {
      invalidate();
      toast.success('Schedule deleted');
    },
    onError: (e: Error) => toast.error(e.message),
  });

  return (
    <>
      <tr className="border-b border-border/60 last:border-0">
        <td className="py-2 pr-4">
          <button
            className="text-left font-medium text-foreground hover:underline"
            onClick={() => setShowRuns((v) => !v)}
          >
            {schedule.name}
          </button>
          <div className="text-xs text-muted-foreground">{reportLabel(schedule.report_key)}</div>
        </td>
        <td className="py-2 pr-4 text-muted-foreground">{describeSchedule(schedule.schedule)}</td>
        <td className="py-2 pr-4">
          <span className="capitalize">{schedule.delivery_channel.replace('_', '-')}</span>
          <div className="text-xs text-muted-foreground">
            {describeRecipients(schedule.recipients)}
          </div>
        </td>
        <td className="py-2 pr-4 text-muted-foreground">
          {new Date(schedule.next_run_at).toLocaleString()}
        </td>
        <td className="py-2 pr-4">
          <Badge tone={statusTone(schedule.status)}>
            {schedule.enabled ? schedule.status : 'paused'}
          </Badge>
        </td>
        <td className="py-2 pr-0 text-right">
          <div className="flex items-center justify-end gap-1">
            <Button
              size="sm"
              variant="ghost"
              title="Run now"
              disabled={runNow.isPending}
              onClick={() => runNow.mutate()}
            >
              <Play className="size-4" />
            </Button>
            <Button
              size="sm"
              variant="ghost"
              title={schedule.enabled ? 'Pause' : 'Enable'}
              disabled={toggle.isPending}
              onClick={() => toggle.mutate()}
            >
              <Power className="size-4" />
            </Button>
            <Button size="sm" variant="ghost" title="Edit" onClick={() => onEdit(schedule)}>
              <Pencil className="size-4" />
            </Button>
            <Button
              size="sm"
              variant="ghost"
              title="Delete"
              disabled={remove.isPending}
              onClick={() => {
                if (confirm(`Delete schedule “${schedule.name}”?`)) remove.mutate();
              }}
            >
              <Trash2 className="size-4" />
            </Button>
          </div>
        </td>
      </tr>
      {showRuns ? (
        <tr>
          <td colSpan={6} className="bg-muted/30 px-4 py-2">
            <RunsPanel schedule={schedule} />
          </td>
        </tr>
      ) : null}
    </>
  );
}

export default function ScheduledReportsPage() {
  const canManage = usePermission('reports.schedule');
  const [dialogOpen, setDialogOpen] = React.useState(false);
  const [editing, setEditing] = React.useState<ScheduledReport | null>(null);

  const list = useQuery({
    queryKey: ['scheduled-reports'],
    queryFn: ({ signal }) => api.listScheduledReports({ limit: 50 }, signal),
    enabled: canManage === true,
    retry: false,
  });

  function openCreate() {
    setEditing(null);
    setDialogOpen(true);
  }
  function openEdit(s: ScheduledReport) {
    setEditing(s);
    setDialogOpen(true);
  }

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Scheduled"
        title="Scheduled reports"
        description="Schedule a report to re-run on a recurrence and deliver it by in-app notification, email or webhook. Permissions are re-checked at every delivery, so a recipient who loses access receives nothing."
        actions={
          canManage ? (
            <Button onClick={openCreate}>
              <Plus className="size-4" />
              New schedule
            </Button>
          ) : undefined
        }
      />

      {canManage === false ? (
        <EmptyState
          title="No access"
          description="You don't have permission to manage scheduled reports."
        />
      ) : canManage === null || list.isPending ? (
        <LoadingState title="Loading scheduled reports…" />
      ) : list.isError ? (
        <ErrorState
          title="Couldn't load scheduled reports"
          description={String((list.error as Error).message)}
          onRetry={() => list.refetch()}
        />
      ) : (list.data?.items ?? []).length === 0 ? (
        <EmptyState
          icon={<CalendarClock className="size-6" />}
          title="No scheduled reports yet"
          description="Create your first schedule to have a report delivered automatically on a recurrence."
          action={
            <Button onClick={openCreate}>
              <Plus className="size-4" />
              New schedule
            </Button>
          }
        />
      ) : (
        <Card>
          <CardHeader>
            <CardTitle>Schedules</CardTitle>
          </CardHeader>
          <CardContent className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs uppercase tracking-wide text-muted-foreground">
                  <th className="py-2 pr-4 font-medium">Name</th>
                  <th className="py-2 pr-4 font-medium">Recurrence</th>
                  <th className="py-2 pr-4 font-medium">Delivery</th>
                  <th className="py-2 pr-4 font-medium">Next run</th>
                  <th className="py-2 pr-4 font-medium">Status</th>
                  <th className="py-2 pr-0 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {(list.data?.items ?? []).map((s) => (
                  <ScheduleRow key={s.id} schedule={s} onEdit={openEdit} />
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}

      <ScheduleDialog open={dialogOpen} onOpenChange={setDialogOpen} editing={editing} />
    </div>
  );
}
