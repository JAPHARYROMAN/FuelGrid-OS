'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ShieldAlert, Users } from 'lucide-react';

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
  PageHeader,
  Skeleton,
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';

function money(n?: string) {
  return formatMoney(n);
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
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Commerce"
        title="Customers"
        description="Credit accounts, status, and credit-risk alerts."
        actions={
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
        }
      />

      {/* Open credit alerts */}
      <Card>
        <CardHeader>
          <CardTitle>Open credit alerts</CardTitle>
        </CardHeader>
        <CardContent>
          {alerts.isPending ? (
            <div className="flex flex-col gap-2">
              {Array.from({ length: 2 }).map((_, i) => (
                <Skeleton key={i} className="h-14 rounded-lg" />
              ))}
            </div>
          ) : (alerts.data?.items?.length ?? 0) === 0 ? (
            <EmptyState
              title="No open alerts"
              description="No customers are flagged right now."
              icon={<ShieldAlert />}
            />
          ) : (
            <div className="flex flex-col gap-1">
              {alerts.data!.items.map((a) => (
                <div
                  key={a.id}
                  className="-mx-2 flex items-center justify-between gap-3 rounded-lg px-2 py-2.5"
                >
                  <span className="text-sm text-muted-foreground">{a.alert_type}</span>
                  <span className="flex items-center gap-2">
                    <span className="text-sm text-foreground">{a.detail}</span>
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
          <CardTitle>Accounts</CardTitle>
        </CardHeader>
        <CardContent>
          {customers.isPending ? (
            <div className="flex flex-col gap-2">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-14 rounded-lg" />
              ))}
            </div>
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
            <EmptyState
              title="No customers yet"
              description="Credit customers will appear here."
              icon={<Users />}
            />
          ) : (
            <div className="flex flex-col gap-1">
              {customers.data!.items.map((c: Customer) => (
                <div
                  key={c.id}
                  className="-mx-2 flex items-center justify-between gap-3 rounded-lg px-2 py-2.5"
                >
                  <span className="text-sm text-foreground">
                    {c.name} <span className="text-muted-foreground">({c.code})</span>
                  </span>
                  <span className="flex items-center gap-3">
                    <span className="font-mono text-sm tabular-nums text-muted-foreground">
                      limit {money(c.credit_limit)}
                    </span>
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
