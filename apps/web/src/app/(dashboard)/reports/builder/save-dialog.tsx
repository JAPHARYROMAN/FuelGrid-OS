'use client';

import * as React from 'react';

import type {
  BuilderSharedScope,
  BuilderSpec,
  ReportTemplate,
  ReportTemplateInput,
} from '@fuelgrid/sdk';
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

/**
 * The Save-as-template dialog (step 7). A saved template is a validated spec + a
 * name/description + a SHARE SCOPE: private (only you / admins), tenant (everyone
 * in the tenant) or role (a set of role codes). The spec is built by the caller
 * from the current builder state; this dialog only collects the metadata and
 * share scope and posts via the SDK. Running a shared template ALWAYS re-checks
 * the runner's own dataset permission server-side, so sharing never leaks data a
 * recipient could not read live.
 */

const selectClasses =
  'h-9 w-full rounded-md border border-border bg-background px-2.5 text-sm text-foreground ' +
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50';

const SCOPES: { value: BuilderSharedScope; label: string; hint: string }[] = [
  { value: 'private', label: 'Private', hint: 'Only you (and tenant admins) can see and run it.' },
  {
    value: 'tenant',
    label: 'Whole tenant',
    hint: 'Everyone in your tenant can run it (their own permissions still apply).',
  },
  { value: 'role', label: 'Specific roles', hint: 'Only the role codes you list can see it.' },
];

export function SaveTemplateDialog({
  open,
  onOpenChange,
  spec,
  editing,
  saving,
  onSave,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  /** The spec to persist (already built from the builder state by the caller). */
  spec: BuilderSpec | null;
  /** When set, the dialog edits an existing template's metadata + share scope. */
  editing?: ReportTemplate | null;
  saving: boolean;
  onSave: (input: ReportTemplateInput) => void;
}) {
  const [name, setName] = React.useState('');
  const [description, setDescription] = React.useState('');
  const [scope, setScope] = React.useState<BuilderSharedScope>('private');
  const [roles, setRoles] = React.useState('');

  React.useEffect(() => {
    if (!open) return;
    if (editing) {
      setName(editing.name);
      setDescription(editing.description ?? '');
      setScope(editing.shared_scope);
      setRoles(editing.shared_roles.join(', '));
    } else {
      setName('');
      setDescription('');
      setScope('private');
      setRoles('');
    }
  }, [open, editing]);

  function submit() {
    if (!spec || !name.trim()) return;
    const input: ReportTemplateInput = {
      name: name.trim(),
      description: description.trim() || undefined,
      spec,
      shared_scope: scope,
    };
    if (scope === 'role') {
      input.shared_roles = roles
        .split(',')
        .map((r) => r.trim())
        .filter(Boolean);
    }
    onSave(input);
  }

  const scopeHint = SCOPES.find((s) => s.value === scope)?.hint;
  const roleMissing =
    scope === 'role' &&
    roles
      .split(',')
      .map((r) => r.trim())
      .filter(Boolean).length === 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? 'Update template' : 'Save as template'}</DialogTitle>
          <DialogDescription>
            Save this report so you can run, export, schedule or share it later. Sharing never
            bypasses permissions — every runner is re-checked against the dataset live.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4 py-1">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="tpl-name">Name</Label>
            <Input
              id="tpl-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Daily net revenue by station"
              autoFocus
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="tpl-desc">Description (optional)</Label>
            <textarea
              id="tpl-desc"
              className={`${selectClasses} h-16 py-2`}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What this report answers"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="tpl-scope">Share scope</Label>
            <select
              id="tpl-scope"
              className={selectClasses}
              value={scope}
              onChange={(e) => setScope(e.target.value as BuilderSharedScope)}
            >
              {SCOPES.map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}
                </option>
              ))}
            </select>
            {scopeHint ? <p className="text-xs text-muted-foreground">{scopeHint}</p> : null}
          </div>
          {scope === 'role' ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="tpl-roles">Role codes (comma separated)</Label>
              <Input
                id="tpl-roles"
                value={roles}
                onChange={(e) => setRoles(e.target.value)}
                placeholder="e.g. station_manager, finance"
              />
            </div>
          ) : null}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={saving}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={saving || !name.trim() || !spec || roleMissing}>
            {saving ? 'Saving…' : editing ? 'Save changes' : 'Save template'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
