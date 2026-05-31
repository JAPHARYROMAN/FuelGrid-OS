'use client';

import * as React from 'react';
import Link from 'next/link';
import { RotateCw } from 'lucide-react';

import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle } from '@fuelgrid/ui';

import { reportError } from '@/lib/sentry';

/**
 * Dashboard segment boundary. Catches render errors thrown anywhere inside the
 * (dashboard) tree, keeping the chrome (sidebar/topbar from the layout) intact
 * while replacing the failed <main> with a friendly card. `reset()` re-renders
 * the segment; the link is an escape hatch back to the command center.
 */
export default function DashboardError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  React.useEffect(() => {
    reportError(error, { boundary: 'dashboard' });
  }, [error]);

  return (
    <div className="grid h-full place-items-center p-6">
      <Card className="w-full max-w-md border-danger/40 bg-danger/5">
        <CardHeader>
          <CardTitle>Something went wrong</CardTitle>
          <CardDescription>
            This screen hit an unexpected error and could not finish loading. The issue has been
            reported to our team.
            {error.digest ? (
              <span className="mt-2 block text-xs text-muted-foreground">
                Reference: {error.digest}
              </span>
            ) : null}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex gap-2">
          <Button variant="primary" onClick={() => reset()}>
            <RotateCw className="size-4" />
            Try again
          </Button>
          <Button variant="outline" asChild>
            <Link href="/command-center">Go to command center</Link>
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
