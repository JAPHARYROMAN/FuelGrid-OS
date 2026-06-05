'use client';

import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CalendarClock, Check, ListChecks, Plus, Users } from 'lucide-react';

import {
  SdkError,
  type SetupChecklist,
  type SetupChecklistStep,
  type ShiftTeam,
} from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  Input,
  Label,
  PageHeader,
  Skeleton,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
import { api } from '@/lib/api';

function todayISO(): string {
  return new Date().toISOString().slice(0, 10);
}

function findSetupStep(checklist: SetupChecklist | undefined, code: string) {
  return checklist?.steps.find((step) => step.code === code);
}

function formatSetupCount(step: SetupChecklistStep | undefined) {
  if (!step) return '0 / 0';
  return `${step.count} / ${step.required_count}`;
}

function setupProgressSummary(checklist: SetupChecklist | undefined): string | null {
  const teams = findSetupStep(checklist, 'teams');
  const anchor = findSetupStep(checklist, 'rotation_anchor');
  if (!teams && !anchor) return null;

  const remaining: string[] = [];
  if (teams && !teams.ready) remaining.push(`teams ${formatSetupCount(teams)}`);
  if (anchor && !anchor.ready) remaining.push(`anchors ${formatSetupCount(anchor)}`);

  if (remaining.length > 0) return `Setup still needs ${remaining.join(' and ')}.`;
  if (teams?.status === 'completed' && anchor?.status === 'completed') {
    return 'Setup checklist reviewed.';
  }
  return 'Setup is ready for review.';
}

function setupSyncErrorMessage(error: unknown) {
  if (error instanceof SdkError && (error.status === 401 || error.status === 403)) return null;
  return error instanceof SdkError ? error.message : 'Could not refresh setup checklist';
}

