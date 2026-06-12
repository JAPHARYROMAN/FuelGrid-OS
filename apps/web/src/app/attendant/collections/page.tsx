'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Check, Loader2 } from 'lucide-react';

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
import { toast } from '@/lib/toast';
import { formatLitres, formatMoney } from '@/lib/money';
import {
  addMeterDecimals,
  compareMeterDecimals,
  isMeterDecimal,
  meterFractionDigits,
  subtractMeterDecimals,
} from '@/lib/meter-decimal';

const QUERY_KEY = ['attendant-current-shift'];

/** The tender breakdown of the existing cash-submission contract, in order. */
const TENDERS = [
  { key: 'cash', label: 'Cash' },
  { key: 'mobile_money', label: 'Mobile money' },
  { key: 'card', label: 'Card' },
  { key: 'credit', label: 'Credit' },
] as const;

type TenderKey = (typeof TENDERS)[number]['key'];
type TenderInputs = Record<TenderKey, string>;

const EMPTY_TENDERS: TenderInputs = { cash: '', mobile_money: '', card: '', credit: '' };

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
  const snapshot = useQuery({
    queryKey: QUERY_KEY,
    queryFn: ({ signal }) => api.attendantCurrentShift(signal),
    // Receipt status advances on supervisor actions — keep the screen live.
    refetchInterval: 30_000,
  });

  if (snapshot.isPending) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-10 rounded-xl" />
        <Skeleton className="h-48 rounded-xl" />
        <Skeleton className="h-64 rounded-xl" />
      </div>
    );
  }
  if (snapshot.isError) {
    return (
      <ErrorState
        title="Couldn't load your collections"
        description={String((snapshot.error as Error).message)}
        onRetry={() => snapshot.refetch()}
      />
    );
  }

  const data = snapshot.data;
  if (!data.shift) {
    return (
      <div className="flex flex-col gap-4">
        <BackHome />
        <EmptyState
          title="No collections right now"
          description="You are not on a shift. Collections are submitted after your shift closes."
        />
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
            <CardTitle className="text-lg">Collections</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <p className="text-base text-muted-foreground" role="status">
              Your expected collection is available after the shift closes. Finish your closing
              readings and wait for your supervisor to close the shift.
            </p>
            <Button asChild variant="outline" className="h-12 text-base">
              <Link href="/attendant">Back to my shift</Link>
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
        <h1 className="text-xl font-semibold leading-tight">Collections</h1>
        <p className="text-base text-muted-foreground">
          Hand in everything you collected this shift. Amounts are checked against the meters.
        </p>
      </div>

      <ExpectedCollection data={data} />

      {data.cash_submission ? (
        <>
          <SubmittedCollection data={data} />
          <ReceiptStatus data={data} />
        </>
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
  const lines = data.close_lines ?? [];
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base">Expected collection</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <p className="flex items-baseline justify-between gap-2">
          <span className="text-muted-foreground">Total expected</span>
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
                      Pump {l.pump_number} · Nozzle {l.nozzle_number}
                    </span>
                  </span>
                </span>
                <span className="text-right">
                  <span className="block font-mono font-medium tabular-nums">
                    {formatMoney(l.expected_value, { fallback: '0.00' })}
                  </span>
                  <span className="block font-mono text-xs tabular-nums text-muted-foreground">
                    {formatLitres(l.litres_sold, { maximumFractionDigits: 3 })} L ×{' '}
                    {formatMoney(l.unit_price, { fallback: '0.00' })}
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
  const qc = useQueryClient();
  const [inputs, setInputs] = useState<TenderInputs>(EMPTY_TENDERS);
  const [reason, setReason] = useState('');
  const [confirming, setConfirming] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const shiftID = data.shift?.id ?? '';
  const expected = data.expected_cash ?? '0';

  const submit = useMutation({
    mutationFn: () =>
      api.submitCash(shiftID, {
        cash_amount: tenderOrZero(inputs.cash),
        mobile_money_amount: tenderOrZero(inputs.mobile_money),
        card_amount: tenderOrZero(inputs.card),
        credit_amount: tenderOrZero(inputs.credit),
        ...(reason.trim() !== '' ? { notes: reason.trim() } : {}),
      }),
    onSuccess: async () => {
      setSubmitError(null);
      toast.success(
        'Collections submitted',
        'Your supervisor will now confirm the cash they receive from you.',
      );
      await qc.invalidateQueries({ queryKey: QUERY_KEY });
    },
    onError: (e) => {
      setConfirming(false);
      setSubmitError(submitErrorMessage(e));
    },
  });

  const allValid = TENDERS.every((t) => tenderValid(inputs[t.key]));
  const total = allValid
    ? TENDERS.reduce((sum, t) => addMeterDecimals(sum, tenderOrZero(inputs[t.key])), '0')
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
          <CardTitle className="text-base">Confirm your collections</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <p className="text-base" role="status">
            You are submitting{' '}
            <span className="font-mono font-semibold tabular-nums">{formatMoney(total)}</span>{' '}
            against an expected{' '}
            <span className="font-mono font-semibold tabular-nums">{formatMoney(expected)}</span> —
            difference{' '}
            <span className="font-mono font-semibold tabular-nums">
              {formatMoney(signedDifference(difference))}
            </span>{' '}
            — confirm.
          </p>
          <ul className="flex flex-col gap-1.5 border-t border-border pt-3">
            {TENDERS.map((t) => (
              <li key={t.key} className="flex items-center justify-between text-base">
                <span className="text-muted-foreground">{t.label}</span>
                <span className="font-mono font-medium tabular-nums">
                  {formatMoney(tenderOrZero(inputs[t.key]))}
                </span>
              </li>
            ))}
          </ul>
          {reason.trim() !== '' ? (
            <p className="text-sm text-muted-foreground">Reason: {reason.trim()}</p>
          ) : null}
          <p className="text-sm text-muted-foreground">
            You can submit collections only once for this shift. After this, only your supervisor
            handles changes.
          </p>
          <Button className="h-14 text-lg" disabled={submit.isPending} onClick={() => submit.mutate()}>
            {submit.isPending ? <Loader2 className="size-5 animate-spin" aria-hidden /> : null}
            Confirm and submit
          </Button>
          <Button
            variant="outline"
            className="h-12 text-base"
            disabled={submit.isPending}
            onClick={() => setConfirming(false)}
          >
            Go back and edit
          </Button>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base">Submit your collections</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {TENDERS.map((t) => {
          const value = inputs[t.key];
          const invalid = !tenderValid(value);
          const inputID = `tender-${t.key}`;
          return (
            <div key={t.key} className="flex flex-col gap-1">
              <label htmlFor={inputID} className="text-sm text-muted-foreground">
                {t.label}
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
                  setInputs((p) => ({ ...p, [t.key]: e.target.value }));
                  setSubmitError(null);
                }}
                aria-invalid={invalid}
                aria-describedby={invalid ? `${inputID}-error` : undefined}
              />
              {invalid ? (
                <p id={`${inputID}-error`} className="text-sm font-medium text-danger" role="alert">
                  Enter a money amount like 250000 or 250000.50 (no minus sign).
                </p>
              ) : null}
            </div>
          );
        })}

        <div className="flex flex-col gap-1.5 border-t border-border pt-3">
          <p className="flex items-center justify-between text-base">
            <span className="text-muted-foreground">Submitted total</span>
            <span className="font-mono text-lg font-semibold tabular-nums">
              {total != null ? formatMoney(total) : '—'}
            </span>
          </p>
          <p className="flex items-center justify-between text-base">
            <span className="text-muted-foreground">Expected</span>
            <span className="font-mono font-medium tabular-nums">{formatMoney(expected)}</span>
          </p>
          {difference != null ? <DifferenceLine difference={difference} /> : null}
        </div>

        {reasonRequired ? (
          <div className="flex flex-col gap-1">
            <label htmlFor="collection-reason" className="text-sm font-medium">
              Reason for the difference (required)
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
                  ? 'Explain why you are submitting nothing'
                  : 'Explain why your total does not match the expected amount'
              }
            />
            {reasonMissing ? (
              <p className="text-sm text-warning" role="status">
                Add a short reason before you can submit.
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
          Submit collections
        </Button>
      </CardContent>
    </Card>
  );
}

/** Live difference strip — text always carries the meaning, colour reinforces it (PRD §15.1). */
function DifferenceLine({ difference }: { difference: Difference }) {
  switch (difference.kind) {
    case 'balanced':
      return (
        <p className="rounded-md bg-success/10 px-3 py-2 text-base font-medium text-success" role="status">
          Balanced — your total matches the expected collection.
        </p>
      );
    case 'shortage':
      return (
        <p className="rounded-md bg-danger/10 px-3 py-2 text-base font-medium text-danger" role="status">
          Shortage of {formatMoney(difference.amount)} — you are handing in less than expected.
        </p>
      );
    case 'excess':
      return (
        <p className="rounded-md bg-warning/10 px-3 py-2 text-base font-medium text-warning" role="status">
          Excess of {formatMoney(difference.amount)} — you are handing in more than expected.
        </p>
      );
  }
}

/** The locked, read-only view once the one-per-shift submission exists. */
function SubmittedCollection({ data }: { data: AttendantCurrentShift }) {
  const sub = data.cash_submission;
  if (!sub) return null;
  const difference = differenceVsExpected(sub.submitted_total, sub.expected_cash);
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center justify-between gap-2 text-base">
          Your submission
          <span className="flex size-7 items-center justify-center rounded-full bg-success/15 text-success">
            <Check className="size-4" aria-hidden />
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-1.5">
        <Row label="Cash" value={formatMoney(sub.cash_amount)} />
        <Row label="Mobile money" value={formatMoney(sub.mobile_money_amount)} />
        <Row label="Card" value={formatMoney(sub.card_amount)} />
        <Row label="Credit" value={formatMoney(sub.credit_amount)} />
        <div className="border-t border-border pt-1.5">
          <Row label="Submitted total" value={formatMoney(sub.submitted_total)} strong />
          <Row label="Expected" value={formatMoney(sub.expected_cash)} />
        </div>
        <DifferenceLine difference={difference} />
        {sub.notes ? <p className="text-sm text-muted-foreground">Reason: {sub.notes}</p> : null}
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
  const receipt = data.collection_receipt;
  if (!receipt) {
    return (
      <p className="rounded-md bg-accent/10 px-3 py-2 text-base" role="status">
        Submitted — waiting for your supervisor to confirm receipt.
      </p>
    );
  }
  const difference = differenceVsExpected(receipt.supervisor_received_total, receipt.expected_amount);
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center justify-between gap-2 text-base">
          Supervisor receipt
          <ReceiptBadge status={receipt.status} />
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-1.5">
        <Row label="Received" value={formatMoney(receipt.supervisor_received_total)} strong />
        <Row label="Expected" value={formatMoney(receipt.expected_amount)} />
        <Row label="Difference" value={formatMoney(receipt.difference)} />
        {receipt.status === 'rejected' ? (
          <p className="rounded-md bg-danger/10 px-3 py-2 text-base font-medium text-danger" role="alert">
            Your collection was rejected. See your supervisor.
          </p>
        ) : difference.kind !== 'balanced' ? (
          <DifferenceLine difference={difference} />
        ) : null}
        {receipt.reason ? (
          <p className="text-sm text-muted-foreground">Supervisor reason: {receipt.reason}</p>
        ) : null}
        {receipt.supervisor_comment ? (
          <p className="text-sm text-muted-foreground">Comment: {receipt.supervisor_comment}</p>
        ) : null}
      </CardContent>
    </Card>
  );
}

