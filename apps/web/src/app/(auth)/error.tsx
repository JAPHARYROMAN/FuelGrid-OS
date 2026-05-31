'use client';

import * as React from 'react';

import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle } from '@fuelgrid/ui';

import { reportError } from '@/lib/sentry';

/**
 * Auth segment boundary. Minimal by design — the (auth) layout already centers
 * a single card, so a render error here (e.g. login/MFA form) gets a friendly
 * retry without leaking a stack trace to unauthenticated users.
 */
export default function AuthError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  React.useEffect(() => {
    reportError(error, { boundary: 'auth' });
  }, [error]);

  return (
    <Card className="border-danger/40 bg-danger/5">
      <CardHeader>
        <CardTitle>Something went wrong</CardTitle>
        <CardDescription>
          We hit an unexpected error. Please try again.
          {error.digest ? (
            <span className="mt-2 block text-xs text-muted-foreground">
              Reference: {error.digest}
            </span>
          ) : null}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Button variant="primary" onClick={() => reset()}>
          Try again
        </Button>
      </CardContent>
    </Card>
  );
}
