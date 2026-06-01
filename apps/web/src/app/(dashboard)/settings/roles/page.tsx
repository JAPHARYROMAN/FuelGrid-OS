'use client';

import { useQuery } from '@tanstack/react-query';
import { ShieldCheck } from 'lucide-react';

import {
  Badge,
  Card,
  CardContent,
  ErrorState,
  PageHeader,
  Skeleton,
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

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Settings"
        title="Roles & permissions"
        description="System roles ship with FuelGrid OS and are shared by every tenant. The permission matrix is read-only here; assign roles to users from the Users tab."
      />

      {list.isPending ? (
        <Card>
          <CardContent className="flex flex-col gap-2 p-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-14 rounded-lg" />
            ))}
          </CardContent>
        </Card>
      ) : list.isError ? (
        <ErrorState
          title="Couldn't load roles"
          description={String((list.error as Error).message)}
          onRetry={() => list.refetch()}
        />
      ) : (
        <Card>
          <CardContent className="p-0">
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
                      <div className="flex items-center gap-3">
                        <span className="flex size-9 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">
                          <ShieldCheck className="size-4" />
                        </span>
                        <div className="flex flex-col gap-0.5">
                          <span className="font-medium">{r.name}</span>
                          <span className="font-mono text-[11px] text-muted-foreground">
                            {r.code}
                          </span>
                        </div>
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
          </CardContent>
        </Card>
      )}
    </div>
  );
}
