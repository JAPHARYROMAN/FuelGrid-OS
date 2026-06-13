'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useMutation } from '@tanstack/react-query';
import {
  AlertTriangle,
  ArrowLeft,
  Check,
  CreditCard,
  Fuel,
  Gauge,
  Loader2,
  MoreHorizontal,
  Shield,
  Wrench,
} from 'lucide-react';

import { SdkError, type IncidentReportType } from '@fuelgrid/sdk';
import { Button, Card, CardContent, CardHeader, CardTitle } from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { useT, type Messages } from '@/lib/i18n';
import { toast } from '@/lib/toast';
import { getSyncEngine, isOfflineError, useAttendantSnapshot } from '@/lib/offline';

/** A symbol the mutation returns when the report was queued offline. */
const QUEUED = Symbol('queued-offline');

/**
 * The PRD §6.12 issue types, in pick order, each mapped to its API enum value.
 * Labels/hints come from the dictionary (en+sw) at render; large touch targets
 * with an icon AND text label (PRD §15.1 — never icon/colour alone).
 */
const ISSUE_TYPES: { value: IncidentReportType; icon: React.ReactNode }[] = [
  { value: 'pump', icon: <Fuel className="size-6" aria-hidden /> },
  { value: 'nozzle', icon: <Wrench className="size-6" aria-hidden /> },
  { value: 'meter', icon: <Gauge className="size-6" aria-hidden /> },
  { value: 'payment', icon: <CreditCard className="size-6" aria-hidden /> },
  { value: 'safety', icon: <Shield className="size-6" aria-hidden /> },
  { value: 'other', icon: <MoreHorizontal className="size-6" aria-hidden /> },
];

/** Urgency → the incident severity the backend understands. */
type Urgency = 'low' | 'medium' | 'high';
const URGENCY_TO_SEVERITY: Record<Urgency, string> = {
  low: 'low',
  medium: 'medium',
  high: 'high',
};

