'use client';

import * as React from 'react';
import Link from 'next/link';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowRight, Check, Circle, ListChecks, Rocket } from 'lucide-react';

import { type SetupChecklistStep } from '@fuelgrid/sdk';
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
  cn,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

function stepTone(step: SetupChecklistStep) {
  if (step.ready && step.status === 'completed') return 'success' as const;
  if (step.ready) return 'neutral' as const;
  if (step.blocked) return 'neutral' as const;
  return 'warning' as const;
}

function stepLabel(step: SetupChecklistStep) {
  if (step.ready && step.status === 'completed') return 'Reviewed';
  if (step.ready) return 'Ready';
  if (step.blocked) return 'Blocked';
  return 'To do';
}

export default function SetupPage() {
  const qc = useQueryClient();
  const [stationID, setStationID] = React.useState('');
  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  const effectiveStation = stationID || stations.data?.items[0]?.id || '';

  React.useEffect(() => {
    const first = stations.data?.items?.[0];
    if (!stationID && first) setStationID(first.id);
  }, [stationID, stations.data]);

  const checklist = useQuery({
    queryKey: ['setup-checklist', effectiveStation],
    queryFn: ({ signal }) =>
      api.getSetupChecklist(effectiveStation ? { stationID: effectiveStation } : {}, signal),
    enabled: !stations.isPending,
  });

  const updateStep = useMutation({
    mutationFn: (stepCode: string) =>
      api.updateSetupStep(
        { step_code: stepCode, status: 'completed' },
        effectiveStation ? { stationID: effectiveStation } : {},
      ),
    onSuccess: (data) => {
      qc.setQueryData(['setup-checklist', effectiveStation], data);
    },
  });

  const data = checklist.data;
  const allReady = Boolean(data?.operationally_ready);
  const requiredTotal = data?.required_total ?? 0;
  const requiredReady = data?.required_ready ?? 0;
  const requiredReviewed = data?.required_completed ?? 0;
  const noStations = !stations.isPending && (stations.data?.items.length ?? 0) === 0;
  const stationSelect =
    (stations.data?.items.length ?? 0) > 0 ? (
      <label className="flex items-center gap-2 text-sm">
        <span className="text-muted-foreground">Station</span>
        <select
          className="h-9 min-w-56 rounded-md border border-border bg-background px-2 text-sm"
          value={effectiveStation}
          onChange={(e) => setStationID(e.target.value)}
        >
          {stations.data!.items.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name} ({s.code})
            </option>
          ))}
        </select>
      </label>
    ) : null;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Getting started"
        title="Setup"
        description="Track company-wide setup and the selected station's operational readiness."
        actions={
          checklist.isPending ? (
            <Skeleton className="h-7 w-28 rounded-full" />
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              {stationSelect}
              <Badge tone={allReady ? 'success' : 'neutral'}>
                {requiredReady} / {requiredTotal} ready
              </Badge>
            </div>
          )
        }
      />

      {noStations ? (
        <EmptyState
          title="No stations yet"
          description="Create a station before reviewing station setup."
          action={
            <Button asChild>
              <Link href="/settings/stations">Add stations</Link>
            </Button>
          }
        />
      ) : checklist.isPending ? (
        <Card>
          <CardContent className="flex flex-col gap-3 p-4">
            {Array.from({ length: 8 }).map((_, i) => (
              <Skeleton key={i} className="h-16 rounded-lg" />
            ))}
          </CardContent>
        </Card>
      ) : checklist.isError ? (
        <ErrorState
          title="Couldn't load setup"
          description={String((checklist.error as Error).message)}
          onRetry={() => checklist.refetch()}
        />
      ) : !data || data.steps.length === 0 ? (
        <EmptyState title="No setup steps" description="The setup checklist is empty." />
      ) : (
        <>
          {allReady ? (
            <Card>
              <CardContent className="flex flex-wrap items-center gap-3 py-5">
                <span className="flex size-10 items-center justify-center rounded-full bg-success/15 text-success">
                  <Rocket className="size-5" />
                </span>
                <div className="flex min-w-0 flex-1 flex-col">
                  <p className="font-medium text-foreground">Operational setup is ready</p>
                  <p className="text-sm text-muted-foreground">
                    {requiredReviewed} / {requiredTotal} required steps have been reviewed.
                  </p>
                </div>
                <Button asChild variant="ghost">
                  <Link href="/command-center">
                    Command Center
                    <ArrowRight className="size-4" />
                  </Link>
                </Button>
              </CardContent>
            </Card>
          ) : null}

          <Card>
            <CardHeader>
              <CardTitle className="inline-flex items-center gap-2">
                <ListChecks className="size-5" />
                Setup checklist
              </CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col divide-y divide-border">
              {data.steps.map((step, i) => {
                const canReview = step.ready && step.status !== 'completed';
                const updating = updateStep.isPending && updateStep.variables === step.code;
                return (
                  <div
                    key={step.code}
                    className="flex items-center gap-4 py-3.5 first:pt-0 last:pb-0"
                  >
                    <span
                      className={cn(
                        'flex size-8 shrink-0 items-center justify-center rounded-full border text-sm font-medium',
                        step.ready
                          ? 'border-transparent bg-success/15 text-success'
                          : 'border-border bg-muted text-muted-foreground',
                      )}
                      aria-hidden
                    >
                      {step.ready ? (
                        <Check className="size-4" />
                      ) : (
                        <span className="font-mono text-xs tabular-nums">{i + 1}</span>
                      )}
                    </span>

                    <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-medium text-foreground">{step.title}</span>
                        <Badge tone={stepTone(step)}>{stepLabel(step)}</Badge>
                        {step.required ? <Badge tone="neutral">Required</Badge> : null}
                        {step.required_count > 1 ? (
                          <span className="font-mono text-xs text-muted-foreground tabular-nums">
                            {step.count} / {step.required_count}
                          </span>
                        ) : step.count > 0 ? (
                          <span className="font-mono text-xs text-muted-foreground tabular-nums">
                            {step.count}
                          </span>
                        ) : null}
                      </div>
                      <p className="truncate text-sm text-muted-foreground">
                        {step.blocked_reason ?? step.description}
                      </p>
                    </div>

                    <div className="flex shrink-0 items-center gap-2">
                      {canReview ? (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => updateStep.mutate(step.code)}
                          disabled={updating}
                        >
                          <Check className="size-4" />
                          {updating ? 'Saving...' : 'Review'}
                        </Button>
                      ) : null}
                      <Button asChild size="sm" variant={step.ready ? 'ghost' : 'primary'}>
                        <Link href={step.href}>
                          {step.ready ? (
                            <>
                              <Circle className="size-3.5" />
                              Open
                            </>
                          ) : (
                            <>
                              {step.cta}
                              <ArrowRight className="size-4" />
                            </>
                          )}
                        </Link>
                      </Button>
                    </div>
                  </div>
                );
              })}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
