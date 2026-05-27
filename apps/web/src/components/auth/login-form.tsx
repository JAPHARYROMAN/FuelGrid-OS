'use client';

import { useState } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import { zodResolver } from '@hookform/resolvers/zod';
import { useForm } from 'react-hook-form';
import { z } from 'zod';

import { SdkError } from '@fuelgrid/sdk';
import {
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
import { useAuthStore } from '@/stores/auth-store';

const schema = z.object({
  tenant_slug: z
    .string()
    .min(1, 'Required')
    .regex(/^[a-z0-9-]+$/, 'Lowercase letters, digits, and dashes only'),
  email: z.string().email(),
  password: z.string().min(1, 'Required'),
  mfa_code: z.string().optional(),
});

type FormValues = z.infer<typeof schema>;

export function LoginForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const setSession = useAuthStore((s) => s.setSession);

  const [submitError, setSubmitError] = useState<string | null>(null);
  const [mfaRequired, setMfaRequired] = useState(false);

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { tenant_slug: 'demo' },
  });

  async function onSubmit(values: FormValues) {
    setSubmitError(null);
    try {
      const res = await api.login(values);

      if (res.mfa_required) {
        setMfaRequired(true);
        return;
      }

      if (!res.token) {
        setSubmitError('Server did not return a session token.');
        return;
      }

      setSession(res.token, res.expires_at);

      const next = searchParams.get('next');
      const safeNext = next && next.startsWith('/') ? next : '/command-center';
      router.replace(safeNext);
    } catch (err) {
      if (err instanceof SdkError) {
        if (err.status === 401) {
          setSubmitError('Invalid tenant, email, or password.');
        } else if (err.status === 429) {
          setSubmitError('Too many attempts. Wait a few minutes and try again.');
        } else {
          setSubmitError(err.message || 'Login failed.');
        }
        return;
      }
      setSubmitError('Network error. Check your connection and try again.');
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Sign in</CardTitle>
        <CardDescription>Use your tenant slug, email, and password.</CardDescription>
      </CardHeader>

      <CardContent>
        <form className="flex flex-col gap-4" onSubmit={handleSubmit(onSubmit)} noValidate>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="tenant_slug">Tenant</Label>
            <Input
              id="tenant_slug"
              autoComplete="organization"
              autoCapitalize="none"
              spellCheck={false}
              {...register('tenant_slug')}
            />
            {errors.tenant_slug ? (
              <p className="text-xs text-danger">{errors.tenant_slug.message}</p>
            ) : null}
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="email">Email</Label>
            <Input
              id="email"
              type="email"
              autoComplete="email"
              autoCapitalize="none"
              spellCheck={false}
              {...register('email')}
            />
            {errors.email ? <p className="text-xs text-danger">{errors.email.message}</p> : null}
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="password">Password</Label>
            <Input
              id="password"
              type="password"
              autoComplete="current-password"
              {...register('password')}
            />
            {errors.password ? (
              <p className="text-xs text-danger">{errors.password.message}</p>
            ) : null}
          </div>

          {mfaRequired ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="mfa_code">MFA code</Label>
              <Input
                id="mfa_code"
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={6}
                {...register('mfa_code')}
              />
            </div>
          ) : null}

          {submitError ? (
            <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
              {submitError}
            </p>
          ) : null}

          <Button type="submit" disabled={isSubmitting}>
            {isSubmitting ? 'Signing in…' : 'Sign in'}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
