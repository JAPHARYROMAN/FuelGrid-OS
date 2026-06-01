'use client';

import * as React from 'react';
import { AlertTriangle, Info, ShieldAlert } from 'lucide-react';

import type { ReportInsights as ReportInsightsData, InsightSeverity } from '@fuelgrid/sdk';
import { Card, CardContent, CardHeader, CardTitle, Skeleton } from '@fuelgrid/ui';

const SEVERITY_META: Record<
  InsightSeverity,
  { tone: string; ring: string; icon: React.ReactNode; label: string }
> = {
  info: {
    tone: 'text-accent',
    ring: 'border-accent/30 bg-accent-muted/40',
    icon: <Info className="size-4" />,
    label: 'Insight',
  },
  warning: {
    tone: 'text-warning',
    ring: 'border-warning/30 bg-warning/10',
    icon: <AlertTriangle className="size-4" />,
    label: 'Warning',
  },
  critical: {
    tone: 'text-danger',
    ring: 'border-danger/30 bg-danger/10',
    icon: <ShieldAlert className="size-4" />,
    label: 'Critical',
  },
};

/**
 * Renders the deterministic insight cards + a data-quality banner for a report
 * view. All figures are derived server-side by the reporting package; this is a
 * pure presentation of {insights, data_quality}.
 */
export function ReportInsightsPanel({
  data,
  isPending,
  isError,
}: {
  data?: ReportInsightsData;
  isPending: boolean;
  isError: boolean;
}) {
  if (isPending) {
    return <Skeleton className="h-24 rounded-xl" />;
  }
  if (isError || !data) {
    return null; // insights are advisory — never block the report on their failure
  }

  const { insights, data_quality: dataQuality } = data;
  if (insights.length === 0 && dataQuality.length === 0) {
    return null;
  }

  return (
    <div className="flex flex-col gap-4">
      {dataQuality.length > 0 ? (
        <DataQualityBanner messages={dataQuality.map((d) => d.message)} />
      ) : null}
      {insights.length > 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>Insights</CardTitle>
            <p className="text-sm text-muted-foreground">
              Deterministic observations derived from this report&apos;s figures.
            </p>
          </CardHeader>
          <CardContent className="flex flex-col gap-2.5">
            {insights.map((ins, i) => {
              const meta = SEVERITY_META[ins.severity];
              return (
                <div
                  key={i}
                  className={`flex items-start gap-3 rounded-lg border px-3.5 py-3 ${meta.ring}`}
                >
                  <span className={`mt-0.5 shrink-0 ${meta.tone}`}>{meta.icon}</span>
                  <div className="flex min-w-0 flex-col gap-0.5">
                    <span className="text-sm text-foreground">{ins.message}</span>
                    {ins.recommended_action ? (
                      <span className="text-xs text-muted-foreground">
                        Recommended: {ins.recommended_action}
                      </span>
                    ) : null}
                  </div>
                </div>
              );
            })}
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}

/** A prominent banner that warns figures may be incomplete or provisional. */
export function DataQualityBanner({ messages }: { messages: string[] }) {
  if (messages.length === 0) return null;
  return (
    <div
      className="flex items-start gap-3 rounded-xl border border-warning/40 bg-warning/10 px-4 py-3"
      role="status"
    >
      <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" />
      <div className="flex flex-col gap-1">
        <span className="text-sm font-medium text-foreground">Data quality</span>
        <ul className="flex flex-col gap-0.5">
          {messages.map((m, i) => (
            <li key={i} className="text-xs text-muted-foreground">
              {m}
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
