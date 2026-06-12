'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Check, CloudOff, Loader2 } from 'lucide-react';

import { SdkError, type AttendantCurrentShift } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  Input,
  Skeleton,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { useT, type Messages } from '@/lib/i18n';
import { toast } from '@/lib/toast';
import { formatLitres, formatMoney } from '@/lib/money';
import {
  addMeterDecimals,
  compareMeterDecimals,
  isMeterDecimal,
  meterFractionDigits,
  subtractMeterDecimals,
} from '@/lib/meter-decimal';
import {
  getSyncEngine,
  isOfflineError,
  useAttendantSnapshot,
  useSyncEngineState,
  type CollectionPayload,
} from '@/lib/offline';

const QUERY_KEY = ['attendant-current-shift'];

/** The tender breakdown of the existing cash-submission contract, in order. */
const TENDER_KEYS = ['cash', 'mobile_money', 'card', 'credit'] as const;

type TenderKey = (typeof TENDER_KEYS)[number];
type TenderInputs = Record<TenderKey, string>;

const EMPTY_TENDERS: TenderInputs = { cash: '', mobile_money: '', card: '', credit: '' };

function tenderLabel(key: TenderKey, t: Messages): string {
  switch (key) {
    case 'cash':
      return t.collections.tenderCash;
    case 'mobile_money':
      return t.collections.tenderMobileMoney;
    case 'card':
      return t.collections.tenderCard;
    case 'credit':
      return t.collections.tenderCredit;
  }
}

/** An omitted tender is "0" — mirroring the server's tenderOrZero. */
function tenderOrZero(value: string): string {
  const v = value.trim();
  return v === '' ? '0' : v;
}

/** A tender is valid when empty (= 0) or a non-negative money decimal (max 2dp). */
function tenderValid(value: string): boolean {
  const v = value.trim();
  return v === '' || (isMeterDecimal(v) && meterFractionDigits(v) <= 2);
}

/**
 * The live difference vs expected, decided with exact decimal-string math
 * (never parseFloat): negative = shortage, positive = excess, zero = balanced
 * (PRD §12.4). `amount` is the absolute difference for display.
 */
type Difference =
  | { kind: 'balanced' }
  | { kind: 'shortage'; amount: string }
  | { kind: 'excess'; amount: string };

function differenceVsExpected(total: string, expected: string): Difference {
  const cmp = compareMeterDecimals(total, expected);
  if (cmp === 0) return { kind: 'balanced' };
  if (cmp < 0) return { kind: 'shortage', amount: subtractMeterDecimals(expected, total) };
  return { kind: 'excess', amount: subtractMeterDecimals(total, expected) };
}

/** Signed difference (total − expected) for the confirmation sentence. */
function signedDifference(d: Difference): string {
  switch (d.kind) {
    case 'balanced':
      return '0';
    case 'shortage':
      return `-${d.amount}`;
    case 'excess':
      return d.amount;
  }
}

export default function CollectionsPage() {
  const t = useT();
  const snapshot = useAttendantSnapshot({
    // Receipt status advances on supervisor actions — keep the screen live.
    refetchInterval: 30_000,
  });
  const engineState = useSyncEngineState();

  if (snapshot.isPending) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-10 rounded-xl" />
        <Skeleton className="h-48 rounded-xl" />
        <Skeleton className="h-64 rounded-xl" />
      </div>
    );
  }
  if (snapshot.showError) {
    return (
      <ErrorState
        title={t.collections.errLoadTitle}
        description={String((snapshot.error as Error).message)}
        action={
          <Button variant="secondary" onClick={() => snapshot.refetch()}>
            {t.common.tryAgain}
          </Button>
        }
      />
    );
  }

  const data = snapshot.data as AttendantCurrentShift;

  // A collection already saved on this phone for this shift (Phase 6a queue):
  // the form is replaced by the queued submission until it syncs — one
  // submission per shift, including the offline one.
  const queuedCollection = engineState.items.find(
    (i) =>
      i.action_type === 'collection' &&
      i.shift_id === (data.shift?.id ?? '') &&
      (i.sync_status === 'pending' || i.sync_status === 'syncing'),
  );
  if (!data.shift) {
    return (
      <div className="flex flex-col gap-4">
        <BackHome />
        <EmptyState title={t.collections.emptyTitle} description={t.collections.emptyNoShift} />
      </div>
    );
  }

  // Honest pre-close state: expected collection (the frozen close lines) only
  // exists once the supervisor closes the shift.
  if (data.expected_cash == null) {
    return (
      <div className="flex flex-col gap-4">
        <BackHome />
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">{t.collections.title}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <p className="text-base text-muted-foreground" role="status">
              {t.collections.preCloseBody}
            </p>
            <Button asChild variant="outline" className="h-12 text-base">
              <Link href="/attendant">{t.common.backToMyShift}</Link>
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
        <h1 className="text-xl font-semibold leading-tight">{t.collections.title}</h1>
        <p className="text-base text-muted-foreground">{t.collections.subtitle}</p>
      </div>

      <ExpectedCollection data={data} />

      {data.cash_submission ? (
        <>
          <SubmittedCollection data={data} />
          <ReceiptStatus data={data} />
        </>
      ) : queuedCollection ? (
        <QueuedCollection payload={queuedCollection.payload as CollectionPayload} data={data} />
      ) : data.next_action === 'await_reading_verification' ? (
        // Honest wait state: the shift is closed but the supervisor is still
        // verifying readings — a correction would change the expected figure,
        // so the form waits for the final basis (PRD §7.9: approved readings).
        <p className="rounded-md bg-accent/10 px-3 py-2 text-base" role="status">
          {t.collections.awaitVerification}
        </p>
      ) : (
        <SubmissionForm data={data} />
      )}
    </div>
  );
}

