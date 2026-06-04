'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { ExternalLink, Lightbulb, ShieldAlert } from 'lucide-react';

import { SdkError, type Insight, type RiskAlert } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  PageHeader,
  RiskAlertCard,
  type RiskSeverity,
  Skeleton,
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
import { api } from '@/lib/api';

const SEVERITIES: RiskSeverity[] = ['critical', 'high', 'medium', 'low', 'info'];

function toRiskSeverity(sev: string): RiskSeverity {
  return (SEVERITIES as string[]).includes(sev) ? (sev as RiskSeverity) : 'info';
}

/** Humanize the alert_type slug for the card title (fallback when no detail). */
function alertTitle(a: RiskAlert): string {
  return a.alert_type.replace(/[_-]+/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

/** Humanize a snake/kebab slug for display. */
function humanize(slug: string): string {
  return slug.replace(/[_-]+/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

function insightTone(sev: string): 'danger' | 'warning' | 'info' {
  if (sev === 'critical' || sev === 'high') return 'danger';
  if (sev === 'medium') return 'warning';
  return 'info';
}

export default function RiskPage() {
  const qc = useQueryClient();
  const overview = useQuery({
    queryKey: ['risk-overview'],
    queryFn: ({ signal }) => api.getRiskOverview(signal),
  });
  const alerts = useQuery({
    queryKey: ['risk-alerts', 'open'],
    queryFn: ({ signal }) => api.listRiskAlerts({ status: 'open' }, signal),
  });
  const insights = useQuery({
    queryKey: ['insights', 'open'],
    queryFn: ({ signal }) => api.listInsights({ status: 'open' }, signal),
  });
  const detect = useMutation({
    mutationFn: () => api.runRiskDetection(),
    onSuccess: async () => {
      await api.recomputeRiskScores();
      qc.invalidateQueries({ queryKey: ['risk-overview'] });
      qc.invalidateQueries({ queryKey: ['risk-alerts', 'open'] });
      qc.invalidateQueries({ queryKey: ['insights', 'open'] });
    },
  });
  const resolve = useMutation({
    mutationFn: (id: string) => api.transitionRiskAlert(id, 'resolve', { disposition: 'reviewed' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['risk-overview'] });
      qc.invalidateQueries({ queryKey: ['risk-alerts', 'open'] });
      qc.invalidateQueries({ queryKey: ['insights', 'open'] });
    },
  });

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Monitor"
        title="Risk"
        description="Open alerts, station risk scores, and detection."
        actions={
          <PermissionGate permission="risk_alert.manage">
            <Button
              size="sm"
              variant="outline"
              disabled={detect.isPending}
              onClick={() => detect.mutate()}
            >
              {detect.isPending ? 'Running…' : 'Run detection'}
            </Button>
          </PermissionGate>
        }
      />

      {overview.isPending ? (
        <Skeleton className="h-[120px] rounded-xl" />
      ) : overview.isError ? (
        <ErrorState
          title={
            overview.error instanceof SdkError && overview.error.status === 403
              ? 'No risk access'
              : "Couldn't load risk overview"
          }
          description={String((overview.error as Error).message)}
          onRetry={
            overview.error instanceof SdkError && overview.error.status === 403
              ? undefined
              : () => overview.refetch()
          }
        />
      ) : (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Open alerts by severity</CardTitle>
          </CardHeader>
          <CardContent className="grid grid-cols-3 gap-3 text-sm md:grid-cols-5">
            {['critical', 'high', 'medium', 'low', 'info'].map((sev) => (
              <div key={sev} className="flex flex-col gap-0.5 rounded-md bg-muted/40 px-3 py-2">
                <span className="text-xs uppercase tracking-wider text-muted-foreground">
                  {sev}
                </span>
                <span className="font-semibold tabular-nums">
                  {overview.data.open_by_severity[sev] ?? 0}
                </span>
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Insights</CardTitle>
        </CardHeader>
        <CardContent className="text-sm">
          {insights.isPending ? (
            <div className="flex flex-col gap-2">
              {Array.from({ length: 2 }).map((_, i) => (
                <Skeleton key={i} className="h-12 rounded-lg" />
              ))}
            </div>
          ) : insights.isError ? (
            <ErrorState
              title={
                insights.error instanceof SdkError && insights.error.status === 403
                  ? 'No insights access'
                  : "Couldn't load insights"
              }
              description={String((insights.error as Error).message)}
              onRetry={
                insights.error instanceof SdkError && insights.error.status === 403
                  ? undefined
                  : () => insights.refetch()
              }
            />
          ) : (insights.data?.items?.length ?? 0) === 0 ? (
            <EmptyState
              title="No insights"
              description="Deterministic, rule-based insights appear here as detection runs."
              icon={<Lightbulb />}
            />
          ) : (
            <div className="flex flex-col gap-3">
              {insights.data!.items.map((ins: Insight) => (
                <div
                  key={ins.id}
                  className="flex flex-col gap-2 rounded-lg border border-border bg-card/40 p-3"
                >
                  <div className="flex items-center gap-2">
                    <Badge tone={insightTone(ins.severity)}>{ins.severity}</Badge>
                    <span className="font-medium text-foreground">{humanize(ins.type)}</span>
                    {ins.rule_code ? (
                      <span className="font-mono text-[11px] text-muted-foreground">
                        {ins.rule_code}
                      </span>
                    ) : null}
                  </div>
                  {ins.detail ? <p className="text-muted-foreground">{ins.detail}</p> : null}
                  {ins.recommended_action ? (
                    <p className="text-xs text-muted-foreground">
                      Recommended: {ins.recommended_action}
                    </p>
                  ) : null}
                  {ins.source ? (
                    <div className="flex items-center gap-2 text-xs">
                      <span className="text-muted-foreground">
                        Source: {humanize(ins.source.kind)}
                      </span>
                      {ins.source.href ? (
                        <Button asChild variant="ghost" size="sm">
                          <a href={ins.source.href}>
                            <ExternalLink className="size-3.5" />
                            View source
                          </a>
                        </Button>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Open alerts</CardTitle>
        </CardHeader>
        <CardContent className="text-sm">
          {alerts.isPending ? (
            <div className="flex flex-col gap-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-12 rounded-lg" />
              ))}
            </div>
          ) : (alerts.data?.items?.length ?? 0) === 0 ? (
            <EmptyState
              title="No open alerts"
              description="Run detection to surface risk."
              icon={<ShieldAlert />}
            />
          ) : (
            <div className="flex flex-col gap-3">
              {alerts.data!.items.map((a: RiskAlert) => (
                <div key={a.id} className="flex flex-col gap-2">
                  <RiskAlertCard
                    severity={toRiskSeverity(a.severity)}
                    title={alertTitle(a)}
                    description={a.detail}
                    metricLabel={a.amount ? 'Amount' : undefined}
                    metricValue={a.amount ?? undefined}
                    recommendedAction={a.recommended_action}
                    station={a.station_id ?? undefined}
                  />
                  <PermissionGate permission="risk_alert.manage">
                    <Button
                      size="sm"
                      variant="outline"
                      className="self-end"
                      disabled={resolve.isPending && resolve.variables === a.id}
                      onClick={() => resolve.mutate(a.id)}
                    >
                      {resolve.isPending && resolve.variables === a.id ? 'Resolving…' : 'Resolve'}
                    </Button>
                  </PermissionGate>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
