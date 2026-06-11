'use client';

import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { ArrowLeft, ArrowRight } from 'lucide-react';

import type { AttendantReading } from '@fuelgrid/sdk';
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
import { subtractMeterDecimals } from '@/lib/meter-decimal';

const QUERY_KEY = ['attendant-current-shift'];

/**
 * Supervisor review status per nozzle (Mobile Attendant Phase 3, PRD §6.9 /
 * 20.8). Everything comes from the self-scoped workflow snapshot: the
 * attendant's submitted closing, its verification status, and — for corrected
 * readings — BOTH values (the dual-value model: submitted preserved, final
 * approved applied) with the supervisor's reason.
 */
export default function ReviewStatusPage() {
  const snapshot = useQuery({
    queryKey: QUERY_KEY,
    queryFn: ({ signal }) => api.attendantCurrentShift(signal),
    // Supervisor decisions land outside this screen — keep it live.
    refetchInterval: 30_000,
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
  if (!data.shift || data.assignments.length === 0) {
    return (
      <div className="flex flex-col gap-4">
        <BackHome />
        <EmptyState
          title="No readings to review"
          description={
            !data.shift
              ? 'You are not on a shift, so there are no submitted readings to track.'
              : 'No nozzles are assigned to you on this shift.'
          }
        />
      </div>
    );
  }

  const readingByNozzle = new Map(data.readings.map((r) => [r.nozzle_id, r]));
  const rows = data.assignments.map((a) => ({
    assignment: a,
    reading: readingByNozzle.get(a.nozzle_id),
  }));
  const submitted = rows.filter((r) => r.reading?.closing_reading != null);
  const verified = submitted.filter(
    (r) => r.reading?.verification_status && r.reading.verification_status !== 'pending',
  );

  return (
    <div className="flex flex-col gap-4">
      <BackHome />

      <div>
        <h1 className="text-xl font-semibold leading-tight">Reading review status</h1>
        <p className="text-base text-muted-foreground" role="status">
          {verified.length} of {rows.length} readings verified by your supervisor.
        </p>
      </div>

      {rows.map(({ assignment: a, reading }) => (
        <Card key={a.assignment_id}>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center justify-between gap-2 text-base">
              <span className="flex items-center gap-2">
                <span
                  className="inline-block size-3 rounded-full border border-border"
                  style={{ backgroundColor: a.product_color }}
                  aria-hidden
                />
                {a.product_name}
              </span>
              <span className="font-mono text-xs font-normal text-muted-foreground">
                Pump {a.pump_number} · Nozzle {a.nozzle_number}
              </span>
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-2.5">
            <div>
              <StatusBadge reading={reading} />
            </div>
            <ReadingOutcome reading={reading} />
          </CardContent>
        </Card>
      ))}

      {submitted.length < rows.length ? (
        <Button asChild className="h-14 text-lg">
          <Link href="/attendant/closing-readings">
            Finish closing readings
            <ArrowRight className="size-5" aria-hidden />
          </Link>
        </Button>
      ) : null}
    </div>
  );
}

/** Review status as text + colour, never colour alone (PRD §15.1). */
function StatusBadge({ reading }: { reading?: AttendantReading }) {
  if (reading?.closing_reading == null) {
    return <Badge tone="neutral">Not submitted yet</Badge>;
  }
  switch (reading.verification_status) {
    case 'approved':
      return <Badge tone="success">Approved</Badge>;
    case 'corrected':
      return <Badge tone="warning">Corrected by supervisor</Badge>;
    case 'rejected':
      return <Badge tone="danger">Rejected</Badge>;
    default:
      return <Badge tone="info">Pending supervisor review</Badge>;
  }
}

/**
 * The figures behind the badge. A corrected reading shows BOTH values — what
 * the attendant submitted (preserved forever) and what the supervisor
 * approved — plus the exact difference and the supervisor's reason.
 */
function ReadingOutcome({ reading }: { reading?: AttendantReading }) {
  if (reading?.closing_reading == null) {
    return (
      <p className="text-sm text-muted-foreground">
        Submit this nozzle's closing reading to start the review.
      </p>
    );
  }
  const submitted = reading.closing_reading;
  const final = reading.final_reading;
  const corrected = reading.verification_status === 'corrected' && final != null;

  return (
    <div className="flex flex-col gap-1.5 text-base">
      <Row label="You submitted" value={submitted} />
      {corrected ? (
        <>
          <Row label="Supervisor approved" value={final} />
          <Row label="Difference" value={subtractMeterDecimals(final, submitted)} />
        </>
      ) : null}
      {reading.verification_status === 'approved' && final != null ? (
        <Row label="Approved reading" value={final} />
      ) : null}
      {reading.verification_reason ? (
        <p className="rounded-md bg-muted px-3 py-2 text-sm">
          <span className="font-medium">Reason:</span> {reading.verification_reason}
        </p>
      ) : null}
      {reading.verification_status === 'pending' ? (
        <p className="text-sm text-muted-foreground">
          Your supervisor has not reviewed this reading yet.
        </p>
      ) : null}
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <p className="flex items-center justify-between">
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
