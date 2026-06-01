'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { ShieldAlert } from 'lucide-react';

import { SdkError, type RiskAlert } from '@fuelgrid/sdk';
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
  Skeleton,
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
import { api } from '@/lib/api';

function sevTone(sev: string): 'neutral' | 'warning' {
  return sev === 'high' || sev === 'critical' ? 'warning' : 'neutral';
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
  const detect = useMutation({
    mutationFn: () => api.runRiskDetection(),
    onSuccess: async () => {
      await api.recomputeRiskScores();
      qc.invalidateQueries({ queryKey: ['risk-overview'] });
      qc.invalidateQueries({ queryKey: ['risk-alerts', 'open'] });
    },
  });
  const resolve = useMutation({
    mutationFn: (id: string) => api.transitionRiskAlert(id, 'resolve', { disposition: 'reviewed' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['risk-overview'] });
      qc.invalidateQueries({ queryKey: ['risk-alerts', 'open'] });
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
            <div className="flex flex-col gap-2">
              {alerts.data!.items.map((a: RiskAlert) => (
                <div key={a.id} className="flex items-center justify-between gap-2">
                  <span className="flex items-center gap-2">
                    <Badge tone={sevTone(a.severity)}>{a.severity}</Badge>
                    <span>{a.alert_type}</span>
                    <span className="text-muted-foreground">{a.detail}</span>
                  </span>
                  <PermissionGate permission="risk_alert.manage">
                    <Button
                      size="sm"
                      variant="outline"
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
