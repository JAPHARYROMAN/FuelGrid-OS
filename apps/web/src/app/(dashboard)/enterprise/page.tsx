'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  AlertTriangle,
  ArrowDownLeft,
  ArrowUpRight,
  CircleDollarSign,
  ClipboardCheck,
  TrendingUp,
} from 'lucide-react';

import { SdkError, type StationRank } from '@fuelgrid/sdk';
import {
  Badge,
  BarChart,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  chartColors,
  EmptyState,
  ErrorState,
  PageHeader,
  Skeleton,
  Stat,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  formatMoney,
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
import { ScopeSwitcher } from '@/components/enterprise/scope-switcher';
import { api } from '@/lib/api';

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
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Enterprise"
        title="Enterprise"
        description="Network revenue, margin, exposure, and station ranking."
        actions={
          <div className="flex items-center gap-2">
            <ScopeSwitcher />
            <PermissionGate permission="enterprise_projection.admin">
              <Button
                variant="secondary"
                disabled={rebuild.isPending}
                onClick={() => rebuild.mutate()}
              >
                {rebuild.isPending ? 'Rebuilding…' : 'Rebuild projections'}
              </Button>
            </PermissionGate>
          </div>
        }
      />

      {overview.isPending ? (
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-[120px] rounded-xl" />
          ))}
        </section>
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
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
          <Stat
            label="Gross revenue"
            value={formatMoney(overview.data.gross_revenue)}
            hint="Network"
            icon={<TrendingUp />}
          />
          <Stat
            label="Margin"
            value={formatMoney(overview.data.margin_total)}
            hint="Network"
            icon={<CircleDollarSign />}
          />
          <Stat
            label="AP outstanding"
            value={formatMoney(overview.data.ap_outstanding)}
            hint="Payables"
            icon={<ArrowUpRight />}
          />
          <Stat
            label="AR outstanding"
            value={formatMoney(overview.data.ar_outstanding)}
            hint="Receivables"
            icon={<ArrowDownLeft />}
          />
          <Stat
            label="Open incidents"
            value={overview.data.open_incidents}
            hint="Across the network"
            icon={<AlertTriangle />}
          />
          <Stat
            label="Approvals waiting"
            value={overview.data.approvals_waiting}
            hint="In the queue"
            icon={<ClipboardCheck />}
          />
        </section>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Station ranking</CardTitle>
          <p className="text-sm text-muted-foreground">Top sites by gross revenue and margin.</p>
        </CardHeader>
        <CardContent>
          {ranking.isPending ? (
            <div className="flex flex-col gap-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-14 rounded-lg" />
              ))}
            </div>
          ) : (ranking.data?.items?.length ?? 0) === 0 ? (
            <EmptyState
              title="No ranked stations yet"
              description="Station rankings appear once projections are built."
              icon={<TrendingUp className="size-8" />}
            />
          ) : (
            <div className="flex flex-col gap-5">
              <BarChart
                data={ranking.data!.items.slice(0, 8)}
                xKey="name"
                layout="vertical"
                series={[
                  { key: 'gross_revenue', label: 'Gross', color: chartColors.accent },
                  { key: 'margin_total', label: 'Margin', color: chartColors.success },
                ]}
                valueFormatter={(v) => formatMoney(v as string)}
                height={Math.max(180, Math.min(8, ranking.data!.items.length) * 44)}
              />
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Rank</TableHead>
                    <TableHead>Station</TableHead>
                    <TableHead className="text-right">Gross</TableHead>
                    <TableHead className="text-right">Margin</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {ranking.data!.items.map((s: StationRank, i: number) => (
                    <TableRow key={s.station_id}>
                      <TableCell>
                        <Badge tone="neutral">#{i + 1}</Badge>
                      </TableCell>
                      <TableCell className="font-medium text-foreground">{s.name}</TableCell>
                      <TableCell className="text-right font-mono font-medium tabular-nums">
                        {formatMoney(s.gross_revenue)}
                      </TableCell>
                      <TableCell className="text-right font-mono tabular-nums text-muted-foreground">
                        {formatMoney(s.margin_total)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