/**
 * The expected collection and its calculation basis per nozzle: litres sold ×
 * unit price = amount, straight from the frozen close lines (PRD §7.9/§12.3).
 * The attendant sees the price but can never edit it.
 */
function ExpectedCollection({ data }: { data: AttendantCurrentShift }) {
  const t = useT();
  const lines = data.close_lines ?? [];
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base">{t.collections.expectedCollection}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <p className="flex items-baseline justify-between gap-2">
          <span className="text-muted-foreground">{t.collections.totalExpected}</span>
          <span className="font-mono text-2xl font-semibold tabular-nums">
            {formatMoney(data.expected_cash, { fallback: '0.00' })}
          </span>
        </p>
        {lines.length > 0 ? (
          <ul className="flex flex-col gap-2 border-t border-border pt-3">
            {lines.map((l) => (
              <li key={l.nozzle_id} className="flex items-center justify-between gap-2 text-sm">
                <span className="flex min-w-0 items-center gap-2">
                  <span
                    className="inline-block size-3 shrink-0 rounded-full border border-border"
                    style={{ backgroundColor: l.product_color }}
                    aria-hidden
                  />
                  <span className="min-w-0">
                    <span className="block truncate font-medium">{l.product_name}</span>
                    <span className="block font-mono text-xs text-muted-foreground">
                      {t.common.pumpNozzle(l.pump_number, l.nozzle_number)}
                    </span>
                  </span>
                </span>
                <span className="text-right">
                  <span className="block font-mono font-medium tabular-nums">
                    {formatMoney(l.expected_value, { fallback: '0.00' })}
                  </span>
                  <span className="block font-mono text-xs tabular-nums text-muted-foreground">
                    {t.collections.litresTimesPrice(
                      formatLitres(l.litres_sold, { maximumFractionDigits: 3 }),
                      formatMoney(l.unit_price, { fallback: '0.00' }),
                    )}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        ) : null}
      </CardContent>
    </Card>
  );
}

/** The tender breakdown form with live difference, reason policy, and confirm step. */
function SubmissionForm({ data }: { data: AttendantCurrentShift }) {
  const t = useT();
  const qc = useQueryClient();
  const [inputs, setInputs] = useState<TenderInputs>(EMPTY_TENDERS);
  const [reason, setReason] = useState('');
  const [confirming, setConfirming] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const shiftID = data.shift?.id ?? '';
  const expected = data.expected_cash ?? '0';

  const submit = useMutation({
    mutationFn: async () => {
      const payload = {
        cash_amount: tenderOrZero(inputs.cash),
        mobile_money_amount: tenderOrZero(inputs.mobile_money),
        card_amount: tenderOrZero(inputs.card),
        credit_amount: tenderOrZero(inputs.credit),
        ...(reason.trim() !== '' ? { notes: reason.trim() } : {}),
      };
      try {
        await api.submitCash(shiftID, payload);
        return 'submitted' as const;
      } catch (e) {
        // Connectivity failure → save the submission on this phone (exact
        // decimal strings preserved) and replay it when online returns.
        if (isOfflineError(e)) {
          await getSyncEngine().enqueue({
            action_type: 'collection',
            shift_id: shiftID,
            payload,
          });
          return 'queued' as const;
        }
        throw e;
      }
    },
    onSuccess: async (result) => {
      setSubmitError(null);
      if (result === 'queued') {
        toast.success(t.collections.toastQueuedTitle, t.collections.toastQueuedBody);
      } else {
        toast.success(t.collections.toastSubmittedTitle, t.collections.toastSubmittedBody);
      }
      await qc.invalidateQueries({ queryKey: QUERY_KEY });
    },
    onError: (e) => {
      setConfirming(false);
      setSubmitError(submitErrorMessage(e, t));
    },
  });

  const allValid = TENDER_KEYS.every((key) => tenderValid(inputs[key]));
  const total = allValid
    ? TENDER_KEYS.reduce((sum, key) => addMeterDecimals(sum, tenderOrZero(inputs[key])), '0')
    : null;
  const difference = total != null ? differenceVsExpected(total, expected) : null;
  const totalIsZero = total != null && compareMeterDecimals(total, '0') === 0;

  // Reason policy (client-side mirror of the server's 422
  // variance_reason_required): any difference from expected — or handing in
  // nothing at all — must be explained before submitting.
  const reasonRequired = difference != null && (difference.kind !== 'balanced' || totalIsZero);
  const reasonMissing = reasonRequired && reason.trim() === '';
  const canSubmit = allValid && total != null && !reasonMissing && !submit.isPending;

  if (confirming && total != null && difference != null) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t.collections.confirmTitle}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <p className="text-base" role="status">
            {t.collections.confirmPart1}
            <span className="font-mono font-semibold tabular-nums">{formatMoney(total)}</span>
            {t.collections.confirmPart2}
            <span className="font-mono font-semibold tabular-nums">{formatMoney(expected)}</span>
            {t.collections.confirmPart3}
            <span className="font-mono font-semibold tabular-nums">
              {formatMoney(signedDifference(difference))}
            </span>
            {t.collections.confirmPart4}
          </p>
          <ul className="flex flex-col gap-1.5 border-t border-border pt-3">
            {TENDER_KEYS.map((key) => (
              <li key={key} className="flex items-center justify-between text-base">
                <span className="text-muted-foreground">{tenderLabel(key, t)}</span>
                <span className="font-mono font-medium tabular-nums">
                  {formatMoney(tenderOrZero(inputs[key]))}
                </span>
              </li>
            ))}
          </ul>
          {reason.trim() !== '' ? (
            <p className="text-sm text-muted-foreground">{t.common.reason(reason.trim())}</p>
          ) : null}
          <p className="text-sm text-muted-foreground">{t.collections.onceNote}</p>
          <Button
            className="h-14 text-lg"
            disabled={submit.isPending}
            onClick={() => submit.mutate()}
          >
            {submit.isPending ? <Loader2 className="size-5 animate-spin" aria-hidden /> : null}
            {t.common.confirmAndSubmit}
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
    );
  }

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base">{t.collections.formTitle}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {TENDER_KEYS.map((key) => {
          const value = inputs[key];
          const invalid = !tenderValid(value);
          const inputID = `tender-${key}`;
          return (
            <div key={key} className="flex flex-col gap-1">
              <label htmlFor={inputID} className="text-sm text-muted-foreground">
                {tenderLabel(key, t)}
              </label>
              <Input
                id={inputID}
                className="h-14 text-right font-mono text-lg tabular-nums"
                type="text"
                inputMode="decimal"
                autoComplete="off"
                placeholder="0"
                value={value}
                disabled={submit.isPending}
                onChange={(e) => {
                  setInputs((p) => ({ ...p, [key]: e.target.value }));
                  setSubmitError(null);
                }}
                aria-invalid={invalid}
                aria-describedby={invalid ? `${inputID}-error` : undefined}
              />
              {invalid ? (
                <p id={`${inputID}-error`} className="text-sm font-medium text-danger" role="alert">
                  {t.collections.tenderInvalid}
                </p>
              ) : null}
            </div>
          );
        })}

        <div className="flex flex-col gap-1.5 border-t border-border pt-3">
          <p className="flex items-center justify-between text-base">
            <span className="text-muted-foreground">{t.collections.submittedTotal}</span>
            <span className="font-mono text-lg font-semibold tabular-nums">
              {total != null ? formatMoney(total) : '—'}
            </span>
          </p>
          <p className="flex items-center justify-between text-base">
            <span className="text-muted-foreground">{t.collections.expected}</span>
            <span className="font-mono font-medium tabular-nums">{formatMoney(expected)}</span>
          </p>
          {difference != null ? <DifferenceLine difference={difference} /> : null}
        </div>

        {reasonRequired ? (
          <div className="flex flex-col gap-1">
            <label htmlFor="collection-reason" className="text-sm font-medium">
              {t.collections.reasonLabel}
            </label>
            <textarea
              id="collection-reason"
              rows={3}
              className="rounded-md border border-border bg-background px-3 py-2 text-base focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              value={reason}
              disabled={submit.isPending}
              onChange={(e) => setReason(e.target.value)}
              placeholder={
                totalIsZero && difference?.kind === 'balanced'
                  ? t.collections.reasonPlaceholderZero
                  : t.collections.reasonPlaceholderDiff
              }
            />
            {reasonMissing ? (
              <p className="text-sm text-warning" role="status">
                {t.collections.reasonMissing}
              </p>
            ) : null}
          </div>
        ) : null}

        {submitError ? (
          <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
            {submitError}
          </p>
        ) : null}

        <Button className="h-14 text-lg" disabled={!canSubmit} onClick={() => setConfirming(true)}>
          {t.collections.submitButton}
        </Button>
      </CardContent>
    </Card>
  );
}

