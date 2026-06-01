'use client';

import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ScrollText } from 'lucide-react';

import {
  Badge,
  Button,
  Card,
  CardContent,
  EmptyState,
  ErrorState,
  Input,
  Label,
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

interface Filters {
  action: string;
  entity_type: string;
  entity_id: string;
  since: string;
  until: string;
}

const blankFilters: Filters = {
  action: '',
  entity_type: '',
  entity_id: '',
  since: '',
  until: '',
};

export default function AuditPage() {
  const [filters, setFilters] = useState<Filters>(blankFilters);
  const [appliedFilters, setAppliedFilters] = useState<Filters>(blankFilters);

  const list = useQuery({
    queryKey: ['audit', appliedFilters],
    queryFn: ({ signal }) =>
      api.listAuditLogs(
        {
          action: appliedFilters.action || undefined,
          entityType: appliedFilters.entity_type || undefined,
          entityID: appliedFilters.entity_id || undefined,
          since: appliedFilters.since ? new Date(appliedFilters.since).toISOString() : undefined,
          until: appliedFilters.until ? new Date(appliedFilters.until).toISOString() : undefined,
          limit: 100,
        },
        signal,
      ),
  });

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Monitor"
        title="Audit log"
        description="Append-only record of sensitive actions. Filtered by tenant automatically."
      />

      <form
        className="grid grid-cols-2 gap-3 rounded-xl border border-border bg-card/40 p-4 md:grid-cols-5"
        onSubmit={(e) => {
          e.preventDefault();
          setAppliedFilters(filters);
        }}
      >
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="action">Action</Label>
          <Input
            id="action"
            placeholder="e.g. user.role.granted"
            value={filters.action}
            onChange={(e) => setFilters({ ...filters, action: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="entity_type">Entity type</Label>
          <Input
            id="entity_type"
            placeholder="company, station…"
            value={filters.entity_type}
            onChange={(e) => setFilters({ ...filters, entity_type: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="entity_id">Entity id</Label>
          <Input
            id="entity_id"
            placeholder="uuid"
            value={filters.entity_id}
            onChange={(e) => setFilters({ ...filters, entity_id: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="since">Since</Label>
          <Input
            id="since"
            type="datetime-local"
            value={filters.since}
            onChange={(e) => setFilters({ ...filters, since: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="until">Until</Label>
          <Input
            id="until"
            type="datetime-local"
            value={filters.until}
            onChange={(e) => setFilters({ ...filters, until: e.target.value })}
          />
        </div>
        <div className="col-span-2 flex gap-2 md:col-span-5">
          <Button type="submit">Apply</Button>
          <Button
            type="button"
            variant="ghost"
            onClick={() => {
              setFilters(blankFilters);
              setAppliedFilters(blankFilters);
            }}
          >
            Reset
          </Button>
        </div>
      </form>

      {list.isPending ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-14 rounded-lg" />
          ))}
        </div>
      ) : list.isError ? (
        <ErrorState
          title="Couldn't load audit logs"
          description={String((list.error as Error).message)}
          onRetry={() => list.refetch()}
        />
      ) : (list.data?.items?.length ?? 0) === 0 ? (
        <EmptyState
          title="No matching audit entries"
          description="Sensitive actions show up here as they happen. Try widening the date range or clearing filters."
          icon={<ScrollText />}
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>When</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead>Entity</TableHead>
                  <TableHead>Entity id</TableHead>
                  <TableHead>Actor</TableHead>
                  <TableHead>Request</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.data!.items.map((e) => (
                  <TableRow key={e.id}>
                    <TableCell className="whitespace-nowrap font-mono text-xs">
                      {new Date(e.occurred_at).toLocaleString()}
                    </TableCell>
                    <TableCell>
                      <Badge tone="accent">{e.action}</Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{e.entity_type}</TableCell>
                    <TableCell className="font-mono text-[11px] text-muted-foreground">
                      {e.entity_id ?? '—'}
                    </TableCell>
                    <TableCell className="font-mono text-[11px] text-muted-foreground">
                      {e.actor_id ?? '—'}
                    </TableCell>
                    <TableCell className="font-mono text-[11px] text-muted-foreground">
                      {e.request_id ?? '—'}
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