export default function ReportIssuePage() {
  const t = useT();
  const snapshot = useAttendantSnapshot();
  const shiftID = snapshot.data?.shift?.id ?? '';

  const [type, setType] = useState<IncidentReportType | null>(null);
  const [urgency, setUrgency] = useState<Urgency>('medium');
  const [description, setDescription] = useState('');
  const [confirming, setConfirming] = useState(false);
  const [sent, setSent] = useState<false | 'sent' | 'queued'>(false);
  const [showErrors, setShowErrors] = useState(false);

  // One dedupe_key per submission ATTEMPT, generated when the attendant moves
  // to the confirm step and reused across retries (online and offline). A new
  // attempt (e.g. "report another") gets a fresh key — see resetForm.
  const [dedupeKey, setDedupeKey] = useState(() => newUuid());

  const submit = useMutation({
    mutationFn: async () => {
      if (!type) throw new Error('no type'); // guarded by the UI
      const req = {
        type,
        description: description.trim(),
        severity: URGENCY_TO_SEVERITY[urgency],
        dedupe_key: dedupeKey,
      };
      try {
        // The station is derived server-side from the actor's current shift —
        // we deliberately never send station_id.
        return await api.reportIncident(req);
      } catch (e) {
        if (isOfflineError(e)) {
          // Queue with the SAME dedupe_key so the replay returns the original
          // incident (200) rather than creating a duplicate (201).
          await getSyncEngine().enqueue({
            action_type: 'report_issue',
            shift_id: shiftID,
            payload: {
              type,
              description: description.trim(),
              severity: URGENCY_TO_SEVERITY[urgency],
              dedupe_key: dedupeKey,
            },
          });
          return QUEUED;
        }
        throw e;
      }
    },
    onSuccess: (result) => {
      setConfirming(false);
      if (result === QUEUED) {
        setSent('queued');
        toast.success(t.report.toastQueuedTitle, t.report.toastQueuedBody);
      } else {
        setSent('sent');
        toast.success(t.report.toastSentTitle, t.report.toastSentBody);
      }
    },
    onError: (e) => {
      setConfirming(false);
      // 409 no_active_shift is surfaced as its own message; everything else is
      // the server's prose (verbatim) or the generic fallback.
      if (e instanceof SdkError && e.status === 409 && errorCode(e) === 'no_active_shift') {
        toast.error(t.report.errNoShiftTitle, t.report.errNoShiftBody);
        return;
      }
      toast.error(t.report.errGeneric, e instanceof SdkError ? e.message : undefined);
    },
  });

  const descriptionMissing = showErrors && description.trim() === '';
  const typeMissing = showErrors && type == null;

  function startConfirm() {
    setShowErrors(true);
    if (type == null || description.trim() === '') return;
    setConfirming(true);
  }

  function resetForm() {
    setType(null);
    setUrgency('medium');
    setDescription('');
    setConfirming(false);
    setSent(false);
    setShowErrors(false);
    setDedupeKey(newUuid());
  }

  // ----- No active shift: report-issue is shift-linked -----
  // (Belt-and-braces with the server's 409; the screen shows the honest state
  // even before any submit attempt.)
  if (snapshot.data && !snapshot.data.shift) {
    return (
      <div className="flex flex-col gap-4">
        <BackHome />
        <Card>
          <CardContent className="flex flex-col items-center gap-3 pt-6 text-center">
            <span className="flex size-12 items-center justify-center rounded-full bg-warning/15 text-warning">
              <AlertTriangle className="size-6" aria-hidden />
            </span>
            <p className="text-lg font-semibold" role="status">
              {t.report.errNoShiftTitle}
            </p>
            <p className="text-base text-muted-foreground">{t.report.errNoShiftBody}</p>
            <Button asChild className="h-14 w-full text-lg">
              <Link href="/attendant">{t.report.backHome}</Link>
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  // ----- Sent / queued success state -----
  if (sent) {
    return (
      <div className="flex flex-col gap-4">
        <BackHome />
        <Card>
          <CardContent className="flex flex-col items-center gap-3 pt-6 text-center">
            <span
              className={
                'flex size-12 items-center justify-center rounded-full ' +
                (sent === 'queued' ? 'bg-warning/15 text-warning' : 'bg-success/15 text-success')
              }
            >
              <Check className="size-6" aria-hidden />
            </span>
            <p className="text-lg font-semibold" role="status">
              {sent === 'queued' ? t.report.queuedTitle : t.report.sentTitle}
            </p>
            <p className="text-base text-muted-foreground">
              {sent === 'queued' ? t.report.queuedBody : t.report.sentBody}
            </p>
            <Button className="h-14 w-full text-lg" onClick={resetForm}>
              {t.report.reportAnother}
            </Button>
            <Button asChild variant="outline" className="h-12 w-full text-base">
              <Link href="/attendant">{t.report.backHome}</Link>
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <BackHome />

      <div>
        <h1 className="text-xl font-semibold leading-tight">{t.report.title}</h1>
        <p className="text-base text-muted-foreground">{t.report.subtitle}</p>
      </div>

      {/* Issue-type picker — large targets, icon + text label */}
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1 text-base font-medium">{t.report.typeLabel}</legend>
        <div className="grid grid-cols-2 gap-2.5">
          {ISSUE_TYPES.map(({ value, icon }) => {
            const selected = type === value;
            return (
              <button
                key={value}
                type="button"
                onClick={() => {
                  setType(value);
                  setShowErrors(false);
                }}
                aria-pressed={selected}
                className={
                  'flex min-h-20 flex-col items-center justify-center gap-1.5 rounded-xl border p-3 text-center transition-colors ' +
                  (selected
                    ? 'border-accent bg-accent/10 text-accent'
                    : 'border-border bg-card text-foreground hover:bg-accent/5')
                }
              >
                {icon}
                <span className="text-base font-medium">{t.report.types[value]}</span>
              </button>
            );
          })}
        </div>
        {typeMissing ? (
          <p className="text-sm text-warning" role="status">
            {t.report.typeMissing}
          </p>
        ) : type ? (
          <p className="text-sm text-muted-foreground">{t.report.typeHints[type]}</p>
        ) : null}
      </fieldset>

      {/* Urgency */}
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1 text-base font-medium">{t.report.urgencyLabel}</legend>
        <div className="grid grid-cols-3 gap-2" role="group">
          {(
            [
              ['low', t.report.urgencyLow],
              ['medium', t.report.urgencyMedium],
              ['high', t.report.urgencyHigh],
            ] as const
          ).map(([value, label]) => {
            const selected = urgency === value;
            return (
              <button
                key={value}
                type="button"
                onClick={() => setUrgency(value)}
                aria-pressed={selected}
                className={
                  'min-h-12 rounded-lg border px-2 text-base font-medium transition-colors ' +
                  (selected
                    ? 'border-accent bg-accent/10 text-accent'
                    : 'border-border bg-card text-foreground hover:bg-accent/5')
                }
              >
                {label}
              </button>
            );
          })}
        </div>
      </fieldset>

      {/* Description */}
      <div className="flex flex-col gap-1.5">
        <label htmlFor="issue-description" className="text-base font-medium">
          {t.report.descriptionLabel}
        </label>
        <textarea
          id="issue-description"
          rows={4}
          className="rounded-md border border-border bg-background px-3 py-2 text-base focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          value={description}
          disabled={submit.isPending}
          onChange={(e) => {
            setDescription(e.target.value);
            setShowErrors(false);
          }}
          placeholder={t.report.descriptionPlaceholder}
          aria-invalid={descriptionMissing}
          aria-describedby={descriptionMissing ? 'issue-description-error' : undefined}
        />
        {descriptionMissing ? (
          <p id="issue-description-error" className="text-sm text-warning" role="status">
            {t.report.descriptionMissing}
          </p>
        ) : null}
      </div>

      {/* Confirm-then-send: one primary action with an explicit confirm step
          before anything is submitted (PRD §15.3). */}
      {confirming && type ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t.report.confirmTitle}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <dl className="flex flex-col gap-1.5 text-base">
              <ConfirmRow label={t.report.confirmType} value={t.report.types[type]} />
              <ConfirmRow label={t.report.confirmUrgency} value={urgencyLabel(urgency, t)} />
            </dl>
            <p className="whitespace-pre-wrap rounded-md bg-muted/40 px-3 py-2 text-base">
              {description.trim()}
            </p>
            <p className="text-sm text-muted-foreground">{t.report.onceNote}</p>
            <Button
              className="h-14 text-lg"
              disabled={submit.isPending}
              onClick={() => submit.mutate()}
            >
              {submit.isPending ? <Loader2 className="size-5 animate-spin" aria-hidden /> : null}
              {t.report.confirmSend}
            </Button>
            <Button
              variant="outline"
              className="h-12 text-base"
              disabled={submit.isPending}
              onClick={() => setConfirming(false)}
            >
              {t.common.goBackAndEdit}
            </Button>
          </CardContent>
        </Card>
      ) : (
        <Button className="h-14 text-lg" disabled={submit.isPending} onClick={startConfirm}>
          {t.report.submitButton}
        </Button>
      )}
    </div>
  );
}

function urgencyLabel(urgency: Urgency, t: Messages): string {
  return urgency === 'low'
    ? t.report.urgencyLow
    : urgency === 'high'
      ? t.report.urgencyHigh
      : t.report.urgencyMedium;
}

function ConfirmRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-2">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium">{value}</dd>
    </div>
  );
}

function errorCode(err: SdkError): string | undefined {
  const body = err.body as { code?: unknown } | null;
  return body && typeof body.code === 'string' ? body.code : undefined;
}

/** A uuid for the dedupe_key, with an offline-safe fallback for old WebViews. */
function newUuid(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `issue-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function BackHome() {
  const t = useT();
  return (
    <Button asChild variant="ghost" className="h-12 w-fit -ml-2 text-base">
      <Link href="/attendant">
        <ArrowLeft className="size-5" aria-hidden />
        {t.common.myShift}
      </Link>
    </Button>
  );
}
