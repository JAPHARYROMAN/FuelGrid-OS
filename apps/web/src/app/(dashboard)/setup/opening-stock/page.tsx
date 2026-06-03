'use client';

import * as React from 'react';
import Link from 'next/link';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Check, RefreshCw, Save, TriangleAlert } from 'lucide-react';

import { SdkError, type StockMovement, type Tank } from '@fuelgrid/sdk';
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
import { formatLitres } from '@/lib/money';

interface OpeningStockRow {
  tank: Tank;
  opening?: StockMovement;
  book_balance: string;
}

interface FormState {
  litres: string;
  notes: string;
}

function isOpeningMovement(m: StockMovement) {
  return (
    m.movement_type === 'opening' &&
    m.status === 'posted' &&
    (!m.source_ref_type || m.source_ref_type !== 'correction')
  );
}

export default function OpeningStockPage() {
  const qc = useQueryClient();
  const [stationID, setStationID] = React.useState('');
  const [forms, setForms] = React.useState<Record<string, FormState>>({});
  const [formErrors, setFormErrors] = React.useState<Record<string, string>>({});

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  const products = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
  });

  const effectiveStation = stationID || stations.data?.items[0]?.id || '';

  const rows = useQuery({
    queryKey: ['opening-stock', effectiveStation],
    enabled: Boolean(effectiveStation),
    queryFn: async ({ signal }) => {
      const tankPage = await api.listTanks({ stationID: effectiveStation }, signal);
      const items = await Promise.all(
        tankPage.items.map(async (tank) => {
          const [ledger, balance] = await Promise.all([
            api.listTankLedger(tank.id, { limit: 100, offset: 0 }, signal),
            api.getTankBookBalance(tank.id, signal),
          ]);
          return {
            tank,
            opening: ledger.items.find(isOpeningMovement),
            book_balance: balance.book_balance,
          } satisfies OpeningStockRow;
        }),
      );
      return items;
    },
  });

  const setOpening = useMutation({
    mutationFn: ({ tankID, litres, notes }: { tankID: string; litres: string; notes?: string }) =>
      api.setTankOpeningBalance(tankID, { litres, notes }),
    onSuccess: (_movement, vars) => {
      setForms((prev) => {
        const next = { ...prev };
        delete next[vars.tankID];
        return next;
      });
      setFormErrors((prev) => {
        const next = { ...prev };
        delete next[vars.tankID];
        return next;
      });
      qc.invalidateQueries({ queryKey: ['opening-stock', effectiveStation] });
      qc.invalidateQueries({ queryKey: ['setup-checklist'] });
    },
  });

  const reviewOpeningStock = useMutation({
    mutationFn: () => api.updateSetupStep({ step_code: 'opening_stock', status: 'completed' }),
    onSuccess: (checklist) => {
      qc.setQueryData(['setup-checklist'], checklist);
    },
  });

  const productLookup = React.useMemo(
    () => new Map((products.data?.items ?? []).map((p) => [p.id, p])),
    [products.data],
  );

  const openedCount = rows.data?.filter((r) => r.opening).length ?? 0;
  const totalTanks = rows.data?.length ?? 0;
  const allOpened = totalTanks > 0 && openedCount === totalTanks;
  const noStations = (stations.data?.items.length ?? 0) === 0;

  function setForm(tankID: string, patch: Partial<FormState>) {
    setForms((prev) => ({
      ...prev,
      [tankID]: { litres: '', notes: '', ...prev[tankID], ...patch },
    }));
  }

  function submit(tankID: string) {
    const form = forms[tankID] ?? { litres: '', notes: '' };
    const litres = form.litres.trim();
    if (!litres || Number.isNaN(Number(litres)) || Number(litres) < 0) {
      setFormErrors((prev) => ({
        ...prev,
        [tankID]: 'Litres must be a non-negative decimal',
      }));
      return;
    }
    setFormErrors((prev) => {
      const next = { ...prev };
      delete next[tankID];
      return next;
    });
    setOpening.mutate({
      tankID,
      litres,
      notes: form.notes.trim() || undefined,
    });
  }

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Setup"
        title="Opening stock"
        description="Seed the first ledger balance for each tank before station operations start."
        actions={
          <>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="station">Station</Label>
              <select
                id="station"
                className="h-10 min-w-56 rounded-md border border-border bg-background px-3 text-sm"
                value={effectiveStation}
                onChange={(e) => setStationID(e.target.value)}
                disabled={noStations}
              >
                {(stations.data?.items ?? []).map((st) => (
                  <option key={st.id} value={st.id}>
                    {st.name} ({st.code})
                  </option>
                ))}
              </select>
            </div>
            <Badge tone={allOpened ? 'success' : 'neutral'}>
              {openedCount} / {totalTanks} opened
            </Badge>
            <Button asChild variant="ghost">
              <Link href="/setup">
                <ArrowLeft className="size-4" />
                Setup
              </Link>
            </Button>
          </>
        }
      />

      {allOpened ? (
        <Card>
          <CardContent className="flex flex-wrap items-center gap-3 py-5">
            <span className="flex size-10 items-center justify-center rounded-full bg-success/15 text-success">
              <Check className="size-5" />
            </span>
            <div className="flex min-w-0 flex-1 flex-col">
              <p className="font-medium text-foreground">Opening stock is complete</p>
              <p className="text-sm text-muted-foreground">
                Every tank at this station has a posted opening movement.
              </p>
            </div>
            <Button
              variant="ghost"
              onClick={() => reviewOpeningStock.mutate()}
              disabled={reviewOpeningStock.isPending}
            >
              <Check className="size-4" />
              {reviewOpeningStock.isPending ? 'Saving...' : 'Review step'}
            </Button>
          </CardContent>
        </Card>
      ) : null}

      {noStations ? (
        <EmptyState
          title="No stations yet"
          description="Create a station before setting opening stock."
        />
      ) : rows.isPending || stations.isPending ? (
        <Card>
          <CardContent className="flex flex-col gap-2 p-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-14 rounded-lg" />
            ))}
          </CardContent>
        </Card>
      ) : rows.isError ? (
        <ErrorState
          title="Couldn't load opening stock"
          description={String((rows.error as Error).message)}
          onRetry={() => rows.refetch()}
        />
      ) : totalTanks === 0 ? (
        <EmptyState
          title="No tanks at this station"
          description="Create tanks before setting opening stock."
          action={
            <Button asChild>
              <Link href="/settings/tanks">Add tanks</Link>
            </Button>
          }
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Tank</TableHead>
                  <TableHead>Product</TableHead>
                  <TableHead className="text-right">Book balance</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Opening litres</TableHead>
                  <TableHead>Notes</TableHead>
                  <TableHead className="text-right">Action</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.data!.map((row) => {
                  const product = productLookup.get(row.tank.product_id);
                  const form = forms[row.tank.id] ?? { litres: '', notes: '' };
                  const isSaving =
                    setOpening.isPending && setOpening.variables?.tankID === row.tank.id;
                  const apiError =
                    setOpening.isError && setOpening.variables?.tankID === row.tank.id
                      ? setOpening.error instanceof SdkError
                        ? setOpening.error.message
                        : 'Could not set opening stock'
                      : null;
                  const formError = formErrors[row.tank.id] ?? apiError;
                  return (
                    <TableRow key={row.tank.id}>
                      <TableCell>
                        <div className="flex flex-col">
                          <span className="font-medium">{row.tank.name}</span>
                          <span className="font-mono text-xs text-muted-foreground">
                            {row.tank.code}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <span className="inline-flex items-center gap-2">
                          <span
                            className="inline-block size-3 rounded-full border border-border"
                            style={{ backgroundColor: product?.color ?? '#64748b' }}
                            aria-hidden
                          />
                          {product?.name ?? 'Product'}
                        </span>
                      </TableCell>
                      <TableCell className="text-right font-mono tabular-nums">
                        {formatLitres(row.book_balance)}
                      </TableCell>
                      <TableCell>
                        {row.opening ? (
                          <Badge tone="success">Opened</Badge>
                        ) : (
                          <Badge tone="warning">Missing</Badge>
                        )}
                      </TableCell>
                      <TableCell className="min-w-40">
                        {row.opening ? (
                          <span className="font-mono tabular-nums">
                            {formatLitres(row.opening.litres)}
                          </span>
                        ) : (
                          <div className="flex flex-col gap-1">
                            <Input
                              type="number"
                              min="0"
                              step="0.001"
                              value={form.litres}
                              onChange={(e) => setForm(row.tank.id, { litres: e.target.value })}
                              placeholder="0.000"
                            />
                            {formError ? (
                              <span className="inline-flex items-center gap-1 text-xs text-danger">
                                <TriangleAlert className="size-3" />
                                {formError}
                              </span>
                            ) : null}
                          </div>
                        )}
                      </TableCell>
                      <TableCell className="min-w-52">
                        {row.opening ? (
                          <span className="text-sm text-muted-foreground">
                            {row.opening.notes ?? 'Recorded'}
                          </span>
                        ) : (
                          <Input
                            value={form.notes}
                            onChange={(e) => setForm(row.tank.id, { notes: e.target.value })}
                            placeholder="Optional"
                          />
                        )}
                      </TableCell>
                      <TableCell className="text-right">
                        {row.opening ? (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => rows.refetch()}
                            title="Refresh"
                          >
                            <RefreshCw className="size-4" />
                          </Button>
                        ) : (
                          <Button size="sm" onClick={() => submit(row.tank.id)} disabled={isSaving}>
                            <Save className="size-4" />
                            {isSaving ? 'Saving...' : 'Set'}
                          </Button>
                        )}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
