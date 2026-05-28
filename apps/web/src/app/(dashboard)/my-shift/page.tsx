'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { SdkError, type MyShiftNozzle, type MyShiftTank } from '@fuelgrid/sdk';
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
  Label,
  LoadingState,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

const QUERY_KEY = ['my-shift'];

function fmtMoney(n: number) {
  return n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

export default function MyShiftPage() {
  const qc = useQueryClient();
  const shift = useQuery({
    queryKey: QUERY_KEY,
    queryFn: ({ signal }) => api.myActiveShift(signal),
  });

  // Per-nozzle reading inputs, keyed by `${nozzleId}:${type}`.
  const [readingInputs, setReadingInputs] = useState<Record<string, string>>({});
  // Per-tank dip inputs (mm), keyed by `${tankId}:${type}`.
  const [dipInputs, setDipInputs] = useState<Record<string, string>>({});
  const [cash, setCash] = useState({ cash: '', mobile: '', card: '', credit: '' });
  const [actionError, setActionError] = useState<string | null>(null);

  const shiftID = shift.data?.shift?.id ?? '';

  const capture = useMutation({
    mutationFn: ({
      nozzleID,
      type,
      reading,
    }: {
      nozzleID: string;
      type: 'opening' | 'closing';
      reading: number;
    }) => api.captureMeterReading(shiftID, { nozzle_id: nozzleID, reading_type: type, reading }),
    onSuccess: () => {
      setActionError(null);
      qc.invalidateQueries({ queryKey: QUERY_KEY });
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not save reading'),
  });

  const captureDip = useMutation({
    mutationFn: ({
      tankID,
      type,
      dipMM,
    }: {
      tankID: string;
      type: 'opening' | 'closing';
      dipMM: number;
    }) => api.captureDipReading(shiftID, { tank_id: tankID, reading_type: type, dip_mm: dipMM }),
    onSuccess: () => {
      setActionError(null);
      qc.invalidateQueries({ queryKey: QUERY_KEY });
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not save dip'),
  });

  const submitCash = useMutation({
    mutationFn: () =>
      api.submitCash(shiftID, {
        cash_amount: Number(cash.cash) || 0,
        mobile_money_amount: Number(cash.mobile) || 0,
        card_amount: Number(cash.card) || 0,
        credit_amount: Number(cash.credit) || 0,
      }),
    onSuccess: () => {
      setActionError(null);
      qc.invalidateQueries({ queryKey: QUERY_KEY });
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not submit cash'),
  });

  if (shift.isPending) return <LoadingState />;
  if (shift.isError) {
    return (
      <ErrorState
        title="Couldn't load your shift"
        description={String((shift.error as Error).message)}
        onRetry={() => shift.refetch()}
      />
    );
  }

  const data = shift.data;
  if (!data.shift) {
    return (
      <div className="mx-auto max-w-md">
        <EmptyState
          title="No active shift"
          description="You're not assigned to an open shift right now. Check back when your supervisor opens one."
        />
      </div>
    );
  }

  const s = data.shift;
  const isOpen = s.status === 'open';
  const isClosed = s.status === 'closed';
  const isApproved = s.status === 'approved';

  function captureRow(n: MyShiftNozzle, type: 'opening' | 'closing', current?: number) {
    const key = `${n.nozzle_id}:${type}`;
    const step = 1 / Math.pow(10, n.meter_decimal_places);
    return (
      <div className="flex items-center justify-between gap-2">
        <span className="w-16 text-sm capitalize text-muted-foreground">{type}</span>
        {current != null ? (
          <span className="flex-1 text-right font-mono tabular-nums">
            {current.toLocaleString(undefined, { maximumFractionDigits: n.meter_decimal_places })}
          </span>
        ) : isOpen ? (
          <>
            <Input
              className="h-12 flex-1 text-right text-base"
              type="number"
              inputMode="decimal"
              step={step}
              min="0"
              value={readingInputs[key] ?? ''}
              onChange={(e) => setReadingInputs((p) => ({ ...p, [key]: e.target.value }))}
              placeholder="0"
            />
            <Button
              className="h-12"
              size="sm"
              disabled={!readingInputs[key] || capture.isPending}
              onClick={() =>
                capture.mutate({ nozzleID: n.nozzle_id, type, reading: Number(readingInputs[key]) })
              }
            >
              Save
            </Button>
          </>
        ) : (
          <span className="flex-1 text-right text-muted-foreground">—</span>
        )}
      </div>
    );
  }

  function dipRow(t: MyShiftTank, type: 'opening' | 'closing') {
    const key = `${t.tank_id}:${type}`;
    const captured = type === 'opening' ? t.opening_dip_mm : t.closing_dip_mm;
    const volume = type === 'opening' ? t.opening_volume_litres : t.closing_volume_litres;
    return (
      <div className="flex items-center justify-between gap-2">
        <span className="w-16 text-sm capitalize text-muted-foreground">{type}</span>
        {captured != null ? (
          <span className="flex-1 text-right font-mono text-sm tabular-nums">
            {captured.toLocaleString()} mm
            {volume != null ? (
              <span className="ml-2 text-muted-foreground">
                {volume.toLocaleString(undefined, { maximumFractionDigits: 0 })} L
              </span>
            ) : null}
          </span>
        ) : isOpen ? (
          <>
            <Input
              className="h-12 flex-1 text-right text-base"
              type="number"
              inputMode="decimal"
              step="1"
              min="0"
              value={dipInputs[key] ?? ''}
              onChange={(e) => setDipInputs((p) => ({ ...p, [key]: e.target.value }))}
              placeholder="mm"
            />
            <Button
              className="h-12"
              size="sm"
              disabled={!dipInputs[key] || captureDip.isPending}
              onClick={() =>
                captureDip.mutate({ tankID: t.tank_id, type, dipMM: Number(dipInputs[key]) })
              }
            >
              Save
            </Button>
          </>
        ) : (
          <span className="flex-1 text-right text-muted-foreground">—</span>
        )}
      </div>
    );
  }

  const expected = data.expected_cash ?? 0;
  const cashTotal =
    (Number(cash.cash) || 0) +
    (Number(cash.mobile) || 0) +
    (Number(cash.card) || 0) +
    (Number(cash.credit) || 0);
  const variance = cashTotal - expected;

  return (
    <div className="mx-auto flex max-w-md flex-col gap-4">
      <header className="flex items-center justify-between gap-2">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">My Shift</h1>
          <p className="text-sm text-muted-foreground">{s.name}</p>
        </div>
        <Badge tone={isOpen ? 'success' : isApproved ? 'neutral' : 'warning'}>{s.status}</Badge>
      </header>

      {isApproved ? (
        <div className="rounded-md bg-success/10 px-3 py-2 text-sm text-success">
          This shift has been approved. Nothing more to do.
        </div>
      ) : null}

      {actionError ? (
        <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
          {actionError}
        </p>
      ) : null}

      {/* Assigned nozzles + readings */}
      <section className="flex flex-col gap-3">
        <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
          My nozzles
        </h2>
        {data.assigned_nozzles.length === 0 ? (
          <p className="text-sm text-muted-foreground">No nozzles assigned to you on this shift.</p>
        ) : (
          data.assigned_nozzles.map((n) => (
            <Card key={n.nozzle_id}>
              <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
                <CardTitle className="flex items-center gap-2 text-base">
                  <span
                    className="inline-block size-3 rounded-full border border-border"
                    style={{ backgroundColor: n.product_color }}
                    aria-hidden
                  />
                  {n.product_name}
                </CardTitle>
                <span className="font-mono text-xs text-muted-foreground">
                  P{n.pump_number}·N{n.nozzle_number} ← {n.tank_code}
                </span>
              </CardHeader>
              <CardContent className="flex flex-col gap-2">
                {captureRow(n, 'opening', n.opening_reading)}
                {captureRow(n, 'closing', n.closing_reading)}
              </CardContent>
            </Card>
          ))
        )}
      </section>

      {/* Assigned tanks + dips */}
      {data.assigned_tanks.length > 0 ? (
        <section className="flex flex-col gap-3">
          <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
            My tanks
          </h2>
          {data.assigned_tanks.map((t) => (
            <Card key={t.tank_id}>
              <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
                <CardTitle className="flex items-center gap-2 text-base">
                  <span
                    className="inline-block size-3 rounded-full border border-border"
                    style={{ backgroundColor: t.product_color }}
                    aria-hidden
                  />
                  Tank {t.tank_code}
                </CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-2">
                {dipRow(t, 'opening')}
                {dipRow(t, 'closing')}
              </CardContent>
            </Card>
          ))}
        </section>
      ) : null}

      {/* Cash submission (after close) */}
      {isClosed ? (
        <section className="flex flex-col gap-3">
          <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
            Cash
          </h2>
          {data.cash_submission ? (
            <Card>
              <CardContent className="flex flex-col gap-1 pt-6 text-sm">
                <Row label="Expected" value={fmtMoney(data.cash_submission.expected_cash)} />
                <Row label="Submitted" value={fmtMoney(data.cash_submission.submitted_total)} />
                <Row
                  label="Variance"
                  value={fmtMoney(data.cash_submission.variance)}
                  tone={data.cash_submission.variance < 0 ? 'danger' : 'success'}
                />
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardContent className="flex flex-col gap-3 pt-6">
                {(['cash', 'mobile', 'card', 'credit'] as const).map((field) => (
                  <div key={field} className="flex flex-col gap-1.5">
                    <Label htmlFor={field} className="capitalize">
                      {field === 'mobile' ? 'Mobile money' : field}
                    </Label>
                    <Input
                      id={field}
                      className="h-12 text-base"
                      type="number"
                      inputMode="decimal"
                      step="0.01"
                      min="0"
                      value={cash[field]}
                      onChange={(e) => setCash((p) => ({ ...p, [field]: e.target.value }))}
                      placeholder="0.00"
                    />
                  </div>
                ))}
                <div className="rounded-md bg-muted px-3 py-2 text-sm">
                  <Row label="Expected" value={fmtMoney(expected)} />
                  <Row label="Submitting" value={fmtMoney(cashTotal)} />
                  <Row
                    label="Variance"
                    value={fmtMoney(variance)}
                    tone={variance < 0 ? 'danger' : 'success'}
                  />
                </div>
                <Button
                  className="h-12"
                  disabled={submitCash.isPending}
                  onClick={() => submitCash.mutate()}
                >
                  {submitCash.isPending ? 'Submitting…' : 'Submit cash'}
                </Button>
              </CardContent>
            </Card>
          )}
        </section>
      ) : null}
    </div>
  );
}

function Row({
  label,
  value,
  tone,
}: {
  label: string;
  value: string;
  tone?: 'danger' | 'success';
}) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-muted-foreground">{label}</span>
      <span
        className={
          'font-medium tabular-nums' +
          (tone === 'danger' ? ' text-danger' : tone === 'success' ? ' text-success' : '')
        }
      >
        {value}
      </span>
    </div>
  );
}
