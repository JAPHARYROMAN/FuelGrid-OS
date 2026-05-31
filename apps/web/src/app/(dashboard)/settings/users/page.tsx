'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Mail, Plus, ShieldOff, X } from 'lucide-react';

import { SdkError, type UserSummary } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  EmptyState,
  ErrorState,
  Input,
  Label,
  LoadingState,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
import { api } from '@/lib/api';

export default function UsersPage() {
  const qc = useQueryClient();
  const [inviteOpen, setInviteOpen] = useState(false);
  const [inviteForm, setInviteForm] = useState({ email: '', full_name: '' });
  const [inviteError, setInviteError] = useState<string | null>(null);
  const [scope, setScope] = useState<UserSummary | null>(null);

  const list = useQuery({
    queryKey: ['users'],
    queryFn: ({ signal }) => api.listUsers(signal),
  });
  const roles = useQuery({
    queryKey: ['roles'],
    queryFn: ({ signal }) => api.listRoles(signal),
  });
  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });

  const invite = useMutation({
    mutationFn: () => api.inviteUser(inviteForm),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['users'] });
      setInviteOpen(false);
      setInviteForm({ email: '', full_name: '' });
    },
    onError: (err) => setInviteError(err instanceof SdkError ? err.message : 'Could not invite'),
  });

  const grantRole = useMutation({
    mutationFn: ({ userID, code }: { userID: string; code: string }) =>
      api.grantUserRole(userID, code),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['users'] }),
  });

  const revokeRole = useMutation({
    mutationFn: ({ userID, code }: { userID: string; code: string }) =>
      api.revokeUserRole(userID, code),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['users'] }),
  });

  const grantStation = useMutation({
    mutationFn: ({ userID, stationID }: { userID: string; stationID: string }) =>
      api.grantStationAccess(userID, stationID),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['users'] }),
  });

  const revokeStation = useMutation({
    mutationFn: ({ userID, stationID }: { userID: string; stationID: string }) =>
      api.revokeStationAccess(userID, stationID),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['users'] }),
  });

  const updateStatus = useMutation({
    mutationFn: ({ userID, status }: { userID: string; status: 'active' | 'suspended' }) =>
      api.updateUserStatus(userID, status),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['users'] }),
  });

  function openScope(u: UserSummary) {
    setScope(u);
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">{list.data?.count ?? 0} users</p>
        <PermissionGate permission="users.invite">
          <Button onClick={() => setInviteOpen(true)}>
            <Plus className="size-4" />
            Invite user
          </Button>
        </PermissionGate>
      </div>

      {list.isPending ? (
        <LoadingState />
      ) : list.isError ? (
        <ErrorState
          title="Couldn't load users"
          description={String((list.error as Error).message)}
          onRetry={() => list.refetch()}
        />
      ) : (list.data?.items?.length ?? 0) === 0 ? (
        <EmptyState
          title="No users yet"
          description="Invite the people who'll run this tenant."
          action={<Button onClick={() => setInviteOpen(true)}>Invite someone</Button>}
        />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Email</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>MFA</TableHead>
              <TableHead>Roles</TableHead>
              <TableHead>Scope</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.data!.items.map((u) => (
              <TableRow key={u.id}>
                <TableCell className="font-medium">{u.full_name}</TableCell>
                <TableCell className="text-muted-foreground">{u.email}</TableCell>
                <TableCell>
                  <Badge
                    tone={
                      u.status === 'active'
                        ? 'success'
                        : u.status === 'invited'
                          ? 'info'
                          : 'warning'
                    }
                  >
                    {u.status}
                  </Badge>
                </TableCell>
                <TableCell>
                  <Badge tone={u.mfa_enabled ? 'success' : 'neutral'}>
                    {u.mfa_enabled ? 'on' : 'off'}
                  </Badge>
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap gap-1">
                    {u.roles.length === 0 ? (
                      <span className="text-xs text-muted-foreground">none</span>
                    ) : (
                      u.roles.map((r) => (
                        <Badge key={r} tone="accent">
                          {r}
                        </Badge>
                      ))
                    )}
                  </div>
                </TableCell>
                <TableCell>
                  {u.tenant_wide ? (
                    <Badge tone="info">tenant-wide</Badge>
                  ) : (
                    <span className="font-mono text-xs">{u.station_ids.length} station(s)</span>
                  )}
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex justify-end gap-1">
                    <PermissionGate permission="users.assign_roles">
                      <Button variant="ghost" size="sm" onClick={() => openScope(u)}>
                        Manage
                      </Button>
                    </PermissionGate>
                    {u.status === 'active' ? (
                      <PermissionGate permission="users.manage">
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={
                            updateStatus.isPending && updateStatus.variables?.userID === u.id
                          }
                          onClick={() => updateStatus.mutate({ userID: u.id, status: 'suspended' })}
                          title="Suspend user"
                        >
                          <ShieldOff className="size-3.5" />
                        </Button>
                      </PermissionGate>
                    ) : u.status === 'suspended' ? (
                      <PermissionGate permission="users.manage">
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={
                            updateStatus.isPending && updateStatus.variables?.userID === u.id
                          }
                          onClick={() => updateStatus.mutate({ userID: u.id, status: 'active' })}
                        >
                          Activate
                        </Button>
                      </PermissionGate>
                    ) : null}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <Dialog open={inviteOpen} onOpenChange={setInviteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Invite a user</DialogTitle>
            <DialogDescription>
              The user is created with{' '}
              <code className="rounded bg-muted px-1 py-0.5 text-xs">invited</code> status. Send
              them the password-reset flow to set their password.
            </DialogDescription>
          </DialogHeader>

          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              if (!inviteForm.email || !inviteForm.full_name) {
                setInviteError('Both fields are required');
                return;
              }
              invite.mutate();
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="full_name">Full name</Label>
              <Input
                id="full_name"
                value={inviteForm.full_name}
                onChange={(e) => setInviteForm({ ...inviteForm, full_name: e.target.value })}
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="email">Email</Label>
              <Input
                id="email"
                type="email"
                value={inviteForm.email}
                onChange={(e) => setInviteForm({ ...inviteForm, email: e.target.value })}
                required
              />
            </div>

            {inviteError ? (
              <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                {inviteError}
              </p>
            ) : null}

            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setInviteOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={invite.isPending}>
                <Mail className="size-4" />
                {invite.isPending ? 'Inviting…' : 'Send invite'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(scope)} onOpenChange={(o: boolean) => !o && setScope(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Manage roles &amp; scope</DialogTitle>
            <DialogDescription>
              {scope ? (
                <>
                  Roles and station scope for <span className="font-medium">{scope.full_name}</span>
                  .
                </>
              ) : null}
            </DialogDescription>
          </DialogHeader>

          {scope ? (
            <div className="flex flex-col gap-5">
              <section>
                <h4 className="mb-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                  Roles
                </h4>
                <div className="flex flex-wrap gap-1.5">
                  {(roles.data?.items ?? []).map((r) => {
                    const has = scope.roles.includes(r.code);
                    return (
                      <PermissionGate key={r.id} permission="users.assign_roles">
                        <button
                          type="button"
                          onClick={() =>
                            has
                              ? revokeRole.mutate({ userID: scope.id, code: r.code })
                              : grantRole.mutate({ userID: scope.id, code: r.code })
                          }
                          className={
                            has
                              ? 'inline-flex items-center gap-1 rounded-full bg-accent/15 px-2.5 py-1 text-xs text-accent disabled:opacity-50'
                              : 'inline-flex items-center gap-1 rounded-full border border-border bg-background px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:border-accent hover:text-accent disabled:opacity-50'
                          }
                        >
                          {has ? <X className="size-3" /> : <Plus className="size-3" />}
                          {r.code}
                        </button>
                      </PermissionGate>
                    );
                  })}
                </div>
              </section>

              <section>
                <h4 className="mb-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                  Station access
                </h4>
                {scope.tenant_wide ? (
                  <p className="text-xs text-muted-foreground">
                    Tenant-wide. Granting a specific station below restricts the user.
                  </p>
                ) : null}
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {(stations.data?.items ?? []).map((s) => {
                    const has = scope.station_ids.includes(s.id);
                    return (
                      <PermissionGate key={s.id} permission="users.assign_roles">
                        <button
                          type="button"
                          onClick={() =>
                            has
                              ? revokeStation.mutate({ userID: scope.id, stationID: s.id })
                              : grantStation.mutate({ userID: scope.id, stationID: s.id })
                          }
                          className={
                            has
                              ? 'inline-flex items-center gap-1 rounded-full bg-accent/15 px-2.5 py-1 text-xs text-accent disabled:opacity-50'
                              : 'inline-flex items-center gap-1 rounded-full border border-border bg-background px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:border-accent hover:text-accent disabled:opacity-50'
                          }
                        >
                          {has ? <X className="size-3" /> : <Plus className="size-3" />}
                          {s.code}
                        </button>
                      </PermissionGate>
                    );
                  })}
                </div>
              </section>
            </div>
          ) : null}

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => setScope(null)}>
              Done
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
