'use client';

import Link from 'next/link';
import { ArrowLeft, ArrowRight } from 'lucide-react';

import type { AttendantCurrentShift, AttendantReading } from '@fuelgrid/sdk';
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

import { useT } from '@/lib/i18n';
import { subtractMeterDecimals } from '@/lib/meter-decimal';
import { useAttendantSnapshot } from '@/lib/offline';

/**
 * Supervisor review status per nozzle (Mobile Attendant Phase 3, PRD §6.9 /
 * 20.8). Everything comes from the self-scoped workflow snapshot: the
 * attendant's submitted closing, its verification status, and — for corrected
 * readings — BOTH values (the dual-value model: submitted preserved, final
 * approved applied) with the supervisor's reason.
 */
export default function ReviewStatusPage() {
  const t = useT();
  const snapshot = useAttendantSnapshot({
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
  if (snapshot.showError) {
    return (
      <ErrorState
        title={t.common.couldNotLoadShift}
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
  if (!data.shift || data.assignments.length === 0) {
    return (
      <div className="flex flex-col gap-4">
        <BackHome />
        <EmptyState
          title={t.review.emptyTitle}
          description={!data.shift ? t.review.emptyNoShift : t.review.emptyNoAssignments}
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
        <h1 className="text-xl font-semibold leading-tight">{t.review.title}</h1>
        <p className="text-base text-muted-foreground" role="status">
          {t.review.progress(verified.length, rows.length)}
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
                {t.common.pumpNozzle(a.pump_number, a.nozzle_number)}
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
            {t.review.finishClosings}
            <ArrowRight className="size-5" aria-hidden />
          </Link>
        </Button>
      ) : null}
    </div>
  );
}

/** Review status as text + colour, never colour alone (PRD §15.1). */
function StatusBadge({ reading }: { reading?: AttendantReading }) {
  const t = useT();
  if (reading?.closing_reading == null) {
    return <Badge tone="neutral">{t.review.badgeNotSubmitted}</Badge>;
  }
  switch (reading.verification_status) {
    case 'approved':
      return <Badge tone="success">{t.review.badgeApproved}</Badge>;
    case 'corrected':
      return <Badge tone="warning">{t.review.badgeCorrected}</Badge>;
    case 'rejected':
      return <Badge tone="danger">{t.review.badgeRejected}</Badge>;
    case 'flagged':
      return <Badge tone="warning">{t.review.badgeFlagged}</Badge>;
    default:
      return <Badge tone="info">{t.review.badgePending}</Badge>;
  }
}

/**
 * The figures behind the badge. A corrected reading shows BOTH values — what
 * the attendant submitted (preserved forever) and what the supervisor
 * approved — plus the exact difference and the supervisor's reason.
 */
function ReadingOutcome({ reading }: { reading?: AttendantReading }) {
  const t = useT();
  if (reading?.closing_reading == null) {
    return <p className="text-sm text-muted-foreground">{t.review.submitPrompt}</p>;
  }
  const submitted = reading.closing_reading;
  const final = reading.final_reading;
  const corrected = reading.verification_status === 'corrected' && final != null;
  const rejected = reading.verification_status === 'rejected';
  const flagged = reading.verification_status === 'flagged';

  return (
    <div className="flex flex-col gap-1.5 text-base">
      <Row label={t.review.youSubmitted} value={submitted} />
      {corrected ? (
        <>
          <Row label={t.review.supervisorApproved} value={final} />
          <Row label={t.review.difference} value={subtractMeterDecimals(final, submitted)} />
        </>
      ) : null}
      {reading.verification_status === 'approved' && final != null ? (
        <Row label={t.review.approvedReading} value={final} />
      ) : null}

      {/* A rejection is sent back to the attendant: show WHY (supervisor's
          reason) + a clear call to re-capture. The closing-readings screen is
          unlocked again after a rejection (per backend Phase 3 coordination). */}
      {rejected ? (
        <div className="flex flex-col gap-2 rounded-md bg-danger/10 px-3 py-3">
          <p className="text-sm font-medium text-danger">{t.review.rejectedTitle}</p>
          {reading.verification_reason ? (
            <p className="text-sm">
              <span className="font-medium">{t.review.reasonLabel}</span>{' '}
              {reading.verification_reason}
            </p>
          ) : null}
          <p className="text-sm text-muted-foreground">{t.review.rejectedHelp}</p>
          <Button asChild className="h-12 text-base">
            <Link href="/attendant/closing-readings">
              {t.review.resubmitCta}
              <ArrowRight className="size-5" aria-hidden />
            </Link>
          </Button>
        </div>
      ) : flagged ? (
        <div className="flex flex-col gap-1.5 rounded-md bg-warning/10 px-3 py-3">
          {reading.verification_reason ? (
            <p className="text-sm">
              <span className="font-medium">{t.review.reasonLabel}</span>{' '}
              {reading.verification_reason}
            </p>
          ) : null}
          <p className="text-sm text-muted-foreground">{t.review.flaggedHelp}</p>
        </div>
      ) : reading.verification_reason ? (
        <p className="rounded-md bg-muted px-3 py-2 text-sm">
          <span className="font-medium">{t.review.reasonLabel}</span> {reading.verification_reason}
        </p>
      ) : null}

      {reading.verification_status === 'pending' ? (
        <p className="text-sm text-muted-foreground">{t.review.notReviewedYet}</p>
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
