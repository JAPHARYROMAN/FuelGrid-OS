'use client';

import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus, Sparkles } from 'lucide-react';

import { SdkError, type RiskRule } from '@fuelgrid/sdk';
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

import { RuleDialog } from './_rule-dialog';
import {
  CATEGORY_LABEL,
  CATEGORY_ORDER,
  type RuleCategory,
  conditionLabel,
  normalizeCategory,
  toRiskSeverity,
} from './_rules';

export default function AutomationPage() {
  const qc = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<RiskRule | null>(null);
  const canManage = usePermission('risk_rule.manage');

  const rules = useQuery({
    queryKey: ['risk-rules'],
    queryFn: ({ signal }) => api.listRiskRules(signal),
  });

  const detect = useMutation({
    mutationFn: () => api.runRiskDetection(),
    onSuccess: (res) => {
      const n = res.alerts_created;
      toast.success(
        'Detection complete',
        n === 0 ? 'No new alerts were raised.' : `${n} ${n === 1 ? 'alert' : 'alerts'} created.`,
      );
      qc.invalidateQueries({ queryKey: ['risk-overview'] });
      qc.invalidateQueries({ queryKey: ['risk-alerts', 'open'] });
    },
    onError: (err) =>
      toast.error('Detection failed', err instanceof SdkError ? err.message : 'Please try again.'),
  });

  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      api.setRiskRuleEnabled(id, enabled),
    onSuccess: (_res, vars) => {
      qc.invalidateQueries({ queryKey: ['risk-rules'] });
      toast.success(vars.enabled ? 'Rule enabled' : 'Rule disabled');
    },
    onError: (err) =>
      toast.error(
        'Could not change rule',
        err instanceof SdkError ? err.message : 'Please try again.',
      ),
  });

  const grouped = useMemo(() => {
    const items = rules.data?.items ?? [];
    const map = new Map<RuleCategory, RiskRule[]>();
    for (const rule of items) {
      const cat = normalizeCategory(rule.category);
      const bucket = map.get(cat) ?? [];
      bucket.push(rule);
      map.set(cat, bucket);
    }
    return CATEGORY_ORDER.map((cat) => ({ category: cat, rules: map.get(cat) ?? [] })).filter(
      (g) => g.rules.length > 0,
    );
  }, [rules.data]);

  function openCreate() {
    setEditing(null);
    setDialogOpen(true);
  }

  function openEdit(rule: RiskRule) {
    setEditing(rule);
    setDialogOpen(true);
  }

  const totalRules = rules.data?.count ?? rules.data?.items?.length ?? 0;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Monitor"
        title="Automation & Rules"
        description="Deterministic rules watch inventory, cash, and procurement and raise explained alerts — no guesswork, just thresholds you control."
        actions={
          <div className="flex items-center gap-2">
            <PermissionGate permission="risk_rule.manage" mode="hide">
              <Button variant="outline" size="sm" onClick={openCreate}>
                <Plus className="size-4" />
                New rule
              </Button>
            </PermissionGate>
            <PermissionGate permission="risk_alert.manage">
              <Button size="sm" disabled={detect.isPending} onClick={() => detect.mutate()}>
                <Sparkles className="size-4" />
                {detect.isPending ? 'Running…' : 'Run detection'}
              </Button>
            </PermissionGate>
          </div>
        }
      />

      {rules.isPending ? (
        <div className="flex flex-col gap-4">
          {Array.from({ length: 2 }).map((_, i) => (
            <Skeleton key={i} className="h-48 rounded-xl" />
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
      ) : totalRules === 0 ? (
        <EmptyState
          title="No rules configured"
          description="Add a rule to start surfacing deterministic alerts across your stations."
          icon={<Sparkles />}
          action={
            <PermissionGate permission="risk_rule.manage" mode="hide">
              <Button onClick={openCreate}>Create one</Button>
            </PermissionGate>
          }
        />
      ) : (
        <div className="flex flex-col gap-5">
          {grouped.map((group) => (
            <RuleGroupCard
              key={group.category}
              category={group.category}
              rules={group.rules}
              canManage={canManage === true}
              onEdit={openEdit}
              onToggle={(rule) => toggle.mutate({ id: rule.id, enabled: !rule.enabled })}
              togglingId={toggle.isPending ? toggle.variables?.id : undefined}
            />
          ))}
        </div>
      )}

      <RuleDialog open={dialogOpen} onOpenChange={setDialogOpen} rule={editing} />
    </div>
  );
}

interface RuleGroupCardProps {
  category: RuleCategory;
  rules: RiskRule[];
  canManage: boolean;
  onEdit: (rule: RiskRule) => void;
  onToggle: (rule: RiskRule) => void;
  togglingId?: string;
}

function RuleGroupCard({
  category,
  rules,
  canManage,
  onEdit,
  onToggle,
  togglingId,
}: RuleGroupCardProps) {
  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle className="text-base">{CATEGORY_LABEL[category]}</CardTitle>
        <span className="text-xs text-muted-foreground">
          {rules.length} {rules.length === 1 ? 'rule' : 'rules'}
        </span>
      </CardHeader>
      <CardContent className="flex flex-col divide-y divide-border/60 p-0">
        {rules.map((rule) => (
          <div
            key={rule.id}
            className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between"
          >
            <div className="flex min-w-0 flex-col gap-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-medium">{rule.name}</span>
                <span className="font-mono text-xs text-muted-foreground">{rule.code}</span>
                <RiskBadge severity={toRiskSeverity(rule.severity)} />
                {!rule.enabled ? (
                  <span className="text-xs text-muted-foreground">(disabled)</span>
                ) : null}
              </div>
              <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
                <span>{conditionLabel(rule.condition)}</span>
                {rule.threshold ? (
                  <span className="font-mono tabular-nums">threshold {rule.threshold}</span>
                ) : null}
                {rule.comparison_period_days != null ? (
                  <span>
                    {rule.comparison_period_days}{' '}
                    {rule.comparison_period_days === 1 ? 'day' : 'days'}
                  </span>
                ) : null}
              </div>
            </div>

            <div className="flex shrink-0 items-center gap-2">
              <PermissionGate permission="risk_rule.manage">
                <Button
                  variant={rule.enabled ? 'outline' : 'ghost'}
                  size="sm"
                  role="switch"
                  aria-checked={rule.enabled}
                  disabled={togglingId === rule.id}
                  onClick={() => onToggle(rule)}
                >
                  {togglingId === rule.id ? '…' : rule.enabled ? 'Enabled' : 'Disabled'}
                </Button>
              </PermissionGate>
              <PermissionGate permission="risk_rule.manage">
                <Button variant="ghost" size="sm" onClick={() => canManage && onEdit(rule)}>
                  Edit
                </Button>
              </PermissionGate>
            </div>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}
