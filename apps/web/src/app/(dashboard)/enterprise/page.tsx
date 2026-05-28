'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { SdkError, type StationRank } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  ErrorState,
  LoadingState,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

function money(n?: string) {
  if (n == null) return '—';
  const v = Number(n);
  return Number.isFinite(v) ? v.toLocaleString(undefined, { minimumFractionDigits: 2 }) : n;
}

export default function EnterprisePage() {
  const qc = useQueryClient();
  const overview = useQuery({
    queryKey: ['enterprise-overview'],
    queryFn: ({ signal }) => api.getEnterpriseOverview({}, signal),
  });
  const ranking = useQuery({
    queryKey: ['enterprise-ranking'],
    queryFn: ({ signal }) => api.getStationRanking({}, signal),
  });
  const rebuild = useMutation({
    mutationFn: () => api.rebuildEnterpriseProjections(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['enterprise-overview'] });
      qc.invalidateQueries({ queryKey: ['enterprise-ranking'] });
    },
  });

  return (
    <div className="flex flex-col gap-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold tracking-tight">Enterprise</h1>
          <p className="text-sm text-muted-foreground">
            Network revenue, margin, exposure, and station ranking.
          </p>
        </div>
        <Button
          size="sm"
          variant="outline"
          disabled={rebuild.isPending}
          onClick={() => rebuild.mutate()}
        >
          {rebuild.isPending ? 'Rebuilding…' : 'Rebuild projections'}
        </Button>
      </header>

      {overview.isPending ? (
        <LoadingState />
      ) : overview.isError ? (
        <ErrorState
          title={
            overview.error instanceof SdkError && overview.error.status === 403
              ? 'No enterprise access'
              : "Couldn't load enterprise overview"
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
            <CardTitle className="text-base">Network overview</CardTitle>
          </CardHeader>
          <CardContent className="grid grid-cols-2 gap-3 text-sm md:grid-cols-4">
            <Metric label="Gross revenue" value={money(overview.data.gross_revenue)} />
            <Metric label="Margin" value={money(overview.data.margin_total)} />
            <Metric label="AP outstanding" value={money(overview.data.ap_outstanding)} />
            <Metric label="AR outstanding" value={money(overview.data.ar_outstanding)} />
            <Metric label="Open incidents" value={String(overview.data.open_incidents)} />
            <Metric label="Approvals waiting" value={String(overview.data.approvals_waiting)} />
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Station ranking</CardTitle>
        </CardHeader>
        <CardContent className="text-sm">
          {ranking.isPending ? (
            <LoadingState />
          ) : (ranking.data?.items?.length ?? 0) === 0 ? (
            <p className="text-muted-foreground">No ranked stations yet.</p>
          ) : (
            <div className="flex flex-col gap-1.5">
              {ranking.data!.items.map((s: StationRank, i: number) => (
                <div key={s.station_id} className="flex items-center justify-between gap-2">
                  <span>
                    <Badge tone="neutral">#{i + 1}</Badge> {s.name}
                  </span>
                  <span className="flex items-center gap-3 tabular-nums">
                    <span>gross {money(s.gross_revenue)}</span>
                    <span className="text-muted-foreground">margin {money(s.margin_total)}</span>
                  </span>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5 rounded-md bg-muted/40 px-3 py-2">
      <span className="text-xs uppercase tracking-wider text-muted-foreground">{label}</span>
      <span className="font-semibold tabular-nums">{value}</span>
    </div>
  );
}
