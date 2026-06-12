'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, ArrowRight, Check, Loader2, PartyPopper } from 'lucide-react';

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
  Skeleton,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { toast } from '@/lib/toast';
import { formatMoney } from '@/lib/money';

const QUERY_KEY = ['attendant-current-shift'];

/**
 * Shift Complete screen (Phase 4, PRD 20.10): the end-of-shift summary —
 * reading verification status, collection confirmation status, the final
 * difference, a friendly completion message, and the check-out action when
 * the attendant is still checked in (Phase 0 endpoint).
 */
export default function ShiftCompletePage() {
  const qc = useQueryClient();
  const [actionError, setActionError] = useState<string | null>(null);

  const snapshot = useQuery({
    queryKey: QUERY_KEY,
    queryFn: ({ signal }) => api.attendantCurrentShift(signal),
    refetchInterval: 30_000,
  });
  const shiftID = snapshot.data?.shift?.id ?? '';

  const checkOut = useMutation({
    mutationFn: () => api.checkOutOfShift(shiftID),
    onSuccess: () => {
      setActionError(null);
      toast.success('Checked out', 'Thanks for your shift — see you next time.');
    },
    onError: (e) =>
      setActionError(
        e instanceof SdkError ? e.message : 'Could not check out. Try again.',
      ),
    onSettled: () => qc.invalidateQueries({ queryKey: QUERY_KEY }),
  });

  if (snapshot.isPending) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-10 rounded-xl" />
        <Skeleton className="h-40 rounded-xl" />
        <Skeleton className="h-40 rounded-xl" />
      </div>
    );
  }
  if (snapshot.isError) {
    return (
      <ErrorState
        title="Couldn't load your shift"
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
          title="No shift to complete"
          description="You are not on a shift right now."
        />
      </div>
    );
  }

  // Honest in-progress state: this screen only summarizes a finished shift.
  if (data.next_action !== 'complete') {
    return (
      <div className="flex flex-col gap-4">
        <BackHome />
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Your shift is not complete yet</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <p className="text-base text-muted-foreground" role="status">
              {data.user_message}
            </p>
            <Button asChild className="h-12 text-base" variant="outline">
              <Link href="/attendant">
                Back to my shift
                <ArrowRight className="size-5" aria-hidden />
              </Link>
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  // Reading verification progress across the attendant's own nozzles.
  const decided = data.assignments.filter((a) =>
    data.readings.some(
      (r) =>
        r.nozzle_id === a.nozzle_id &&
        r.closing_reading != null &&
        r.verification_status != null &&
        r.verification_status !== 'pending',
    ),
  ).length;

  const receipt = data.collection_receipt;
  const checkedIn = data.attendance.status === 'checked_in';

  return (
    <div className="flex flex-col gap-4">
      <BackHome />

      {/* Friendly completion banner */}
      <Card>
        <CardContent className="flex flex-col items-center gap-3 pt-6 text-center">
          <span className="flex size-12 items-center justify-center rounded-full bg-success/15 text-success">
            <PartyPopper className="size-6" aria-hidden />
          </span>
          <p className="text-lg font-semibold" role="status">
            Shift complete — well done!
          </p>
          <p className="text-base text-muted-foreground">{data.user_message}</p>
          <Badge
            tone={data.shift.status === 'approved' ? 'success' : 'info'}
            className="capitalize"
          >
            Shift {data.shift.status}
          </Badge>
        </CardContent>
      </Card>

      {/* Readings status */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="flex items-center justify-between gap-2 text-base">
            Readings
            <Badge tone="success">Verified</Badge>
          </CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-1.5">
          <p className="text-base text-muted-foreground" role="status">
            {decided} of {data.assignments.length} closing readings verified by your supervisor.
          </p>
          <Link
            href="/attendant/review-status"
            className="text-base font-medium underline-offset-2 hover:underline"
          >
            View reading details
          </Link>
        </CardContent>
      </Card>

      {/* Collections status */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="flex items-center justify-between gap-2 text-base">
            Collections
            {receipt ? (
              receipt.status === 'approved_with_difference' ? (
                <Badge tone="warning">Approved with difference</Badge>
              ) : (
                <Badge tone="success">Received</Badge>
              )
            ) : (
              <Badge tone="success">Submitted</Badge>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-1.5">
          {data.expected_cash != null ? (
            <Row label="Expected" value={formatMoney(data.expected_cash)} />
          ) : null}
          {data.cash_submission ? (
            <Row label="You submitted" value={formatMoney(data.cash_submission.submitted_total)} />
          ) : null}
          {receipt ? (
            <>
              <Row label="Supervisor received" value={formatMoney(receipt.supervisor_received_total)} />
              <Row label="Difference" value={formatMoney(receipt.difference)} />
              {receipt.reason ? (
                <p className="text-sm text-muted-foreground">Supervisor reason: {receipt.reason}</p>
              ) : null}
            </>
          ) : null}
          <Link
            href="/attendant/collections"
            className="text-base font-medium underline-offset-2 hover:underline"
          >
            View collection details
          </Link>
        </CardContent>
      </Card>

      {actionError ? (
        <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
          {actionError}
        </p>
      ) : null}

      {/* Check-out (Phase 0 endpoint) — only while still checked in */}
      {checkedIn ? (
        <Button
          className="h-14 text-lg"
          disabled={checkOut.isPending}
          onClick={() => checkOut.mutate()}
        >
          {checkOut.isPending ? <Loader2 className="size-5 animate-spin" aria-hidden /> : null}
          Check out
        </Button>
      ) : data.attendance.status === 'checked_out' ? (
        <p
          className="flex items-center justify-center gap-2 rounded-md bg-success/10 px-3 py-3 text-base font-medium text-success"
          role="status"
        >
          <Check className="size-5" aria-hidden />
          You are checked out
          {data.attendance.check_out_at
            ? ` (${new Date(data.attendance.check_out_at).toLocaleTimeString([], {
                hour: '2-digit',
                minute: '2-digit',
              })})`
            : ''}
        </p>
      ) : null}
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <p className="flex items-center justify-between text-base">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-mono font-medium tabular-nums">{value}</span>
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
