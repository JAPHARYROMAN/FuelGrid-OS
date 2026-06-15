'use client';

import * as React from 'react';
import Link from 'next/link';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CalendarClock, Database, Download, Eye, Save, Wand2 } from 'lucide-react';

import {
  SdkError,
  type BuilderDataset,
  type BuilderSpec,
  type ReportEnvelope,
  type ReportTemplate,
  type ReportTemplateInput,
} from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  DataQualityCard,
  EmptyState,
  ErrorState,
  LoadingState,
  PageHeader,
  Skeleton,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { toast } from '@/lib/toast';
import { usePermission, usePermissions } from '@/hooks/use-permissions';

import {
  DataQualityPanel,
  EnvelopeTable,
  InsightPanel,
  SummaryGrid,
} from '../_components/report-envelope';
import {
  BuilderVizCard,
  buildCsv,
  describeSpec,
  downloadCsv,
  errorCode,
  summarizeFilters,
} from './builder-shared';
import {
  BuilderForm,
  SpecSummary,
  emptySpecState,
  specIsRunnable,
  specToState,
  stateToSpec,
  type SpecState,
} from './builder-form';
import { SaveTemplateDialog } from './save-dialog';
import { TemplateGallery } from './template-gallery';

/**
 * Custom Report Builder (Reports Center Phase 11 — blueprint §6.2). A guided,
 * QUERY-SAFE builder over a curated dataset registry: the actor picks a dataset
 * they may use, then dimensions / measures / filters / sort / viz drawn ONLY from
 * that dataset's whitelist (getBuilderDatasets already strips datasets the actor
 * lacks the permission for, and sensitive measures they cannot read). Preview
 * POSTs the spec to /preview and renders the returned ReportEnvelope through the
 * shared report shell (table + chart + data-quality). Save persists it as a
 * template with a share scope; the gallery runs / opens / edits / deletes saved
 * templates. Schedule (Phase 12) and snapshots (Phase 14) are linked from a saved
 * template. Desktop reports are not i18n.
 */

export default function ReportBuilderPage() {
  const canBuild = usePermission('reports.builder');

  const datasetsQuery = useQuery({
    queryKey: ['builder', 'datasets'],
    queryFn: ({ signal }) => api.getBuilderDatasets(signal),
    enabled: canBuild === true,
    retry: false,
  });

  if (canBuild === false) {
    return (
      <Shell>
        <EmptyState
          title="No access to the report builder"
          description="You don't have permission to build custom reports. Ask an administrator for the Reports builder permission."
        />
      </Shell>
    );
  }

  return (
    <Shell>
      {canBuild === null || datasetsQuery.isPending ? (
        <LoadingState title="Loading the dataset registry…" />
      ) : datasetsQuery.isError ? (
        <ErrorState
          title="Couldn't load the report builder"
          description={String((datasetsQuery.error as Error).message)}
          onRetry={() => datasetsQuery.refetch()}
        />
      ) : (
        <BuilderWorkspace datasets={datasetsQuery.data.datasets} />
      )}
    </Shell>
  );
}

function Shell({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Custom"
        title="Report builder"
        description="Build a custom report from a curated dataset — pick dimensions, measures, filters and a chart, preview it live, then save it to run, export, schedule or share. Every report is composed only from whitelisted, permission-checked columns; money and litres stay exact decimals."
        actions={
          <Button variant="secondary" size="sm" asChild>
            <Link href="/reports/scheduled">
              <CalendarClock className="size-4" />
              Scheduled reports
            </Link>
          </Button>
        }
      />
      {children}
    </div>
  );
}

