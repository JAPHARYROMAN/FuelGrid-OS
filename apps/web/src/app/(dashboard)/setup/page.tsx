'use client';

import * as React from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { ArrowRight, Check, Circle, Rocket } from 'lucide-react';

import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  PageHeader,
  Skeleton,
  cn,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

/**
 * Guided onboarding. Walks a brand-new tenant through the setup entities in the
 * order the data model requires them (each later step depends on the earlier
 * ones existing). Done/not-done state is computed live from API counts so the
 * checklist reflects the real database, never a demo seed.
 *
 * The first station is used as the scope for the station-scoped entities
 * (tanks/pumps/nozzles/employees/teams/rotation) — that's enough to get a
 * single-site tenant fully operational; multi-site setup repeats per station.
 */
export default function SetupPage() {
  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  const firstStationID = stations.data?.items[0]?.id ?? '';

  const companies = useQuery({
    queryKey: ['companies'],
    queryFn: ({ signal }) => api.listCompanies(signal),
  });
  const regions = useQuery({
    queryKey: ['regions', 'all'],
    queryFn: ({ signal }) => api.listRegions({}, signal),
  });
  const tanks = useQuery({
    queryKey: ['tanks', firstStationID],
    queryFn: ({ signal }) => api.listTanks({ stationID: firstStationID }, signal),
    enabled: Boolean(firstStationID),
  });
  const pumps = useQuery({
    queryKey: ['pumps', firstStationID],
    queryFn: ({ signal }) => api.listPumps({ stationID: firstStationID }, signal),
    enabled: Boolean(firstStationID),
  });
  const nozzles = useQuery({
    queryKey: ['nozzles', firstStationID],
    queryFn: ({ signal }) => api.listNozzles({ stationID: firstStationID }, signal),
    enabled: Boolean(firstStationID),
  });
  const products = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
  });
  const suppliers = useQuery({
    queryKey: ['suppliers'],
    queryFn: ({ signal }) => api.listSuppliers(signal),
  });
  const employees = useQuery({
    queryKey: ['employees', firstStationID],
    queryFn: ({ signal }) => api.listEmployees(firstStationID, signal),
    enabled: Boolean(firstStationID),
  });
  const teams = useQuery({
    queryKey: ['teams', firstStationID],
    queryFn: ({ signal }) => api.listTeams(firstStationID, signal),
    enabled: Boolean(firstStationID),
  });
  const anchor = useQuery({
    queryKey: ['rotation-anchor', firstStationID],
    queryFn: ({ signal }) => api.getRotationAnchor(firstStationID, signal),
    enabled: Boolean(firstStationID),
  });

  // A step is "pending" while its query is still resolving (or blocked on a
  // prerequisite that isn't there yet) so we don't flash a misleading state.
  const hasStation = Boolean(firstStationID);

  interface Step {
    key: string;
    title: string;
    description: string;
    href: string;
    cta: string;
    done: boolean;
    pending: boolean;
    /** Some steps can't be evaluated until a prerequisite exists. */
    blocked: boolean;
    count?: number;
  }

  const steps: Step[] = [
    {
      key: 'company',
      title: 'Company',
      description: 'Your operating entity — currency, timezone, and legal name.',
      href: '/settings/companies',
      cta: 'Add a company',
      done: (companies.data?.count ?? companies.data?.items.length ?? 0) > 0,
      pending: companies.isPending,
      blocked: false,
      count: companies.data?.count ?? companies.data?.items.length,
    },
    {
      key: 'region',
      title: 'Region',
      description: 'A geographic grouping of stations within a company.',
      href: '/settings/regions',
      cta: 'Add a region',
      done: (regions.data?.count ?? regions.data?.items.length ?? 0) > 0,
      pending: regions.isPending,
      blocked: false,
      count: regions.data?.count ?? regions.data?.items.length,
    },
    {
      key: 'station',
      title: 'Station',
      description: 'A physical fuel site. Everything below hangs off a station.',
      href: '/settings/stations',
      cta: 'Add a station',
      done: hasStation,
      pending: stations.isPending,
      blocked: false,
      count: stations.data?.count ?? stations.data?.items.length,
    },
    {
      key: 'tanks',
      title: 'Tanks',
      description: 'Storage tanks at the station, each tied to a product.',
      href: '/settings/tanks',
      cta: 'Add tanks',
      done: (tanks.data?.count ?? tanks.data?.items.length ?? 0) > 0,
      pending: hasStation && tanks.isPending,
      blocked: !hasStation,
      count: tanks.data?.count ?? tanks.data?.items.length,
    },
    {
      key: 'pumps',
      title: 'Pumps',
      description: 'Dispensing units on the forecourt.',
      href: '/settings/pumps',
      cta: 'Add pumps',
      done: (pumps.data?.count ?? pumps.data?.items.length ?? 0) > 0,
      pending: hasStation && pumps.isPending,
      blocked: !hasStation,
      count: pumps.data?.count ?? pumps.data?.items.length,
    },
    {
      key: 'nozzles',
      title: 'Nozzles',
      description: 'Each nozzle draws a product from a tank — add them under Pumps.',
      href: '/settings/pumps',
      cta: 'Add nozzles',
      done: (nozzles.data?.count ?? nozzles.data?.items.length ?? 0) > 0,
      pending: hasStation && nozzles.isPending,
      blocked: !hasStation,
      count: nozzles.data?.count ?? nozzles.data?.items.length,
    },
    {
      key: 'products',
      title: 'Products',
      description: 'Fuel grades you sell, with default price and tax rate.',
      href: '/settings/products',
      cta: 'Add products',
      done: (products.data?.count ?? products.data?.items.length ?? 0) > 0,
      pending: products.isPending,
      blocked: false,
      count: products.data?.count ?? products.data?.items.length,
    },
    {
      key: 'suppliers',
      title: 'Suppliers',
      description: 'Fuel suppliers you raise purchase orders against.',
      href: '/settings/suppliers',
      cta: 'Add suppliers',
      done: (suppliers.data?.count ?? suppliers.data?.items.length ?? 0) > 0,
      pending: suppliers.isPending,
      blocked: false,
      count: suppliers.data?.count ?? suppliers.data?.items.length,
    },
    {
      key: 'employees',
      title: 'Employees',
      description: 'Pump attendants, cashiers, and supervisors at the station.',
      href: '/settings/employees',
      cta: 'Add employees',
      done: (employees.data?.count ?? employees.data?.items.length ?? 0) > 0,
      pending: hasStation && employees.isPending,
      blocked: !hasStation,
      count: employees.data?.count ?? employees.data?.items.length,
    },
    {
      key: 'teams',
      title: 'Teams',
      description: 'Rotation teams that staff shifts on a recurring cycle.',
      href: '/settings/teams',
      cta: 'Create teams',
      done: (teams.data?.count ?? teams.data?.items.length ?? 0) > 0,
      pending: hasStation && teams.isPending,
      blocked: !hasStation,
      count: teams.data?.count ?? teams.data?.items.length,
    },
    {
      key: 'rotation',
      title: 'Rotation anchor',
      description: 'The reference date that pins the team rotation cycle.',
      href: '/settings/teams',
      cta: 'Set the anchor',
      done: Boolean(anchor.data?.rotation_anchor_date),
      pending: hasStation && anchor.isPending,
      blocked: !hasStation,
    },
  ];

  const evaluated = steps.filter((s) => !s.pending && !s.blocked);
  const doneCount = evaluated.filter((s) => s.done).length;
  const total = steps.length;
  const allDone = doneCount === total && steps.every((s) => !s.pending && !s.blocked);
  const overallPending = stations.isPending || companies.isPending;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Getting started"
        title="Setup"
        description="Set up your tenant end-to-end, in order. Each step links to where you complete it; the status updates from your live data."
        actions={
          overallPending ? (
            <Skeleton className="h-7 w-24 rounded-full" />
          ) : (
            <Badge tone={allDone ? 'success' : 'neutral'}>
              {doneCount} / {total} done
            </Badge>
          )
        }
      />

      {allDone ? (
        <Card>
          <CardContent className="flex items-center gap-3 py-5">
            <span className="flex size-10 items-center justify-center rounded-full bg-success/15 text-success">
              <Rocket className="size-5" />
            </span>
            <div className="flex flex-col">
              <p className="font-medium text-foreground">Your tenant is ready</p>
              <p className="text-sm text-muted-foreground">
                Every required entity exists. Head to the Command Center to start operating.
              </p>
            </div>
            <Link
              href="/command-center"
              className="ml-auto inline-flex items-center gap-1 text-sm font-medium text-accent hover:underline"
            >
              Command Center
              <ArrowRight className="size-4" />
            </Link>
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Setup checklist</CardTitle>
          <p className="text-sm text-muted-foreground">
            Complete these in order — later steps depend on the earlier ones.
          </p>
        </CardHeader>
        <CardContent className="flex flex-col divide-y divide-border">
          {steps.map((step, i) => (
            <div key={step.key} className="flex items-center gap-4 py-3.5 first:pt-0 last:pb-0">
              <span
                className={cn(
                  'flex size-8 shrink-0 items-center justify-center rounded-full border text-sm font-medium',
                  step.done
                    ? 'border-transparent bg-success/15 text-success'
                    : 'border-border bg-muted text-muted-foreground',
                )}
                aria-hidden
              >
                {step.pending ? (
                  <span className="size-3 animate-pulse rounded-full bg-muted-foreground/40" />
                ) : step.done ? (
                  <Check className="size-4" />
                ) : (
                  <span className="font-mono text-xs tabular-nums">{i + 1}</span>
                )}
              </span>

              <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                <div className="flex items-center gap-2">
                  <span className="font-medium text-foreground">{step.title}</span>
                  {step.pending ? null : step.done ? (
                    <Badge tone="success">Done</Badge>
                  ) : step.blocked ? (
                    <Badge tone="neutral">Needs a station first</Badge>
                  ) : (
                    <Badge tone="warning">To do</Badge>
                  )}
                  {!step.pending && typeof step.count === 'number' && step.count > 0 ? (
                    <span className="font-mono text-xs text-muted-foreground tabular-nums">
                      {step.count}
                    </span>
                  ) : null}
                </div>
                <p className="truncate text-sm text-muted-foreground">{step.description}</p>
              </div>

              <Link
                href={step.href}
                className={cn(
                  'inline-flex shrink-0 items-center gap-1 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                  step.done
                    ? 'text-muted-foreground hover:bg-muted hover:text-foreground'
                    : 'bg-accent-muted/70 text-foreground hover:bg-accent-muted',
                )}
              >
                {step.done ? (
                  <>
                    <Circle className="size-3.5" />
                    Review
                  </>
                ) : (
                  <>
                    {step.cta}
                    <ArrowRight className="size-4" />
                  </>
                )}
              </Link>
            </div>
          ))}
        </CardContent>
      </Card>
    </div>
  );
}