/** Live difference strip — text always carries the meaning, colour reinforces it (PRD §15.1). */
function DifferenceLine({ difference }: { difference: Difference }) {
  const t = useT();
  switch (difference.kind) {
    case 'balanced':
      return (
        <p
          className="rounded-md bg-success/10 px-3 py-2 text-base font-medium text-success"
          role="status"
        >
          {t.collections.balanced}
        </p>
      );
    case 'shortage':
      return (
        <p
          className="rounded-md bg-danger/10 px-3 py-2 text-base font-medium text-danger"
          role="status"
        >
          {t.collections.shortage(formatMoney(difference.amount))}
        </p>
      );
    case 'excess':
      return (
        <p
          className="rounded-md bg-warning/10 px-3 py-2 text-base font-medium text-warning"
          role="status"
        >
          {t.collections.excess(formatMoney(difference.amount))}
        </p>
      );
  }
}

/**
 * The submission saved on this phone while offline (Phase 6a): the same
 * locked, read-only breakdown — clearly marked unsynced. It replaces the form
 * (one submission per shift, including the queued one); the sync sheet on the
 * header chip carries retry/conflict handling.
 */
function QueuedCollection({
  payload,
  data,
}: {
  payload: CollectionPayload;
  data: AttendantCurrentShift;
}) {
  const t = useT();
  const expected = data.expected_cash ?? '0';
  const total = [
    payload.cash_amount,
    payload.mobile_money_amount,
    payload.card_amount,
    payload.credit_amount,
  ].reduce((sum, v) => addMeterDecimals(sum, v.trim() === '' ? '0' : v), '0');
  const difference = differenceVsExpected(total, expected);
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center justify-between gap-2 text-base">
          {t.collections.yourSubmission}
          <span className="flex size-7 items-center justify-center rounded-full bg-warning/15 text-warning">
            <CloudOff className="size-4" aria-hidden />
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-1.5">
        <p
          className="rounded-md bg-warning/10 px-3 py-2 text-sm font-medium text-warning"
          role="status"
        >
          {t.common.savedOnPhone}
        </p>
        <Row label={t.collections.tenderCash} value={formatMoney(payload.cash_amount)} />
        <Row
          label={t.collections.tenderMobileMoney}
          value={formatMoney(payload.mobile_money_amount)}
        />
        <Row label={t.collections.tenderCard} value={formatMoney(payload.card_amount)} />
        <Row label={t.collections.tenderCredit} value={formatMoney(payload.credit_amount)} />
        <div className="border-t border-border pt-1.5">
          <Row label={t.collections.submittedTotal} value={formatMoney(total)} strong />
          <Row label={t.collections.expected} value={formatMoney(expected)} />
        </div>
        <DifferenceLine difference={difference} />
        {payload.notes ? (
          <p className="text-sm text-muted-foreground">{t.common.reason(payload.notes)}</p>
        ) : null}
      </CardContent>
    </Card>
  );
}