function BuilderWorkspace({ datasets }: { datasets: BuilderDataset[] }) {
  const qc = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: ({ signal }) => api.me(signal), retry: false });
  const perms = usePermissions();

  // The top of the workspace; we scroll it into view when a template run renders
  // a fresh preview (scrollIntoView is a no-op in jsdom, so it's test-safe).
  const topRef = React.useRef<HTMLDivElement>(null);
  const scrollToPreview = React.useCallback(() => {
    topRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }, []);

  const [state, setState] = React.useState<SpecState>(() => emptySpecState());
  const [previewEnv, setPreviewEnv] = React.useState<ReportEnvelope | null>(null);
  // The dataset the *rendered* preview belongs to — which can be a saved template
  // run from the gallery (where no builder dataset is actively picked) as well as
  // a live spec preview. Kept separate from the editor's selected dataset so a
  // template-run still renders its envelope.
  const [previewDatasetKey, setPreviewDatasetKey] = React.useState<string>('');
  const [previewState, setPreviewState] = React.useState<SpecState | null>(null);
  const [fieldError, setFieldError] = React.useState<string | undefined>();
  const [saveOpen, setSaveOpen] = React.useState(false);
  const [editingTemplate, setEditingTemplate] = React.useState<ReportTemplate | null>(null);
  const [busyTemplate, setBusyTemplate] = React.useState<string | null>(null);

  const dataset = datasets.find((d) => d.key === state.dataset);
  const previewDataset = datasets.find((d) => d.key === previewDatasetKey);

  const templatesQuery = useQuery({
    queryKey: ['builder', 'templates'],
    queryFn: ({ signal }) => api.listReportTemplates({ limit: 100 }, signal),
    retry: false,
  });
  const templates = templatesQuery.data?.items ?? [];

  function pickDataset(key: string) {
    setState(emptySpecState(key));
    setPreviewEnv(null);
    setPreviewState(null);
    setFieldError(undefined);
  }

  const preview = useMutation({
    mutationFn: (spec: BuilderSpec) => api.previewBuilderReport(spec),
    onSuccess: (env) => {
      setPreviewEnv(env);
      setPreviewDatasetKey(state.dataset);
      setPreviewState(state);
      setFieldError(undefined);
    },
    onError: (err) => {
      setFieldError(errorCode(err));
      const status = err instanceof SdkError ? err.status : 0;
      toast.error(
        status === 403 ? "You can't run this dataset" : 'Preview failed',
        err instanceof Error ? err.message : undefined,
      );
    },
  });

  function runPreview() {
    if (!dataset || !specIsRunnable(state)) return;
    preview.mutate(stateToSpec(state));
  }

  const save = useMutation({
    mutationFn: (input: ReportTemplateInput) =>
      editingTemplate
        ? api.updateReportTemplate(editingTemplate.id, input)
        : api.createReportTemplate(input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['builder', 'templates'] });
      toast.success(editingTemplate ? 'Template updated' : 'Template saved');
      setSaveOpen(false);
      setEditingTemplate(null);
    },
    onError: (err) =>
      toast.error('Could not save the template', err instanceof Error ? err.message : undefined),
  });

  // Run a SAVED template: re-validates the dataset permission server-side and
  // renders the returned envelope in the preview pane.
  const runTemplate = useMutation({
    mutationFn: (t: ReportTemplate) => api.runReportTemplate(t.id),
    onMutate: (t) => setBusyTemplate(t.id),
    onSuccess: (env, t) => {
      setPreviewEnv(env);
      setPreviewDatasetKey(t.dataset_key);
      setPreviewState(specToState(t.spec));
      setFieldError(undefined);
      scrollToPreview();
    },
    onError: (err) => {
      const status = err instanceof SdkError ? err.status : 0;
      toast.error(
        status === 403 ? "You can't run this report's dataset" : 'Run failed',
        err instanceof Error ? err.message : undefined,
      );
    },
    onSettled: () => setBusyTemplate(null),
  });

  const removeTemplate = useMutation({
    mutationFn: (id: string) => api.deleteReportTemplate(id),
    onMutate: (id) => setBusyTemplate(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['builder', 'templates'] });
      toast.success('Template deleted');
    },
    onError: (err) =>
      toast.error('Could not delete the template', err instanceof Error ? err.message : undefined),
    onSettled: () => setBusyTemplate(null),
  });

  function openInBuilder(t: ReportTemplate) {
    const ds = datasets.find((d) => d.key === t.dataset_key);
    if (!ds) {
      toast.error('That report uses a dataset you can no longer build from');
      return;
    }
    setState(specToState(t.spec));
    setPreviewEnv(null);
    setPreviewState(null);
    setFieldError(undefined);
    scrollToPreview();
  }

  function editTemplate(t: ReportTemplate) {
    setEditingTemplate(t);
    setState(specToState(t.spec));
    setSaveOpen(true);
  }

  function openSaveNew() {
    setEditingTemplate(null);
    setSaveOpen(true);
  }

  const runnable = !!dataset && specIsRunnable(state);
  const filterSummary = summarizeFilters(stateToSpec(state).filters, dataset);

  return (
    <div ref={topRef} className="flex flex-col gap-7">
      {/* Step 1 — DATASET picker (only the datasets the actor may use). */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <Database className="size-4 text-accent" />1 · Choose a dataset
          </CardTitle>
        </CardHeader>
        <CardContent>
          {datasets.length === 0 ? (
            <EmptyState
              title="No datasets available"
              description="You don't currently have permission to build from any dataset."
            />
          ) : (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
              {datasets.map((d) => {
                const on = d.key === state.dataset;
                return (
                  <button
                    key={d.key}
                    type="button"
                    aria-pressed={on}
                    onClick={() => pickDataset(d.key)}
                    className={
                      'flex flex-col gap-1 rounded-xl border p-4 text-left transition ' +
                      (on
                        ? 'border-accent bg-accent/5 ring-1 ring-accent/30'
                        : 'border-border bg-card hover:bg-accent-muted/30')
                    }
                  >
                    <span className="flex items-center gap-2 font-medium">
                      {d.name}
                      {d.sensitive_permission ? <Badge tone="warning">Gated</Badge> : null}
                    </span>
                    <span className="text-xs text-muted-foreground">{d.description}</span>
                  </button>
                );
              })}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Steps 2–5 — the guided builder for the chosen dataset. */}
      {dataset ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Wand2 className="size-4 text-accent" />2 · Shape the report — {dataset.name}
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-5">
            <BuilderForm
              dataset={dataset}
              state={state}
              onChange={setState}
              fieldError={fieldError}
            />

            {filterSummary.length > 0 ? (
              <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                <span className="font-medium uppercase tracking-wide">Where</span>
                {filterSummary.map((s, i) => (
                  <Badge key={i} tone="neutral">
                    {s}
                  </Badge>
                ))}
              </div>
            ) : null}

            <div className="flex flex-wrap items-center gap-2">
              <Button onClick={runPreview} disabled={!runnable || preview.isPending}>
                <Eye className="size-4" />
                {preview.isPending ? 'Running preview…' : 'Preview'}
              </Button>
              <Button variant="outline" onClick={openSaveNew} disabled={!runnable}>
                <Save className="size-4" />
                Save as template
              </Button>
              {!runnable ? (
                <span className="text-xs text-muted-foreground">
                  Add at least one measure to preview or save.
                </span>
              ) : null}
            </div>
          </CardContent>
        </Card>
      ) : null}

      {/* Step 6 — PREVIEW: the returned ReportEnvelope via the shared shell.
          Renders for a live spec preview OR a saved-template run; the dataset +
          spec shown track whichever produced the envelope. */}
      {preview.isPending && dataset ? (
        <PreviewPanel env={null} pending dataset={dataset} state={state} />
      ) : previewEnv && previewDataset && previewState ? (
        <PreviewPanel
          env={previewEnv}
          pending={false}
          dataset={previewDataset}
          state={previewState}
        />
      ) : null}

      {/* Step 8 — the saved-template gallery (My / Shared). */}
      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold">Saved reports</h2>
          <span className="text-xs text-muted-foreground">{templates.length} template(s)</span>
        </div>
        {templatesQuery.isPending ? (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-40 rounded-xl" />
            ))}
          </div>
        ) : templatesQuery.isError ? (
          <ErrorState
            title="Couldn't load your saved reports"
            description={String((templatesQuery.error as Error).message)}
            onRetry={() => templatesQuery.refetch()}
          />
        ) : (
          <TemplateGallery
            templates={templates}
            datasets={datasets}
            currentUserId={me.data?.user_id}
            isAdmin={perms.data?.is_system_admin}
            busyId={busyTemplate}
            onRun={(t) => runTemplate.mutate(t)}
            onOpen={openInBuilder}
            onEdit={editTemplate}
            onDelete={(t) => {
              if (confirm(`Delete template “${t.name}”?`)) removeTemplate.mutate(t.id);
            }}
          />
        )}
      </section>

      <SaveTemplateDialog
        open={saveOpen}
        onOpenChange={(v) => {
          setSaveOpen(v);
          if (!v) setEditingTemplate(null);
        }}
        spec={dataset ? stateToSpec(state) : null}
        editing={editingTemplate}
        saving={save.isPending}
        onSave={(input) => save.mutate(input)}
      />
    </div>
  );
}