function ReceiptBadge({ status }: { status: string }) {
  switch (status) {
    case 'received':
      return <Badge tone="success">Received</Badge>;
    case 'approved_with_difference':
      return <Badge tone="warning">Approved with difference</Badge>;
    case 'rejected':
      return <Badge tone="danger">Rejected</Badge>;
    default:
      return <Badge tone="neutral">{status.replaceAll('_', ' ')}</Badge>;
  }
}

/** Maps a submission failure to a plain-language message. */
function submitErrorMessage(e: unknown): string {
  if (e instanceof SdkError) {
    const body = e.body as { code?: string } | null;
    if (body?.code === 'variance_reason_required') {
      return 'Your total does not match the expected amount — add a reason explaining the difference.';
    }
    if (e.status === 409) {
      return 'Collections were already submitted for this shift.';
    }
    if (e.message) return e.message;
  }
  return 'Could not submit your collections. Check your connection and try again.';
}

function Row({ label, value, strong }: { label: string; value: string; strong?: boolean }) {
  return (
    <p className="flex items-center justify-between text-base">
      <span className="text-muted-foreground">{label}</span>
      <span className={`font-mono tabular-nums ${strong ? 'text-lg font-semibold' : 'font-medium'}`}>
        {value}
      </span>
    </p>
  );
}

function BackHome() {
  return (
    <Button asChild variant="ghost" className="h-12 w-fit -ml-2 text-base">
      <Link href="/attendant">
        <ArrowLeft className="size-5" aria-hidden />
        My shift
      </Link>
    </Button>
  );
}
