'use client';

import { useState } from 'react';
import Link from 'next/link';

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

export default function ForgotPasswordPage() {
  const [form, setForm] = useState({ tenant_slug: 'demo', email: '' });
  const [submitting, setSubmitting] = useState(false);
  // The backend always returns 202 regardless of whether the email
  // matched — the UI shows the same confirmation either way. Deliberate:
  // it prevents account enumeration.
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!form.tenant_slug || !form.email) {
      setError('Tenant and email are required.');
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await api.requestPasswordReset(form);
      setDone(true);
    } catch (err) {
      setError(err instanceof SdkError ? err.message : 'Network error. Try again.');
    } finally {
      setSubmitting(false);
    }
  }

  if (done) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Check your email</CardTitle>
          <CardDescription>
            If an account matches {form.email}, a reset link is on its way. The link contains a
            one-time token that expires in an hour.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">
            In local development, the reset token is logged by the API rather than emailed — copy it
            into the{' '}
            <Link
              href="/reset-password"
              className="font-medium text-accent underline-offset-2 hover:underline"
            >
              reset form
            </Link>
            .
          </p>
        </CardContent>
        <CardFooter>
          <Link
            href="/login"
            className="text-xs text-muted-foreground underline-offset-2 hover:underline"
          >
            Back to sign in
          </Link>
        </CardFooter>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Forgot password</CardTitle>
        <CardDescription>We&apos;ll send a one-time reset link to your email.</CardDescription>
      </CardHeader>
      <CardContent>
        <form className="flex flex-col gap-4" onSubmit={onSubmit} noValidate>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="tenant_slug">Tenant</Label>
            <Input
              id="tenant_slug"
              autoCapitalize="none"
              spellCheck={false}
              value={form.tenant_slug}
              onChange={(e) => setForm({ ...form, tenant_slug: e.target.value })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="email">Email</Label>
            <Input
              id="email"
              type="email"
              autoComplete="email"
              autoCapitalize="none"
              spellCheck={false}
              value={form.email}
              onChange={(e) => setForm({ ...form, email: e.target.value })}
            />
          </div>

          {error ? (
            <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
              {error}
            </p>
          ) : null}

          <Button type="submit" disabled={submitting}>
            {submitting ? 'Sending…' : 'Send reset link'}
          </Button>
        </form>
      </CardContent>
      <CardFooter>
        <Link
          href="/login"
          className="text-xs text-muted-foreground underline-offset-2 hover:underline"
        >
          Back to sign in
        </Link>
      </CardFooter>
    </Card>
  );
}