/**
 * The preview pane: the chosen chart (when not a table), the always-present
 * decimal-safe table, the row/column summary, data-quality (sensitive-omitted
 * notes), and an honest CSV export of exactly the rows shown. Schedule/snapshot
 * are linked from a SAVED template (the scheduler + snapshot engines key off the
 * catalog reports, not ad-hoc specs), so the pane points the actor at saving.
 */
function PreviewPanel({
  env,
  pending,
  dataset,
  state,
}: {
  env: ReportEnvelope | null;
  pending: boolean;
  dataset: BuilderDataset;
  state: SpecState;
}) {
  if (pending && !env) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-24 rounded-xl" />
        <Skeleton className="h-72 rounded-xl" />
      </div>
    );
  }
  if (!env) return null;

  const filename = `report-${dataset.key}-${new Date().toISOString().slice(0, 10)}.csv`;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-semibold">Preview</h2>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            variant="outline"
            onClick={() => downloadCsv(filename, buildCsv(env.table))}
            disabled={(env.table?.rows ?? []).length === 0}
          >
            <Download className="size-4" />
            Export CSV
          </Button>
        </div>
      </div>

      <DataQualityPanel items={env.data_quality} />
      <SummaryGrid summary={env.summary} />

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
        <div className="flex min-w-0 flex-col gap-6">
          <BuilderVizCard env={env} />
          <EnvelopeTable table={env.table} caption={describeSpec(stateToSpec(state), dataset)} />
        </div>
        <aside className="flex flex-col gap-6">
          <SpecSummary dataset={dataset} state={state} />
          <InsightPanel insights={env.insights} recommendedActions={env.recommended_actions} />
          <Card>
            <CardHeader>
              <CardTitle className="text-sm">Run on a schedule</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-2 text-sm text-muted-foreground">
              <p>
                Save this report as a template, then schedule, snapshot or share it from your saved
                reports below.
              </p>
              <Button variant="secondary" size="sm" asChild className="w-fit">
                <Link href="/reports/scheduled">
                  <CalendarClock className="size-4" />
                  Open scheduled reports
                </Link>
              </Button>
            </CardContent>
          </Card>
          {env.data_quality.length === 0 ? (
            <DataQualityCard
              level="info"
              messages={[
                'Every figure here is composed only from whitelisted columns and is scoped to what you can see.',
              ]}
            />
          ) : null}
        </aside>
      </div>
    </div>
  );
}
