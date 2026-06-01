'use client';

import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { AlertCircle, CreditCard, ShieldAlert } from 'lucide-react';

import type { CreditAlert, RiskAlert, Station } from '@fuelgrid/sdk';
import {
  Badge,
  Card,
  CardContent,
  DataTable,
  EmptyState,
  ErrorState,
  PageHeader,
  Skeleton,
  Stat,
  type DataTableColumn,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

type AlertSource = 'risk' | 'credit';

interface AlertRow {
  id: string;
  source: AlertSource;
  type: string;
  severity: string;
  status: string;
  detail: string;
  context: string;
}

// Severity ranks for sorting (higher = more urgent).
const SEVERITY_RANK: Record<string, number> = {
  critical: 5,
  high: 4,
  medium: 3,
  low: 2,
  info: 1,
};

function sevTone(sev: string): 'danger' | 'warning' | 'neutral' {
  if (sev === 'critical' || sev === 'high') return 'danger';
  if (sev === 'medium') return 'warning';
  return 'neutral';
}

export default function AlertsPage() {
  const riskAlerts = useQuery({
    queryKey: ['risk-alerts', 'open'],
    queryFn: ({ signal }) => api.listRiskAlerts({ status: 'open' }, signal),
  });
  const creditAlerts = useQuery({
    queryKey: ['credit-alerts', 'open'],
    queryFn: ({ signal }) => api.listCreditAlerts({ status: 'open' }, signal),
  });
  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  const customers = useQuery({
    queryKey: ['customers'],
    queryFn: ({ signal }) => api.listCustomers(signal),
  });

  const stationLookup = useMemo(
    () => new Map<string, Station>((stations.data?.items ?? []).map((s) => [s.id, s])),
    [stations.data],
  );
  const customerLookup = useMemo(
    () => new Map((customers.data?.items ?? []).map((c) => [c.id, c])),
    [customers.data],
  );

  const rows = useMemo<AlertRow[]>(() => {
    const risk: AlertRow[] = (riskAlerts.data?.items ?? []).map((a: RiskAlert) => ({
      id: `risk:${a.id}`,
      source: 'risk',
      type: a.alert_type,
      severity: a.severity,
      status: a.status,
      detail: a.detail ?? '—',
      context: a.station_id ? (stationLookup.get(a.station_id)?.name ?? a.station_id) : 'Network',
    }));
    const credit: AlertRow[] = (creditAlerts.data?.items ?? []).map((a: CreditAlert) => ({
      id: `credit:${a.id}`,
      source: 'credit',
      type: a.alert_type,
      severity: a.severity,
      status: a.status,
      detail: a.detail ?? '—',
      context: customerLookup.get(a.customer_id)?.name ?? a.customer_id,
    }));
    return [...risk, ...credit];
  }, [riskAlerts.data, creditAlerts.data, stationLookup, customerLookup]);

  const loading = riskAlerts.isPending || creditAlerts.isPending;
  const isError = riskAlerts.isError && creditAlerts.isError;

  const criticalCount = rows.filter(
    (r) => r.severity === 'critical' || r.severity === 'high',
  ).length;

  const columns: DataTableColumn<AlertRow>[] = [
    {
      id: 'severity',
      header: 'Severity',
      sortValue: (r) => SEVERITY_RANK[r.severity] ?? 0,
      cell: (r) => <Badge tone={sevTone(r.severity)}>{r.severity}</Badge>,
    },
    {
      id: 'source',
      header: 'Source',
      sortValue: (r) => r.source,
      cell: (r) => (
        <span className="inline-flex items-center gap-1.5 text-sm">
          {r.source === 'risk' ? (
            <ShieldAlert className="size-4 text-muted-foreground" />
          ) : (
            <CreditCard className="size-4 text-muted-foreground" />
          )}
          {r.source === 'risk' ? 'Risk' : 'Credit'}
        </span>
      ),
    },
    {
      id: 'type',
      header: 'Type',
      sortValue: (r) => r.type,
      cell: (r) => <span className="font-medium text-foreground">{r.type}</span>,
    },
    {
      id: 'context',
      header: 'Context',
      sortValue: (r) => r.context,
      cell: (r) => <span className="text-foreground">{r.context}</span>,
    },
    {
      id: 'detail',
      header: 'Detail',
      cell: (r) => <span className="text-muted-foreground">{r.detail}</span>,
    },
  ];

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Monitoring"
        title="Alerts"
        description="A single feed of open risk and credit alerts across the network, ordered by severity."
      />

      {isError ? (
        <ErrorState
          title="Couldn't load alerts"
          description={String(
            ((riskAlerts.error ?? creditAlerts.error) as Error | undefined)?.message ??
              'Unknown error',
          )}
          onRetry={() => {
            riskAlerts.refetch();
            creditAlerts.refetch();
          }}
        />
      ) : loading ? (
        <>
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-[120px] rounded-xl" />
            ))}
          </section>
          <Card>
            <CardContent className="flex flex-col gap-2 p-4">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-12 rounded-lg" />
              ))}
            </CardContent>
          </Card>
        </>
      ) : rows.length === 0 ? (
        <EmptyState
          title="No open alerts"
          description="Risk and credit monitoring are all clear."
          icon={<AlertCircle />}
        />
      ) : (
        <>
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <Stat
              label="Open alerts"
              value={rows.length}
              hint="risk + credit"
              icon={<AlertCircle />}
            />
            <Stat
              label="Critical / high"
              value={criticalCount}
              hint="need attention"
              icon={<ShieldAlert />}
            />
            <Stat
              label="Credit"
              value={creditAlerts.data?.items.length ?? 0}
              hint="customer exposure"
              icon={<CreditCard />}
            />
          </section>

          <Card>
            <CardContent className="max-h-[640px] overflow-auto p-0">
              <DataTable
                columns={columns}
                rows={rows}
                rowKey={(r) => r.id}
                defaultSort={{ columnId: 'severity', direction: 'desc' }}
              />
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
