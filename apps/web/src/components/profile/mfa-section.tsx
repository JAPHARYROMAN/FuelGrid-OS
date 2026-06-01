'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { ShieldCheck } from 'lucide-react';

import { SdkError } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Input,
  Label,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

function errMessage(err: unknown, fallback: string): string {
  return err instanceof SdkError ? err.message : fallback;
}

/**
 * Security section of the profile: enroll in TOTP MFA (manual key + otpauth URI
 * shown as text, QR optional later), confirm with the first code, disable, and
 * view / regenerate one-time backup codes. Reads MFA status from /me.
 */
export function MfaSection() {
  const qc = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: ({ signal }) => api.me(signal) });

  // Enrollment-in-progress secret (only present between enroll and confirm).
  const [pending, setPending] = useState<{ secret: string; otpauth_url: string } | null>(null);
  const [code, setCode] = useState('');
  // Backup codes shown exactly once after confirm / regenerate.
  const [backupCodes, setBackupCodes] = useState<string[] | null>(null);
  const [disableCode, setDisableCode] = useState('');
  const [error, setError] = useState<string | null>(null);

  const refreshMe = () => qc.invalidateQueries({ queryKey: ['me'] });

  const enroll = useMutation({
    mutationFn: () => api.mfaEnroll(),
    onSuccess: (res) => {
      setError(null);
      setBackupCodes(null);
      setPending(res);
    },
    onError: (err) => setError(errMessage(err, 'Could not begin enrollment')),
  });

  const confirm = useMutation({
    mutationFn: () => api.mfaConfirm(code.trim()),
    onSuccess: (res) => {
      setError(null);
      setPending(null);
      setCode('');
      setBackupCodes(res.backup_codes);
      refreshMe();
    },
    onError: (err) => setError(errMessage(err, 'That code was not accepted')),
  });

  const regenerate = useMutation({
    mutationFn: () => api.regenerateBackupCodes(),
    onSuccess: (res) => {
      setError(null);
      setBackupCodes(res.backup_codes);
      refreshMe();
    },
    onError: (err) => setError(errMessage(err, 'Could not regenerate backup codes')),
  });

  const disable = useMutation({
    mutationFn: () => api.mfaDisable(disableCode.trim()),
    onSuccess: () => {
      setError(null);
      setDisableCode('');
      setBackupCodes(null);
      refreshMe();
    },
    onError: (err) => setError(errMessage(err, 'Could not disable MFA')),
  });

  const enabled = me.data?.mfa_enabled ?? false;
  const requiredByRole = me.data?.mfa_required ?? false;
  const remaining = me.data?.mfa_backup_codes_remaining ?? 0;

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between gap-3">
          <div className="flex flex-col gap-1">
            <CardTitle>Two-factor authentication</CardTitle>
            <CardDescription>
              A time-based one-time code from an authenticator app, required at sign-in.
            </CardDescription>
          </div>
          {enabled ? (
            <Badge tone="success">Enabled</Badge>
          ) : requiredByRole ? (
            <Badge tone="danger">Required</Badge>
          ) : (
            <Badge tone="neutral">Off</Badge>
          )}
        </div>
      </CardHeader>
      <CardContent>
        <div className="flex flex-col gap-5">
          {requiredByRole && !enabled ? (
            <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
              Your role requires two-factor authentication. Enroll now to keep access to
              administrative and finance functions.
            </p>
          ) : null}

          {error ? (
            <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
              {error}
            </p>
          ) : null}

          {/* One-time backup codes — shown only immediately after issue. */}
          {backupCodes ? (
            <div className="flex flex-col gap-2 rounded-lg border border-border bg-muted/40 p-4">
              <p className="text-sm font-medium">Save your backup codes</p>
              <p className="text-xs text-muted-foreground">
                Each code works once. Store them somewhere safe — they will not be shown again.
              </p>
              <ul className="grid grid-cols-2 gap-1 pt-1 font-mono text-sm tabular-nums">
                {backupCodes.map((c) => (
                  <li key={c}>{c}</li>
                ))}
              </ul>
            </div>
          ) : null}

          {/* State machine: not enrolled -> enrolling (pending) -> enabled. */}
          {!enabled && !pending ? (
            <div>
              <Button onClick={() => enroll.mutate()} disabled={enroll.isPending}>
                {enroll.isPending ? 'Starting…' : 'Set up authenticator'}
              </Button>
            </div>
          ) : null}

          {pending ? (
            <div className="flex max-w-md flex-col gap-3">
              <div className="flex flex-col gap-1">
                <Label>Manual entry key</Label>
                <code className="break-all rounded-md bg-muted px-3 py-2 font-mono text-sm tabular-nums">
                  {pending.secret}
                </code>
              </div>
              <div className="flex flex-col gap-1">
                <Label>otpauth URI</Label>
                <code className="break-all rounded-md bg-muted px-3 py-2 font-mono text-xs">
                  {pending.otpauth_url}
                </code>
                <p className="text-xs text-muted-foreground">
                  Add this to your authenticator app (paste the key or the URI), then enter the
                  6-digit code it shows to finish.
                </p>
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="mfa_confirm_code">Verification code</Label>
                <Input
                  id="mfa_confirm_code"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  maxLength={6}
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                />
              </div>
              <div className="flex gap-2">
                <Button
                  onClick={() => confirm.mutate()}
                  disabled={confirm.isPending || code.trim().length < 6}
                >
                  {confirm.isPending ? 'Verifying…' : 'Enable'}
                </Button>
                <Button
                  variant="ghost"
                  onClick={() => {
                    setPending(null);
                    setCode('');
                    setError(null);
                  }}
                >
                  Cancel
                </Button>
              </div>
            </div>
          ) : null}

          {enabled ? (
            <div className="flex flex-col gap-5">
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <ShieldCheck className="h-4 w-4 text-success" />
                <span>
                  {remaining} backup code{remaining === 1 ? '' : 's'} remaining.
                </span>
              </div>

              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  onClick={() => regenerate.mutate()}
                  disabled={regenerate.isPending}
                >
                  {regenerate.isPending ? 'Generating…' : 'Regenerate backup codes'}
                </Button>
              </div>

              <div className="flex max-w-md flex-col gap-1.5 border-t border-border pt-4">
                <Label htmlFor="mfa_disable_code">Disable two-factor authentication</Label>
                <p className="text-xs text-muted-foreground">
                  Enter a current authenticator code or a backup code to confirm.
                </p>
                <div className="flex gap-2 pt-1">
                  <Input
                    id="mfa_disable_code"
                    autoComplete="one-time-code"
                    placeholder="123456 or a backup code"
                    value={disableCode}
                    onChange={(e) => setDisableCode(e.target.value)}
                  />
                  <Button
                    variant="danger"
                    onClick={() => disable.mutate()}
                    disabled={disable.isPending || disableCode.trim().length === 0}
                  >
                    {disable.isPending ? 'Disabling…' : 'Disable'}
                  </Button>
                </div>
              </div>
            </div>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}
