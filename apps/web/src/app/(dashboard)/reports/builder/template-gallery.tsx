'use client';

import * as React from 'react';
import { Globe, Lock, Pencil, Play, Trash2, Users } from 'lucide-react';

import type { BuilderDataset, ReportTemplate } from '@fuelgrid/sdk';
import { Badge, Button, Card, CardContent, CardHeader, CardTitle, EmptyState } from '@fuelgrid/ui';

import { describeSpec } from './builder-shared';

/**
 * The "My / Shared templates" gallery (step 8 + blueprint §6.2). Lists the saved
 * templates the actor can see (share-scope is enforced server-side on the list),
 * and offers RUN (re-validates the dataset permission live), open-in-builder /
 * EDIT (owner only), and DELETE (owner only). A shared template the actor can see
 * but whose dataset they cannot currently run will 403 on run — the gallery shows
 * that honestly via the run handler's error toast rather than hiding the card.
 */

function scopeBadge(scope: ReportTemplate['shared_scope']) {
  if (scope === 'tenant')
    return (
      <Badge tone="info">
        <Globe className="size-3" /> Tenant
      </Badge>
    );
  if (scope === 'role')
    return (
      <Badge tone="accent">
        <Users className="size-3" /> Roles
      </Badge>
    );
  return (
    <Badge tone="neutral">
      <Lock className="size-3" /> Private
    </Badge>
  );
}

export function TemplateGallery({
  templates,
  datasets,
  currentUserId,
  isAdmin,
  busyId,
  onRun,
  onOpen,
  onEdit,
  onDelete,
}: {
  templates: ReportTemplate[];
  datasets: BuilderDataset[];
  currentUserId?: string;
  isAdmin?: boolean;
  /** The id of a template a mutation is currently in-flight for. */
  busyId?: string | null;
  onRun: (t: ReportTemplate) => void;
  onOpen: (t: ReportTemplate) => void;
  onEdit: (t: ReportTemplate) => void;
  onDelete: (t: ReportTemplate) => void;
}) {
  if (templates.length === 0) {
    return (
      <EmptyState
        title="No saved reports yet"
        description="Build a report above, preview it, then save it as a template to run, share or schedule it later."
      />
    );
  }

  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
      {templates.map((t) => {
        const ds = datasets.find((d) => d.key === t.dataset_key);
        const mine = !!currentUserId && t.created_by === currentUserId;
        const canEdit = mine || !!isAdmin;
        const busy = busyId === t.id;
        return (
          <Card key={t.id} data-testid="template-card" className="flex flex-col">
            <CardHeader className="gap-1">
              <div className="flex items-start justify-between gap-2">
                <CardTitle className="text-base">{t.name}</CardTitle>
                {scopeBadge(t.shared_scope)}
              </div>
              <p className="text-xs text-muted-foreground">{describeSpec(t.spec, ds)}</p>
            </CardHeader>
            <CardContent className="mt-auto flex flex-col gap-3">
              {t.description ? (
                <p className="text-sm text-muted-foreground">{t.description}</p>
              ) : null}
              <div className="flex flex-wrap items-center gap-1.5">
                {ds ? <Badge tone="neutral">{ds.name}</Badge> : null}
                {t.is_system ? <Badge tone="accent">System</Badge> : null}
                {mine ? <Badge tone="success">Yours</Badge> : null}
              </div>
              <div className="flex flex-wrap items-center gap-1.5">
                <Button size="sm" disabled={busy} onClick={() => onRun(t)}>
                  <Play className="size-3.5" />
                  Run
                </Button>
                <Button size="sm" variant="outline" disabled={busy} onClick={() => onOpen(t)}>
                  Open in builder
                </Button>
                {canEdit && !t.is_system ? (
                  <>
                    <Button
                      size="sm"
                      variant="ghost"
                      aria-label={`Edit ${t.name}`}
                      disabled={busy}
                      onClick={() => onEdit(t)}
                    >
                      <Pencil className="size-4" />
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      aria-label={`Delete ${t.name}`}
                      disabled={busy}
                      onClick={() => onDelete(t)}
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  </>
                ) : null}
              </div>
            </CardContent>
          </Card>
        );
      })}
    </div>
  );
}
