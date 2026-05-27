'use client';

import { useQuery } from '@tanstack/react-query';

import {
  Badge,
  ErrorState,
  LoadingState,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

export default function RolesPage() {
  const list = useQuery({
    queryKey: ['roles'],
    queryFn: ({ signal }) => api.listRoles(signal),
  });

  if (list.isPending) return <LoadingState />;
  if (list.isError) {
    return (
      <ErrorState
        title="Couldn't load roles"
        description={String((list.error as Error).message)}
        onRetry={() => list.refetch()}
      />
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm text-muted-foreground">
        System roles ship with FuelGrid OS and are shared by every tenant. The permission matrix is
        read-only here; assign roles to users from the Users tab.
      </p>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Role</TableHead>
            <TableHead>Description</TableHead>
            <TableHead>Permissions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {(list.data?.items ?? []).map((r) => (
            <TableRow key={r.id}>
              <TableCell>
                <div className="flex flex-col gap-0.5">
                  <span className="font-medium">{r.name}</span>
                  <span className="font-mono text-[11px] text-muted-foreground">{r.code}</span>
                </div>
              </TableCell>
              <TableCell className="max-w-md text-muted-foreground">
                {r.description ?? '—'}
              </TableCell>
              <TableCell>
                <div className="flex max-w-2xl flex-wrap gap-1">
                  {r.permissions.length === 0 ? (
                    <span className="text-xs text-muted-foreground">none</span>
                  ) : (
                    r.permissions.map((p) => (
                      <Badge key={p} tone="neutral">
                        {p}
                      </Badge>
                    ))
                  )}
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
