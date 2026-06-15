'use client';

import { useMemo } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Sliders } from 'lucide-react';

import { SdkError, type ReportRule } from '@fuelgrid/sdk';
import {
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  PageHeader,
  RiskBadge,
  Skeleton,
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
import { usePermission } from '@/hooks/use-permissions';
import { api } from '@/lib/api';
import { toast } from '@/lib/toast';

// Report Insight Rules management surface (Reports Center Phase 15). The
// config-driven, deterministic engine that AUGMENTS the report composers. A
// system rule seeds in "shadow" mode (evaluated for preview, but the composer
// stays the source of truth) — flip it to "augment" to fold its line into the
// report, or disable it entirely. Mirrors the Automation (risk rules) page.

const CATEGORY_LABEL: Record<string, string> = {
  sales: 'Sales',
  cash: 'Cash',
  inventory: 'Inventory',
  credit: 'Credit',
  procurement: 'Procurement',
  risk: 'Risk',
  executive: 'Executive',
  general: 'General',
};
const CATEGORY_ORDER = [
  'sales',
  'cash',
  'inventory',
  'credit',
  'procurement',
  'risk',
  'executive',
  'general',
];

function toRiskSeverity(sev: string): 'low' | 'medium' | 'high' | 'critical' {
  switch (sev) {
    case 'critical':
      return 'critical';
    case 'warning':
      return 'high';
    default:
      return 'low';
  }
}

export default function ReportRulesPage() {
  const qc = useQueryClient();
  const canManage = usePermission('reports.rules.manage');

  const rules = useQuery({
    queryKey: ['report-rules'],
    queryFn: ({ signal }) => api.listReportRules({}, signal),
  });

  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      api.setReportRuleEnabled(id, enabled),
    onSuccess: (_res, vars) => {
      qc.invalidateQueries({ queryKey: ['report-rules'] });
      toast.success(vars.enabled ? 'Rule enabled' : 'Rule disabled');
    },
    onError: (err) =>
      toast.error('Could not change rule', err instanceof SdkError ? err.message : 'Try again.'),
  });

  const setMode = useMutation({
    mutationFn: ({ id, mode }: { id: string; mode: 'shadow' | 'augment' }) =>
      api.updateReportRule(id, { mode }),
    onSuccess: (_res, vars) => {
      qc.invalidateQueries({ queryKey: ['report-rules'] });
      toast.success(
        vars.mode === 'augment' ? 'Rule now drives report insights' : 'Rule set to preview-only',
      );
    },
    onError: (err) =>
      toast.error('Could not change mode', err instanceof SdkError ? err.message : 'Try again.'),
  });

  const grouped = useMemo(() => {
    const items = rules.data?.items ?? [];
    const map = new Map<string, ReportRule[]>();
    for (const rule of items) {
      const cat = CATEGORY_LABEL[rule.category] ? rule.category : 'general';
      const bucket = map.get(cat) ?? [];
      bucket.push(rule);
      map.set(cat, bucket);
    }
    return CATEGORY_ORDER.map((cat) => ({ category: cat, rules: map.get(cat) ?? [] })).filter(
      (g) => g.rules.length > 0,
    );
  }, [rules.data]);

  const total = rules.data?.count ?? rules.data?.items?.length ?? 0;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports"
        title="Insight Rules"
        description="Deterministic rules drive the insights and data-quality notes on every report — no AI. System rules mirror the built-in thresholds; tune one, or flip it to Augment to fold its line into the report."
      />

      {rules.isPending ? (
        <div className="flex flex-col gap-4">
          {Array.from({ length: 2 }).map((_, i) => (
            <Skeleton key={i} className="h-40 rounded-xl" />
          ))}
        </div>
      ) : rules.isError ? (
        <ErrorState
          title={
            rules.error instanceof SdkError && rules.error.status === 403
              ? 'No rules access'
              : "Couldn't load rules"
          }
          description={String((rules.error as Error).message)}
          onRetry={
            rules.error instanceof SdkError && rules.error.status === 403
              ? undefined
              : () => rules.refetch()
          }
        />
      ) : total === 0 ? (
        <EmptyState
          title="No report rules"
          description="System rules are seeded per tenant; if none appear, contact your administrator."
          icon={<Sliders />}
        />
      ) : (
        <div className="flex flex-col gap-5">
          {grouped.map((group) => (
            <Card key={group.category}>
              <CardHeader className="flex-row items-center justify-between">
                <CardTitle className="text-base">{CATEGORY_LABEL[group.category]}</CardTitle>
                <span className="text-xs text-muted-foreground">
                  {group.rules.length} {group.rules.length === 1 ? 'rule' : 'rules'}
                </span>
              </CardHeader>
              <CardContent className="flex flex-col divide-y divide-border/60 p-0">
                {group.rules.map((rule) => (
                  <div
                    key={rule.id}
                    className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between"
                    data-testid="report-rule-row"
                  >
                    <div className="flex min-w-0 flex-col gap-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-medium">{rule.name}</span>
                        <span className="font-mono text-xs text-muted-foreground">{rule.code}</span>
                        <RiskBadge severity={toRiskSeverity(rule.severity)} />
                        <span
                          className="rounded border border-border/60 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground"
                          data-testid="report-rule-mode"
                        >
                          {rule.mode === 'augment' ? 'Augment' : 'Preview'}
                        </span>
                        {!rule.enabled ? (
                          <span className="text-xs text-muted-foreground">(disabled)</span>
                        ) : null}
                      </div>
                      <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
                        <span>{rule.report_key ?? 'all reports'}</span>
                        <span className="font-mono">{rule.condition}</span>
                        {rule.threshold ? (
                          <span className="font-mono tabular-nums">threshold {rule.threshold}</span>
                        ) : null}
                      </div>
                    </div>

                    <PermissionGate permission="reports.rules.manage" mode="hide">
                      <div className="flex shrink-0 items-center gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={setMode.isPending || canManage !== true}
                          onClick={() =>
                            setMode.mutate({
                              id: rule.id,
                              mode: rule.mode === 'augment' ? 'shadow' : 'augment',
                            })
                          }
                        >
                          {rule.mode === 'augment' ? 'Set preview-only' : 'Drive insights'}
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={toggle.isPending || canManage !== true}
                          onClick={() => toggle.mutate({ id: rule.id, enabled: !rule.enabled })}
                        >
                          {rule.enabled ? 'Disable' : 'Enable'}
                        </Button>
                      </div>
                    </PermissionGate>
                  </div>
                ))}
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