/** The locked, read-only view once the one-per-shift submission exists. */
function SubmittedCollection({ data }: { data: AttendantCurrentShift }) {
  const t = useT();
  const sub = data.cash_submission;
  if (!sub) return null;
  const difference = differenceVsExpected(sub.submitted_total, sub.expected_cash);
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center justify-between gap-2 text-base">
          {t.collections.yourSubmission}
          <span className="flex size-7 items-center justify-center rounded-full bg-success/15 text-success">
            <Check className="size-4" aria-hidden />
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-1.5">
        <Row label={t.collections.tenderCash} value={formatMoney(sub.cash_amount)} />
        <Row label={t.collections.tenderMobileMoney} value={formatMoney(sub.mobile_money_amount)} />
        <Row label={t.collections.tenderCard} value={formatMoney(sub.card_amount)} />
        <Row label={t.collections.tenderCredit} value={formatMoney(sub.credit_amount)} />
        <div className="border-t border-border pt-1.5">
          <Row
            label={t.collections.submittedTotal}
            value={formatMoney(sub.submitted_total)}
            strong
          />
          <Row label={t.collections.expected} value={formatMoney(sub.expected_cash)} />
        </div>
        <DifferenceLine difference={difference} />
        {sub.notes ? (
          <p className="text-sm text-muted-foreground">{t.common.reason(sub.notes)}</p>
        ) : null}
      </CardContent>
    </Card>
  );
}

