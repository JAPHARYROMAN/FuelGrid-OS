'use client';

import { Suspense, useState } from 'react';
import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';

import { SdkError } from '@fuelgrid/sdk';
import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
  Input,
  Label,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

function ResetPasswordForm() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const [form, setForm] = useState({
    // Pre-fill the token from the email link's ?token= when present.
    token: searchParams.get('token') ?? '',
    new_password: '',
    confirm: '',
  });
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);

    if (!form.token) {
      setError('Reset token is required.');
      return;
    }
    if (form.new_password.length < 12) {
      setError('Password must be at least 12 characters.');
      return;
    }
    if (form.new_password !== form.confirm) {
      setError('Passwords do not match.');
      return;
    }

    setSubmitting(true);
    try {
      await api.confirmPasswordReset({ token: form.token, new_password: form.new_password });
      setDone(true);
    } catch (err) {
      if (err instanceof SdkError && err.status === 400) {
        setError('That reset token is invalid or has expired. Request a new one.');
      } else {
        setError(err instanceof SdkError ? err.message : 'Network error. Try again.');
      }
    } finally {
      setSubmitting(false);
    }
  }

  if (done) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Password updated</CardTitle>
          <CardDescription>
            Your password has been reset and every existing session was revoked. Sign in with your
            new password.
          </CardDescription>
        </CardHeader>
        <CardFooter>
          <Button onClick={() => router.replace('/login')}>Go to sign in</Button>
        </CardFooter>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Reset password</CardTitle>
        <CardDescription>
          Paste the token from your reset email and choose a new password.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form className="flex flex-col gap-4" onSubmit={onSubmit} noValidate>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="token">Reset token</Label>
            <Input
              id="token"
              autoCapitalize="none"
              spellCheck={false}
              value={form.token}
              onChange={(e) => setForm({ ...form, token: e.target.value })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="new_password">New password</Label>
            <Input
              id="new_password"
              type="password"
              autoComplete="new-password"
              value={form.new_password}
              onChange={(e) => setForm({ ...form, new_password: e.target.value })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="confirm">Confirm new password</Label>
            <Input
              id="confirm"
              type="password"
              autoComplete="new-password"
              value={form.confirm}
              onChange={(e) => setForm({ ...form, confirm: e.target.value })}
            />
          </div>

          {error ? (
            <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
              {error}
            </p>
          ) : null}

          <Button type="submit" disabled={submitting}>
            {submitting ? 'Resetting…' : 'Reset password'}
          </Button>
        </form>
      </CardContent>
      <CardFooter>
        <Link href="/login" className="text-sm text-accent underline">
          Back to sign in
        </Link>
      </CardFooter>
    </Card>
  );
}

export default function ResetPasswordPage() {
  // useSearchParams needs a Suspense boundary in the App Router.
  return (
    <Suspense>
      <ResetPasswordForm />
    </Suspense>
  );
}
