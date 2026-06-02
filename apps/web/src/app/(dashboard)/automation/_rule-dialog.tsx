'use client';

import { useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { SdkError, type RiskRule, type RiskRuleInput } from '@fuelgrid/sdk';
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Label,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { toast } from '@/lib/toast';

import {
  CONDITION_KEYS,
  CONDITION_META,
  SEVERITIES,
  conditionLabel,
  normalizeCategory,
} from './_rules';

const SELECT_CLASS =
  'h-10 rounded-md border border-border bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring';

interface RuleFormValues {
  name: string;
  code: string;
  condition: string;
  category: string;
  severity: string;
  threshold: string;
  comparison_period_days: string;
  lookback_days: string;
  message_template: string;
  recommended_action: string;
  enabled: boolean;
}

function ruleToForm(rule: RiskRule | null): RuleFormValues {
  return {
    name: rule?.name ?? '',
    code: rule?.code ?? '',
    condition: rule?.condition ?? CONDITION_KEYS[0],
    category: rule?.category ?? CONDITION_META[CONDITION_KEYS[0]]?.category ?? 'general',
    severity: rule?.severity ?? 'medium',
    threshold: rule?.threshold ?? '',
    comparison_period_days:
      rule?.comparison_period_days != null ? String(rule.comparison_period_days) : '',
    lookback_days: rule?.lookback_days != null ? String(rule.lookback_days) : '',
    message_template: rule?.message_template ?? '',
    recommended_action: rule?.recommended_action ?? '',
    enabled: rule?.enabled ?? true,
  };
}

export interface RuleDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The rule being edited, or null to create a new one. */
  rule: RiskRule | null;
}

/**
 * RuleDialog edits an existing rule or creates a new one. On create the
 * operator picks one of the four code-backed conditions from a select (never
 * free-typed). The message_template + recommended_action are shown so the
 * operator can see exactly what an alert will say.
 */
export function RuleDialog({ open, onOpenChange, rule }: RuleDialogProps) {
  const qc = useQueryClient();
  const isEdit = rule != null;

  const {
    register,
    handleSubmit,
    reset,
    watch,
    setValue,
    formState: { errors },
  } = useForm<RuleFormValues>({ defaultValues: ruleToForm(rule) });

  // Re-seed the form whenever the dialog opens for a different rule.
  useEffect(() => {
    if (open) reset(ruleToForm(rule));
  }, [open, rule, reset]);

  const condition = watch('condition');

  const save = useMutation({
    mutationFn: (values: RuleFormValues) => {
      const base: Partial<RiskRuleInput> = {
        name: values.name.trim(),
        severity: values.severity,
        message_template: values.message_template.trim() || undefined,
        recommended_action: values.recommended_action.trim() || undefined,
        threshold: values.threshold.trim() || undefined,
        comparison_period_days: values.comparison_period_days.trim()
          ? Number(values.comparison_period_days)
          : undefined,
        lookback_days: values.lookback_days.trim() ? Number(values.lookback_days) : undefined,
        enabled: values.enabled,
      };
      if (isEdit) {
        return api.updateRiskRule(rule.id, base);
      }
      return api.createRiskRule({
        ...base,
        code: values.code.trim(),
        name: values.name.trim(),
        condition: values.condition,
        category: normalizeCategory(CONDITION_META[values.condition]?.category),
      } as RiskRuleInput);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['risk-rules'] });
      toast.success(isEdit ? 'Rule updated' : 'Rule created');
      onOpenChange(false);
    },
    onError: (err) =>
      toast.error(
        isEdit ? 'Could not update rule' : 'Could not create rule',
        err instanceof SdkError ? err.message : 'Please try again.',
      ),
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{isEdit ? `Edit rule` : 'New rule'}</DialogTitle>
          <DialogDescription>
            {isEdit
              ? 'Tune how this rule fires and what its alert says.'
              : 'Add a deterministic rule by picking a known condition.'}
          </DialogDescription>
        </DialogHeader>

        <form
          className="flex flex-col gap-3"
          onSubmit={handleSubmit((values) => save.mutate(values))}
        >
          <div className="grid grid-cols-2 gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rule-name">Name</Label>
              <Input
                id="rule-name"
                {...register('name', { required: 'Name is required' })}
                aria-invalid={errors.name ? true : undefined}
              />
              {errors.name ? (
                <span className="text-xs text-danger">{errors.name.message}</span>
              ) : null}
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rule-code">Code</Label>
              <Input
                id="rule-code"
                {...register('code', {
                  required: !isEdit ? 'Code is required' : false,
                })}
                disabled={isEdit}
                aria-invalid={errors.code ? true : undefined}
              />
              {errors.code ? (
                <span className="text-xs text-danger">{errors.code.message}</span>
              ) : null}
            </div>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="rule-condition">Condition</Label>
            {isEdit ? (
              <Input id="rule-condition" value={conditionLabel(condition)} disabled readOnly />
            ) : (
              <select id="rule-condition" className={SELECT_CLASS} {...register('condition')}>
                {CONDITION_KEYS.map((key) => (
                  <option key={key} value={key}>
                    {CONDITION_META[key]?.label ?? key}
                  </option>
                ))}
              </select>
            )}
            <span className="text-xs text-muted-foreground">
              {CONDITION_META[condition]?.description ?? 'A code-backed evaluator.'}
            </span>
          </div>

          <div className="grid grid-cols-3 gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rule-severity">Severity</Label>
              <select id="rule-severity" className={SELECT_CLASS} {...register('severity')}>
                {SEVERITIES.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rule-threshold">Threshold</Label>
              <Input id="rule-threshold" inputMode="decimal" {...register('threshold')} />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rule-period">Comparison (days)</Label>
              <Input id="rule-period" inputMode="numeric" {...register('comparison_period_days')} />
            </div>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="rule-message">Message template</Label>
            <textarea
              id="rule-message"
              rows={2}
              className="rounded-md border border-border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              {...register('message_template')}
            />
            <span className="text-xs text-muted-foreground">
              Shown on the alert. Supports placeholders the engine fills in.
            </span>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="rule-action">Recommended action</Label>
            <textarea
              id="rule-action"
              rows={2}
              className="rounded-md border border-border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              {...register('recommended_action')}
            />
          </div>

          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              className="size-4 rounded border-border"
              checked={watch('enabled')}
              onChange={(e) => setValue('enabled', e.target.checked)}
            />
            Enabled
          </label>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={save.isPending}>
              {save.isPending ? 'Saving...' : 'Save'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
