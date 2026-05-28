'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

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
  LoadingState,
} from '@fuelgrid/ui';

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
    <div className="flex flex-col gap-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold tracking-tight">Risk</h1>
          <p className="text-sm text-muted-foreground">
            Open alerts, station risk scores, and detection.
          </p>
        </div>
        <Button
          size="sm"
          variant="outline"
          disabled={detect.isPending}
          onClick={() => detect.mutate()}
        >
          {detect.isPending ? 'Running…' : 'Run detection'}
        </Button>
      </header>

      {overview.isPending ? (
        <LoadingState />
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
            <LoadingState />
          ) : (alerts.data?.items?.length ?? 0) === 0 ? (
            <EmptyState title="No open alerts" description="Run detection to surface risk." />
          ) : (
            <div className="flex flex-col gap-2">
              {alerts.data!.items.map((a: RiskAlert) => (
                <div key={a.id} className="flex items-center justify-between gap-2">
                  <span className="flex items-center gap-2">
                    <Badge tone={sevTone(a.severity)}>{a.severity}</Badge>
                    <span>{a.alert_type}</span>
                    <span className="text-muted-foreground">{a.detail}</span>
                  </span>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={resolve.isPending}
                    onClick={() => resolve.mutate(a.id)}
                  >
                    Resolve
                  </Button>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
