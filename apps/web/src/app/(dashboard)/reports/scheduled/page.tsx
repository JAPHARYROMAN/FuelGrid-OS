'use client';

import { CalendarClock, FileText, Mail } from 'lucide-react';

import { Badge, Card, CardContent, CardHeader, CardTitle, PageHeader } from '@fuelgrid/ui';

/**
 * Scheduled digests — a light info page reflecting the canned report digests the
 * platform emails automatically. These run on a fixed cadence today; per-tenant
 * scheduling is a future enhancement.
 */

interface Digest {
  title: string;
  cadence: string;
  description: string;
  icon: React.ReactNode;
}

const DIGESTS: Digest[] = [
  {
    title: 'Daily close digest',
    cadence: 'Every morning',
    description:
      'A summary of the previous operating day per station: gross revenue, tendered total, cash variance and any open exceptions, emailed to station and finance recipients.',
    icon: <FileText className="size-4" />,
  },
  {
    title: 'Monthly P&L digest',
    cadence: 'First of the month',
    description:
      'The prior month profit & loss and balance-sheet headline, with links to the full accountant-ready statements in the export center.',
    icon: <FileText className="size-4" />,
  },
];

export default function ScheduledDigestsPage() {
  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Scheduled"
        title="Scheduled digests"
        description="Canned report digests the platform sends automatically. Configurable per-tenant scheduling is on the roadmap."
      />

      <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
        {DIGESTS.map((d) => (
          <Card key={d.title} className="flex flex-col">
            <CardHeader className="flex-row items-start gap-3 space-y-0">
              <span className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">
                {d.icon}
              </span>
              <div className="flex min-w-0 flex-col">
                <div className="flex items-center gap-2">
                  <CardTitle>{d.title}</CardTitle>
                  <Badge tone="neutral">
                    <CalendarClock className="mr-1 inline size-3" />
                    {d.cadence}
                  </Badge>
                </div>
                <p className="text-sm text-muted-foreground">{d.description}</p>
              </div>
            </CardHeader>
            <CardContent className="mt-auto">
              <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                <Mail className="size-3.5" />
                Delivered by email to the configured recipients.
              </span>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  );
}
