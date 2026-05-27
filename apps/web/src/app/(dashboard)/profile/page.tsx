'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { SdkError } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
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

import { api } from '@/lib/api';

export default function ProfilePage() {
  const qc = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: ({ signal }) => api.me(signal) });
  const sessions = useQuery({
    queryKey: ['me', 'sessions'],
    queryFn: ({ signal }) => api.listMySessions(signal),
  });

  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeMySession(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['me', 'sessions'] }),
  });

  const [pwdForm, setPwdForm] = useState({ old_password: '', new_password: '' });
  const [pwdMessage, setPwdMessage] = useState<string | null>(null);
  const [pwdError, setPwdError] = useState<string | null>(null);

  const changePwd = useMutation({
    mutationFn: () => api.changeMyPassword(pwdForm),
    onSuccess: () => {
      setPwdMessage('Password updated.');
      setPwdError(null);
      setPwdForm({ old_password: '', new_password: '' });
    },
    onError: (err) => {
      setPwdError(err instanceof SdkError ? err.message : 'Could not change password');
      setPwdMessage(null);
    },
  });

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Profile</h1>
        <p className="text-sm text-muted-foreground">Your session, devices, and password.</p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>Identity</CardTitle>
          <CardDescription>Information about the current session.</CardDescription>
        </CardHeader>
        <CardContent>
          {me.isPending ? (
            <LoadingState />
          ) : me.isError ? (
            <ErrorState
              title="Couldn't load identity"
              description={String((me.error as Error).message)}
              onRetry={() => me.refetch()}
            />
          ) : (
            <dl className="grid grid-cols-1 gap-4 text-sm md:grid-cols-3">
              <div className="flex flex-col gap-1">
                <dt className="text-xs uppercase tracking-wider text-muted-foreground">User</dt>
                <dd className="font-mono text-xs tabular-nums">{me.data.user_id}</dd>
              </div>
              <div className="flex flex-col gap-1">
                <dt className="text-xs uppercase tracking-wider text-muted-foreground">Tenant</dt>
                <dd className="font-mono text-xs tabular-nums">{me.data.tenant_id}</dd>
              </div>
              <div className="flex flex-col gap-1">
                <dt className="text-xs uppercase tracking-wider text-muted-foreground">MFA</dt>
                <dd>{me.data.mfa_satisfied ? 'Satisfied' : 'Not required'}</dd>
              </div>
            </dl>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Active sessions</CardTitle>
          <CardDescription>
            Each row is a logged-in device. Revoke the ones you don't recognise.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {sessions.isPending ? (
            <LoadingState />
          ) : sessions.isError ? (
            <ErrorState
              title="Couldn't load sessions"
              description={String((sessions.error as Error).message)}
              onRetry={() => sessions.refetch()}
            />
          ) : (sessions.data?.items?.length ?? 0) === 0 ? (
            <EmptyState title="Just this device" description="No other active sessions." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Issued</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead>User agent</TableHead>
                  <TableHead>Current</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sessions.data!.items.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell className="whitespace-nowrap font-mono text-xs">
                      {new Date(s.issued_at).toLocaleString()}
                    </TableCell>
                    <TableCell className="whitespace-nowrap font-mono text-xs">
                      {new Date(s.expires_at).toLocaleString()}
                    </TableCell>
                    <TableCell className="max-w-md truncate text-xs text-muted-foreground">
                      {s.user_agent ?? '—'}
                    </TableCell>
                    <TableCell>
                      {s.is_current ? <Badge tone="success">this device</Badge> : null}
                    </TableCell>
                    <TableCell className="text-right">
                      {!s.is_current ? (
                        <Button variant="ghost" size="sm" onClick={() => revoke.mutate(s.id)}>
                          Revoke
                        </Button>
                      ) : null}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Change password</CardTitle>
          <CardDescription>Minimum 12 characters.</CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="flex max-w-md flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              if (!pwdForm.old_password || !pwdForm.new_password) {
                setPwdError('Both fields are required');
                return;
              }
              changePwd.mutate();
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="old_password">Current password</Label>
              <Input
                id="old_password"
                type="password"
                value={pwdForm.old_password}
                onChange={(e) => setPwdForm({ ...pwdForm, old_password: e.target.value })}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="new_password">New password</Label>
              <Input
                id="new_password"
                type="password"
                value={pwdForm.new_password}
                onChange={(e) => setPwdForm({ ...pwdForm, new_password: e.target.value })}
              />
            </div>

            {pwdError ? (
              <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                {pwdError}
              </p>
            ) : null}
            {pwdMessage ? (
              <p className="rounded-md bg-success/10 px-3 py-2 text-sm text-success" role="status">
                {pwdMessage}
              </p>
            ) : null}

            <Button type="submit" disabled={changePwd.isPending} className="self-start">
              {changePwd.isPending ? 'Saving…' : 'Change password'}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