/**
 * The supervisor receipt status (PRD 20.10's collection side): waiting,
 * received, approved with difference, or rejected — with the supervisor's
 * reason and plain-language guidance on rejection.
 */
function ReceiptStatus({ data }: { data: AttendantCurrentShift }) {
  const t = useT();
  const receipt = data.collection_receipt;
  if (!receipt) {
    return (
      <p className="rounded-md bg-accent/10 px-3 py-2 text-base" role="status">
        {t.collections.receiptWaiting}
      </p>
    );
  }
  const difference = differenceVsExpected(
    receipt.supervisor_received_total,
    receipt.expected_amount,
  );
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center justify-between gap-2 text-base">
          {t.collections.supervisorReceipt}
          <ReceiptBadge status={receipt.status} />
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-1.5">
        <Row
          label={t.collections.receivedRow}
          value={formatMoney(receipt.supervisor_received_total)}
          strong
        />
        <Row label={t.collections.expected} value={formatMoney(receipt.expected_amount)} />
        <Row label={t.collections.differenceRow} value={formatMoney(receipt.difference)} />
        {receipt.status === 'rejected' ? (
          <p
            className="rounded-md bg-danger/10 px-3 py-2 text-base font-medium text-danger"
            role="alert"
          >
            {t.collections.rejectedAlert}
          </p>
        ) : difference.kind !== 'balanced' ? (
          <DifferenceLine difference={difference} />
        ) : null}
        {receipt.reason ? (
          <p className="text-sm text-muted-foreground">
            {t.common.supervisorReason(receipt.reason)}
          </p>
        ) : null}
        {receipt.supervisor_comment ? (
          <p className="text-sm text-muted-foreground">
            {t.collections.comment(receipt.supervisor_comment)}
          </p>
        ) : null}
      </CardContent>
    </Card>
  );
}

function ReceiptBadge({ status }: { status: string }) {
  const t = useT();
  switch (status) {
    case 'received':
      return <Badge tone="success">{t.collections.badgeReceived}</Badge>;
    case 'approved_with_difference':
      return <Badge tone="warning">{t.collections.badgeApprovedWithDifference}</Badge>;
    case 'rejected':
      return <Badge tone="danger">{t.collections.badgeRejected}</Badge>;
    default:
      return <Badge tone="neutral">{status.replaceAll('_', ' ')}</Badge>;
  }
}

/** Maps a submission failure to a plain-language message. */
function submitErrorMessage(e: unknown, t: Messages): string {
  if (e instanceof SdkError) {
    const body = e.body as { code?: string } | null;
    if (body?.code === 'variance_reason_required') {
      return t.collections.errVarianceReason;
    }
    if (e.status === 409) {
      return t.collections.errAlreadySubmitted;
    }
    // Raw server prose — shown verbatim (untranslated) as the honest fallback.
    if (e.message) return e.message;
  }
  return t.collections.errGeneric;
}

function Row({ label, value, strong }: { label: string; value: string; strong?: boolean }) {
  return (
    <p className="flex items-center justify-between text-base">
      <span className="text-muted-foreground">{label}</span>
      <span
        className={`font-mono tabular-nums ${strong ? 'text-lg font-semibold' : 'font-medium'}`}
      >
        {value}
      </span>
    </p>
  );
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
