'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { SdkError, type Customer } from '@fuelgrid/sdk';
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

import { PermissionGate } from '@/components/permission-gate';
import { api } from '@/lib/api';

function money(n?: string) {
  if (n == null) return '—';
  const v = Number(n);
  return Number.isFinite(v) ? v.toLocaleString(undefined, { minimumFractionDigits: 2 }) : n;
}

function statusTone(status: string): 'neutral' | 'success' | 'warning' {
  if (status === 'active') return 'success';
  if (status === 'suspended' || status === 'closed') return 'warning';
  return 'neutral';
}

export default function CustomersPage() {
  const qc = useQueryClient();
  const customers = useQuery({
    queryKey: ['customers'],
    queryFn: ({ signal }) => api.listCustomers(signal),
  });
  const alerts = useQuery({
    queryKey: ['credit-alerts', 'open'],
    queryFn: ({ signal }) => api.listCreditAlerts({ status: 'open' }, signal),
  });
  const scan = useMutation({
    mutationFn: () => api.scanCreditAlerts(),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['credit-alerts', 'open'] }),
  });

  return (
    <div className="flex flex-col gap-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold tracking-tight">Customers</h1>
          <p className="text-sm text-muted-foreground">
            Credit accounts, status, and credit-risk alerts.
          </p>
        </div>
        <PermissionGate permission="customer_credit_alert.manage">
          <Button
            size="sm"
            variant="outline"
            disabled={scan.isPending}
            onClick={() => scan.mutate()}
          >
            {scan.isPending ? 'Scanning…' : 'Scan credit alerts'}
          </Button>
        </PermissionGate>
      </header>

      {/* Open credit alerts */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Open credit alerts</CardTitle>
        </CardHeader>
        <CardContent className="text-sm">
          {alerts.isPending ? (
            <LoadingState />
          ) : (alerts.data?.items?.length ?? 0) === 0 ? (
            <EmptyState title="No open alerts" description="No customers are flagged right now." />
          ) : (
            <div className="flex flex-col gap-1.5">
              {alerts.data!.items.map((a) => (
                <div key={a.id} className="flex items-center justify-between gap-2">
                  <span className="text-muted-foreground">{a.alert_type}</span>
                  <span className="flex items-center gap-2">
                    <span>{a.detail}</span>
                    <Badge
                      tone={
                        a.severity === 'high' || a.severity === 'critical' ? 'warning' : 'neutral'
                      }
                    >
                      {a.severity}
                    </Badge>
                  </span>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Customer list */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Accounts</CardTitle>
        </CardHeader>
        <CardContent className="text-sm">
          {customers.isPending ? (
            <LoadingState />
          ) : customers.isError ? (
            <ErrorState
              title="Couldn't load customers"
              description={String((customers.error as Error).message)}
              onRetry={
                customers.error instanceof SdkError && customers.error.status === 403
                  ? undefined
                  : () => customers.refetch()
              }
            />
          ) : (customers.data?.items?.length ?? 0) === 0 ? (
            <EmptyState title="No customers yet" description="Credit customers will appear here." />
          ) : (
            <div className="flex flex-col gap-1.5">
              {customers.data!.items.map((c: Customer) => (
                <div key={c.id} className="flex items-center justify-between gap-2">
                  <span>
                    {c.name} <span className="text-muted-foreground">({c.code})</span>
                  </span>
                  <span className="flex items-center gap-3 tabular-nums">
                    <span className="text-muted-foreground">limit {money(c.credit_limit)}</span>
                    <Badge tone={statusTone(c.status)}>{c.status}</Badge>
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