export default function TeamsPage() {
  const qc = useQueryClient();
  const [stationID, setStationID] = useState('');
  const [anchorDraft, setAnchorDraft] = useState('');
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionSuccess, setActionSuccess] = useState<string | null>(null);

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });

  useEffect(() => {
    const first = stations.data?.items?.[0];
    if (!stationID && first) setStationID(first.id);
  }, [stationID, stations.data]);

  const teamsKey = ['teams', stationID];
  const teams = useQuery({
    queryKey: teamsKey,
    queryFn: ({ signal }) => api.listTeams(stationID, signal),
    enabled: !!stationID,
  });
  const employees = useQuery({
    queryKey: ['employees', stationID],
    // TODO(pagination): paged envelope; request the max page size so the full
    // roster is available for team assignment. Revisit with a control if a
    // station's headcount grows past one page.
    queryFn: ({ signal }) => api.listEmployees(stationID, { limit: 200 }, signal),
    enabled: !!stationID,
  });
  const anchorKey = ['rotation-anchor', stationID];
  const anchor = useQuery({
    queryKey: anchorKey,
    queryFn: ({ signal }) => api.getRotationAnchor(stationID, signal),
    enabled: !!stationID,
  });
  const rosterKey = ['roster', stationID];
  const roster = useQuery({
    queryKey: rosterKey,
    queryFn: ({ signal }) => api.getRoster(stationID, { days: 7 }, signal),
    enabled: !!stationID,
  });
  const setupChecklist = useQuery({
    queryKey: ['setup-checklist'],
    queryFn: ({ signal }) => api.getSetupChecklist(signal),
  });

  useEffect(() => {
    setAnchorDraft(anchor.data?.rotation_anchor_date ?? '');
  }, [anchor.data]);

  async function syncSetupSteps(stepCodes: string[]) {
    let checklist = await api.getSetupChecklist();
    qc.setQueryData(['setup-checklist'], checklist);

    for (const code of stepCodes) {
      const step = findSetupStep(checklist, code);
      if (step?.ready && step.status !== 'completed') {
        checklist = await api.updateSetupStep({ step_code: code, status: 'completed' });
        qc.setQueryData(['setup-checklist'], checklist);
      }
    }

    return checklist;
  }

  const ensure = useMutation({
    mutationFn: () => api.ensureTeams(stationID, {}),
    onSuccess: async () => {
      setActionError(null);
      qc.invalidateQueries({ queryKey: teamsKey });
      qc.invalidateQueries({ queryKey: rosterKey });
      try {
        const checklist = await syncSetupSteps(['teams', 'rotation_anchor']);
        setActionSuccess(setupProgressSummary(checklist));
      } catch (e) {
        setActionSuccess('Teams created.');
        setActionError(setupSyncErrorMessage(e));
      }
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not create teams'),
  });

  const setMembers = useMutation({
    mutationFn: ({ teamID, employeeIDs }: { teamID: string; employeeIDs: string[] }) =>
      api.setTeamMembers(teamID, employeeIDs),
    onSuccess: async () => {
      setActionError(null);
      qc.invalidateQueries({ queryKey: teamsKey });
      qc.invalidateQueries({ queryKey: ['employees', stationID] });
      qc.invalidateQueries({ queryKey: rosterKey });
      try {
        const checklist = await syncSetupSteps(['teams', 'rotation_anchor']);
        setActionSuccess(setupProgressSummary(checklist));
      } catch (e) {
        setActionSuccess('Team members updated.');
        setActionError(setupSyncErrorMessage(e));
      }
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not update members'),
  });

  const saveAnchor = useMutation({
    mutationFn: (nextAnchor: string | null) => api.setRotationAnchor(stationID, nextAnchor),
    onSuccess: async (saved) => {
      setActionError(null);
      setAnchorDraft(saved.rotation_anchor_date ?? '');
      qc.setQueryData(anchorKey, saved);
      qc.invalidateQueries({ queryKey: anchorKey });
      qc.invalidateQueries({ queryKey: rosterKey });
      const base = saved.rotation_anchor_date
        ? `Rotation anchor saved for ${saved.rotation_anchor_date}`
        : 'Rotation anchor cleared';
      try {
        const checklist = await syncSetupSteps(['teams', 'rotation_anchor']);
        const progress = setupProgressSummary(checklist);
        setActionSuccess(progress ? `${base}. ${progress}` : base);
      } catch (e) {
        setActionSuccess(base);
        setActionError(setupSyncErrorMessage(e));
      }
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not save anchor'),
  });

  // Map each employee to its current team for the membership toggles.
  const employeeTeam = useMemo(() => {
    const m = new Map<string, string | undefined>();
    for (const e of employees.data?.items ?? []) m.set(e.id, e.team_id);
    return m;
  }, [employees.data]);

  function toggleMember(team: ShiftTeam, employeeID: string, currentlyOn: boolean) {
    const members = (employees.data?.items ?? [])
      .filter((e) => e.team_id === team.id)
      .map((e) => e.id);
    const next = currentlyOn ? members.filter((id) => id !== employeeID) : [...members, employeeID];
    setMembers.mutate({ teamID: team.id, employeeIDs: next });
  }

  const stationSelect =
    (stations.data?.items?.length ?? 0) > 0 ? (
      <label className="flex items-center gap-2 text-sm">
        <span className="text-muted-foreground">Station</span>
        <select
          className="h-9 rounded-md border border-border bg-background px-2 text-sm"
          value={stationID}
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

  const teamList = teams.data?.items ?? [];
  const hasTeams = teamList.length === 3;
  const teamsStep = findSetupStep(setupChecklist.data, 'teams');
  const anchorStep = findSetupStep(setupChecklist.data, 'rotation_anchor');
  const setupSummary = setupProgressSummary(setupChecklist.data);

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Settings"
        title="Teams & rotation"
        description="Assign employees to the three shift teams, set the rotation anchor, and preview the roster."
        actions={stationSelect}
      />

      {actionError ? (
        <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
          {actionError}
        </p>
      ) : null}
      {actionSuccess ? (
        <p className="rounded-md bg-success/10 px-3 py-2 text-sm text-success" role="status">
          {actionSuccess}
        </p>
      ) : null}
      {teamsStep || anchorStep ? (
        <Card>
          <CardContent className="flex flex-wrap items-center gap-3 py-4">
            <span className="flex size-9 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">
              <ListChecks className="size-4" />
            </span>
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <p className="font-medium text-foreground">Setup coverage</p>
              {setupSummary ? (
                <p className="text-sm text-muted-foreground">{setupSummary}</p>
              ) : null}
            </div>
            {teamsStep ? (
              <Badge tone={teamsStep.ready ? 'success' : 'neutral'}>
                Teams {formatSetupCount(teamsStep)}
              </Badge>
            ) : null}
            {anchorStep ? (
              <Badge tone={anchorStep.ready ? 'success' : 'neutral'}>
                Anchors {formatSetupCount(anchorStep)}
              </Badge>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {!stationID ? (
        <EmptyState
          title="No station selected"
          description="Pick a station to configure its teams."
        />
      ) : teams.isError ? (
        <ErrorState
          title="Couldn't load teams"
          description={String((teams.error as Error).message)}
          onRetry={() => teams.refetch()}
        />
      ) : (
        <>
          {/* Rotation anchor */}
          <Card>
            <CardHeader className="flex-row items-center gap-2.5 space-y-0">
              <span className="flex size-9 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">
                <CalendarClock className="size-4" />
              </span>
              <CardTitle className="text-base">Rotation anchor</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-wrap items-end gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="anchor">Cycle day 0 (date)</Label>
                <Input
                  id="anchor"
                  type="date"
                  className="h-9 w-44"
                  value={anchorDraft}
                  onChange={(e) => {
                    setAnchorDraft(e.target.value);
                    setActionSuccess(null);
                  }}
                />
              </div>
              <PermissionGate permission="station.manage">
                <div className="flex items-end gap-2">
                  <Button
                    size="sm"
                    onClick={() => saveAnchor.mutate(anchorDraft || null)}
                    disabled={saveAnchor.isPending}
                  >
                    {saveAnchor.isPending ? 'Saving…' : 'Save anchor'}
                  </Button>
                  {anchorDraft ? (
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => saveAnchor.mutate(null)}
                      disabled={saveAnchor.isPending}
                    >
                      Clear
                    </Button>
                  ) : (
                    <Button size="sm" variant="ghost" onClick={() => setAnchorDraft(todayISO())}>
                      Use today
                    </Button>
                  )}
                </div>
              </PermissionGate>
              <p className="text-xs text-muted-foreground">
                The rotation advances by one position each day on a 3-day cycle anchored here.
              </p>
            </CardContent>
          </Card>

          {/* Teams + membership */}
          {teams.isPending ? (
            <Card>
              <CardContent className="flex flex-col gap-2 p-4">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-24 rounded-lg" />
                ))}
              </CardContent>
            </Card>
          ) : !hasTeams ? (
            <EmptyState
              title="Teams not set up"
              description="This station needs its three rotation teams before you can assign employees."
              action={
                <PermissionGate permission="station.manage">
                  <Button onClick={() => ensure.mutate()} disabled={ensure.isPending}>
                    <Plus className="size-4" />
                    {ensure.isPending ? 'Creating…' : 'Create the three teams'}
                  </Button>
                </PermissionGate>
              }
            />
          ) : (
            <div className="grid gap-4 md:grid-cols-3">
              {teamList.map((team) => {
                const members = (employees.data?.items ?? []).filter((e) => e.team_id === team.id);
                return (
                  <Card key={team.id}>
                    <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
                      <CardTitle className="flex items-center gap-2 text-base">
                        <Users className="size-4 text-accent" />
                        {team.name}
                      </CardTitle>
                      <Badge tone="neutral">order {team.rotation_order}</Badge>
                    </CardHeader>
                    <CardContent className="flex flex-col gap-2">
                      <p className="text-xs text-muted-foreground">
                        {members.length} member{members.length === 1 ? '' : 's'}
                      </p>
                      <div className="flex flex-wrap gap-1.5">
                        {(employees.data?.items ?? []).map((e) => {
                          const onThisTeam = employeeTeam.get(e.id) === team.id;
                          const onOtherTeam = !onThisTeam && employeeTeam.get(e.id) != null;
                          return (
                            <PermissionGate key={e.id} permission="station.manage">
                              <button
                                type="button"
                                disabled={setMembers.isPending}
                                onClick={() => toggleMember(team, e.id, onThisTeam)}
                                title={
                                  onOtherTeam
                                    ? 'Currently on another team — click to move here'
                                    : ''
                                }
                                className={
                                  onThisTeam
                                    ? 'inline-flex items-center gap-1 rounded-full bg-accent/15 px-2.5 py-1 text-xs text-accent disabled:opacity-50'
                                    : 'inline-flex items-center gap-1 rounded-full border border-border bg-background px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:border-accent hover:text-accent disabled:opacity-50'
                                }
                              >
                                {onThisTeam ? (
                                  <Check className="size-3" />
                                ) : (
                                  <Plus className="size-3" />
                                )}
                                {e.full_name}
                                {onOtherTeam ? <span className="opacity-60">·moved</span> : null}
                              </button>
                            </PermissionGate>
                          );
                        })}
                        {(employees.data?.items?.length ?? 0) === 0 ? (
                          <span className="text-xs text-muted-foreground">
                            No employees yet — add them under Employees.
                          </span>
                        ) : null}
                      </div>
                    </CardContent>
                  </Card>
                );
              })}
            </div>
          )}

          {/* Roster preview */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Roster — next 7 days</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {roster.isPending ? (
                <div className="flex flex-col gap-2 p-4">
                  {Array.from({ length: 4 }).map((_, i) => (
                    <Skeleton key={i} className="h-10 rounded-lg" />
                  ))}
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Date</TableHead>
                      <TableHead>Morning</TableHead>
                      <TableHead>Evening</TableHead>
                      <TableHead>Resting</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {(roster.data?.items ?? []).map((d) => (
                      <TableRow key={d.date}>
                        <TableCell className="font-mono text-xs tabular-nums">{d.date}</TableCell>
                        <TableCell>
                          {d.morning_team ? (
                            <Badge tone="success">{d.morning_team.name}</Badge>
                          ) : (
                            <span className="text-xs text-muted-foreground">—</span>
                          )}
                        </TableCell>
                        <TableCell>
                          {d.evening_team ? (
                            <Badge tone="info">{d.evening_team.name}</Badge>
                          ) : (
                            <span className="text-xs text-muted-foreground">—</span>
                          )}
                        </TableCell>
                        <TableCell>
                          {d.resting_team ? (
                            <Badge tone="neutral">{d.resting_team.name}</Badge>
                          ) : (
                            <span className="text-xs text-muted-foreground">—</span>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
              {!roster.isPending && roster.data?.items?.[0]?.morning_team == null ? (
                <p className="px-4 py-3 text-xs text-muted-foreground">
                  Roster is empty until the rotation anchor is set and all three teams exist.
                </p>
              ) : null}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
