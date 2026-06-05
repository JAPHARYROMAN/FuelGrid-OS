'use client';

import { useMemo, useState } from 'react';
import Link from 'next/link';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ChevronDown, ChevronRight, Gauge, Plus, Trash2 } from 'lucide-react';

import { SdkError, type Nozzle, type Pump, type Tank } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  EmptyState,
  ErrorState,
  Input,
  Label,
  PageHeader,
  Skeleton,
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
import { usePermission } from '@/hooks/use-permissions';
import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';

interface PumpFormState {
  number: string;
  name: string;
  manufacturer: string;
  model: string;
  serial_number: string;
}

const blankPumpForm: PumpFormState = {
  number: '',
  name: '',
  manufacturer: '',
  model: '',
  serial_number: '',
};

interface NozzleFormState {
  pump_id: string;
  tank_id: string;
  number: string;
  default_price: string;
  meter_decimal_places: string;
  initial_meter_reading: string;
  initial_meter_note: string;
}

interface MeterFormState {
  nozzle_id: string;
  label: string;
  reading: string;
  note: string;
  meter_decimal_places: number;
  current?: string;
}

export default function PumpsPage() {
  const qc = useQueryClient();
  const [stationID, setStationID] = useState('');
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const [pumpOpen, setPumpOpen] = useState(false);
  const [pumpForm, setPumpForm] = useState<PumpFormState>(blankPumpForm);
  const [pumpError, setPumpError] = useState<string | null>(null);

  const [nozzleOpen, setNozzleOpen] = useState(false);
  const [nozzleForm, setNozzleForm] = useState<NozzleFormState | null>(null);
  const [nozzleError, setNozzleError] = useState<string | null>(null);

  const [meterOpen, setMeterOpen] = useState(false);
  const [meterForm, setMeterForm] = useState<MeterFormState | null>(null);
  const [meterError, setMeterError] = useState<string | null>(null);

  // Inline deletes have no dialog of their own — surface their failure in a
  // page-level banner so a failed delete isn't silent.
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  const products = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
  });

  const effectiveStation = stationID || stations.data?.items[0]?.id || '';

  // pumps.manage is station-scoped and also covers nozzle mutations (see
  // services/api server_routes.go). Gates the pump/nozzle create + delete
  // submits defensively; PermissionGate hides/disables the controls, and the
  // backend stays authoritative.
  const canManage = usePermission('pumps.manage', { stationID: effectiveStation });

  const tanks = useQuery({
    queryKey: ['tanks', effectiveStation],
    queryFn: ({ signal }) => api.listTanks({ stationID: effectiveStation }, signal),
    enabled: Boolean(effectiveStation),
  });
  const pumps = useQuery({
    queryKey: ['pumps', effectiveStation],
    queryFn: ({ signal }) => api.listPumps({ stationID: effectiveStation }, signal),
    enabled: Boolean(effectiveStation),
  });
  const nozzles = useQuery({
    queryKey: ['nozzles', effectiveStation],
    queryFn: ({ signal }) => api.listNozzles({ stationID: effectiveStation }, signal),
    enabled: Boolean(effectiveStation),
  });

  const productLookup = useMemo(
    () => new Map((products.data?.items ?? []).map((p) => [p.id, p])),
    [products.data],
  );
  const tankLookup = useMemo(
    () => new Map((tanks.data?.items ?? []).map((t) => [t.id, t])),
    [tanks.data],
  );
  const nozzlesByPump = useMemo(() => {
    const m = new Map<string, Nozzle[]>();
    for (const n of nozzles.data?.items ?? []) {
      const arr = m.get(n.pump_id) ?? [];
      arr.push(n);
      m.set(n.pump_id, arr);
    }
    return m;
  }, [nozzles.data]);

  function invalidateStation() {
    qc.invalidateQueries({ queryKey: ['pumps', effectiveStation] });
    qc.invalidateQueries({ queryKey: ['nozzles', effectiveStation] });
  }

  const createPump = useMutation({
    mutationFn: (input: PumpFormState) =>
      api.createPump({
        station_id: effectiveStation,
        number: Number(input.number),
        name: input.name || undefined,
        manufacturer: input.manufacturer || undefined,
        model: input.model || undefined,
        serial_number: input.serial_number || undefined,
      }),
    onSuccess: () => {
      invalidateStation();
      setPumpOpen(false);
      setPumpForm(blankPumpForm);
    },
    onError: (err) => setPumpError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  const deletePump = useMutation({
    mutationFn: (id: string) => api.deletePump(id),
    onSuccess: () => {
      setDeleteError(null);
      invalidateStation();
    },
    onError: (err) =>
      setDeleteError(err instanceof SdkError ? err.message : 'Could not delete pump'),
  });

  const createNozzle = useMutation({
    mutationFn: (input: NozzleFormState) =>
      api.createNozzle({
        pump_id: input.pump_id,
        tank_id: input.tank_id,
        number: Number(input.number),
        // default_price is a decimal STRING (the API accepts string or number).
        default_price: input.default_price.trim() ? input.default_price.trim() : undefined,
        meter_decimal_places: input.meter_decimal_places
          ? Number(input.meter_decimal_places)
          : undefined,
        initial_meter_reading: input.initial_meter_reading.trim()
          ? input.initial_meter_reading.trim()
          : undefined,
        initial_meter_note: input.initial_meter_note.trim() || undefined,
      }),
    onSuccess: () => {
      invalidateStation();
      setNozzleOpen(false);
      setNozzleForm(null);
    },
    onError: (err) => setNozzleError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  const deleteNozzle = useMutation({
    mutationFn: (id: string) => api.deleteNozzle(id),
    onSuccess: () => {
      setDeleteError(null);
      invalidateStation();
    },
    onError: (err) =>
      setDeleteError(err instanceof SdkError ? err.message : 'Could not delete nozzle'),
  });

  const setNozzleMeter = useMutation({
    mutationFn: (input: MeterFormState) =>
      api.setNozzleInitialMeter(input.nozzle_id, {
        reading: input.reading.trim(),
        note: input.note.trim() || undefined,
      }),
    onSuccess: () => {
      setMeterError(null);
      invalidateStation();
      setMeterOpen(false);
      setMeterForm(null);
    },
    onError: (err) =>
      setMeterError(err instanceof SdkError ? err.message : 'Could not save meter reading'),
  });

  function toggle(id: string) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function openPumpCreate() {
    const next = (pumps.data?.items.reduce((max, p) => Math.max(max, p.number), 0) ?? 0) + 1;
    setPumpForm({ ...blankPumpForm, number: String(next) });
    setPumpError(null);
    setPumpOpen(true);
  }

  function openNozzleCreate(pump: Pump) {
    const existing = nozzlesByPump.get(pump.id) ?? [];
    const next = existing.reduce((max, n) => Math.max(max, n.number), 0) + 1;
    setNozzleForm({
      pump_id: pump.id,
      tank_id: '',
      number: String(next),
      default_price: '',
      meter_decimal_places: '2',
      initial_meter_reading: '',
      initial_meter_note: '',
    });
    setNozzleError(null);
    setNozzleOpen(true);
  }

  function openMeterAdjust(nozzle: Nozzle) {
    const product = productLookup.get(nozzle.product_id);
    const tank = tankLookup.get(nozzle.tank_id);
    setMeterForm({
      nozzle_id: nozzle.id,
      label: `N${nozzle.number} · ${product?.name ?? 'Product'} · ${tank?.code ?? 'Tank'}`,
      reading: nozzle.initial_meter_reading ?? '',
      note: '',
      meter_decimal_places: nozzle.meter_decimal_places,
      current: nozzle.initial_meter_reading,
    });
    setMeterError(null);
    setMeterOpen(true);
  }

  // When the tank changes in the nozzle dialog, prefill the price from the
  // tank's product default and lock the product to it.
  function onNozzleTankChange(tankID: string) {
    if (!nozzleForm) return;
    const tank = tankLookup.get(tankID);
    const product = tank ? productLookup.get(tank.product_id) : undefined;
    setNozzleForm({
      ...nozzleForm,
      tank_id: tankID,
      default_price: product ? String(product.default_price) : nozzleForm.default_price,
    });
  }

  function submitPump() {
    if (canManage === false) {
      setPumpError("You don't have permission to manage pumps at this station");
      return;
    }
    if (!pumpForm.number || Number(pumpForm.number) <= 0) {
      setPumpError('A positive pump number is required');
      return;
    }
    createPump.mutate(pumpForm);
  }

  function submitNozzle() {
    if (!nozzleForm) return;
    if (canManage === false) {
      setNozzleError("You don't have permission to manage pumps at this station");
      return;
    }
    if (!nozzleForm.tank_id) {
      setNozzleError('Pick a tank — it sets the product');
      return;
    }
    if (!nozzleForm.number || Number(nozzleForm.number) <= 0) {
      setNozzleError('A positive nozzle number is required');
      return;
    }
    if (nozzleForm.initial_meter_reading.trim() && Number(nozzleForm.initial_meter_reading) < 0) {
      setNozzleError('Initial meter reading cannot be negative');
      return;
    }
    createNozzle.mutate(nozzleForm);
  }

  function submitMeter() {
    if (!meterForm) return;
    if (canManage === false) {
      setMeterError("You don't have permission to manage pumps at this station");
      return;
    }
    if (!meterForm.reading.trim()) {
      setMeterError('Enter the meter reading shown on the nozzle');
      return;
    }
    if (Number(meterForm.reading) < 0) {
      setMeterError('Meter reading cannot be negative');
      return;
    }
    setNozzleMeter.mutate(meterForm);
  }

  const noStations = (stations.data?.items?.length ?? 0) === 0;
  const noTanks = (tanks.data?.items?.length ?? 0) === 0;
  const lockedProduct = nozzleForm?.tank_id
    ? productLookup.get(tankLookup.get(nozzleForm.tank_id)?.product_id ?? '')
    : undefined;
  const nozzleMeterStep = nozzleForm
    ? 1 / Math.pow(10, Number(nozzleForm.meter_decimal_places || '2'))
    : 0.01;
  const adjustMeterStep = meterForm ? 1 / Math.pow(10, meterForm.meter_decimal_places) : 0.01;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Settings"
        title="Pumps"
        description="Dispensing units at each station and the nozzles that draw from its tanks."
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
            <PermissionGate permission="pumps.manage" stationId={effectiveStation}>
              <Button onClick={openPumpCreate} disabled={noStations || !effectiveStation}>
                <Plus className="size-4" />
                New pump
              </Button>
            </PermissionGate>
          </>
        }
      />

      {deleteError ? (
        <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
          {deleteError}
        </p>
      ) : null}

      {noStations ? (
        <EmptyState
          title="No stations yet"
          description="Create a station before installing pumps."
        />
      ) : pumps.isPending ? (
        <div className="flex flex-col gap-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-16 rounded-xl" />
          ))}
        </div>
      ) : pumps.isError ? (
        <ErrorState
          title="Couldn't load pumps"
          description={String((pumps.error as Error).message)}
          onRetry={() => pumps.refetch()}
        />
      ) : (pumps.data?.items?.length ?? 0) === 0 ? (
        <EmptyState
          title="No pumps at this station"
          description="Add a pump, then attach nozzles that draw from this station's tanks."
          action={
            <PermissionGate permission="pumps.manage" stationId={effectiveStation} mode="hide">
              <Button onClick={openPumpCreate}>Create one</Button>
            </PermissionGate>
          }
        />
      ) : (
        <div className="flex flex-col gap-3">
          {pumps.data!.items.map((pump) => {
            const isOpen = expanded.has(pump.id);
            const pumpNozzles = nozzlesByPump.get(pump.id) ?? [];
            return (
              <Card key={pump.id}>
                <CardHeader className="flex-row items-center justify-between gap-3 space-y-0">
                  <button
                    type="button"
                    className="flex items-center gap-2 text-left"
                    onClick={() => toggle(pump.id)}
                  >
                    {isOpen ? (
                      <ChevronDown className="size-4 text-muted-foreground" />
                    ) : (
                      <ChevronRight className="size-4 text-muted-foreground" />
                    )}
                    <span className="font-semibold">Pump {pump.number}</span>
                    {pump.name ? (
                      <span className="text-sm text-muted-foreground">{pump.name}</span>
                    ) : null}
                    <Badge tone={pump.status === 'active' ? 'success' : 'warning'}>
                      {pump.status}
                    </Badge>
                    <span className="text-xs text-muted-foreground">
                      {pumpNozzles.length} nozzle{pumpNozzles.length === 1 ? '' : 's'}
                    </span>
                  </button>
                  <div className="flex items-center gap-2">
                    <Button variant="ghost" size="sm" asChild>
                      <Link href={`/stations/${effectiveStation}/pumps/${pump.id}`}>Details</Link>
                    </Button>
                    <PermissionGate permission="pumps.manage" stationId={effectiveStation}>
                      <Button variant="ghost" size="sm" onClick={() => openNozzleCreate(pump)}>
                        <Plus className="size-4" />
                        Nozzle
                      </Button>
                    </PermissionGate>
                    <PermissionGate permission="pumps.manage" stationId={effectiveStation}>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => deletePump.mutate(pump.id)}
                        disabled={pumpNozzles.length > 0}
                        title={pumpNozzles.length > 0 ? 'Remove its nozzles first' : 'Delete pump'}
                      >
                        <Trash2 className="size-4" />
                      </Button>
                    </PermissionGate>
                  </div>
                </CardHeader>
                {isOpen ? (
                  <CardContent>
                    {pumpNozzles.length === 0 ? (
                      <p className="text-sm text-muted-foreground">
                        No nozzles yet. Use “Nozzle” to add one.
                      </p>
                    ) : (
                      <div className="flex flex-col divide-y divide-border">
                        {pumpNozzles
                          .slice()
                          .sort((a, b) => a.number - b.number)
                          .map((n) => {
                            const product = productLookup.get(n.product_id);
                            const tank = tankLookup.get(n.tank_id);
                            return (
                              <div
                                key={n.id}
                                className="flex flex-col gap-3 py-2 sm:flex-row sm:items-center sm:justify-between"
                              >
                                <div className="flex min-w-0 items-center gap-3">
                                  <span className="w-16 font-mono text-xs text-muted-foreground">
                                    N{n.number}
                                  </span>
                                  <div className="flex flex-col gap-0.5">
                                    <span className="inline-flex items-center gap-2">
                                      <span
                                        className="inline-block size-3 rounded-full border border-border"
                                        style={{ backgroundColor: product?.color ?? '#64748b' }}
                                        aria-hidden
                                      />
                                      {product?.name ?? '—'}
                                    </span>
                                    <span className="text-xs text-muted-foreground">
                                      {tank ? `${tank.name} (${tank.code})` : 'tank'} · Initial{' '}
                                      {n.initial_meter_reading ?? 'not set'}
                                    </span>
                                  </div>
                                </div>
                                <div className="flex flex-wrap items-center gap-2 sm:justify-end">
                                  <span className="font-mono text-sm tabular-nums">
                                    {formatMoney(n.default_price)}
                                  </span>
                                  <PermissionGate
                                    permission="pumps.manage"
                                    stationId={effectiveStation}
                                  >
                                    <Button
                                      variant="ghost"
                                      size="sm"
                                      onClick={() => openMeterAdjust(n)}
                                    >
                                      <Gauge className="size-4" />
                                      {n.initial_meter_reading ? 'Adjust meter' : 'Seed meter'}
                                    </Button>
                                  </PermissionGate>
                                  <PermissionGate
                                    permission="pumps.manage"
                                    stationId={effectiveStation}
                                  >
                                    <Button
                                      variant="ghost"
                                      size="sm"
                                      onClick={() => deleteNozzle.mutate(n.id)}
                                    >
                                      <Trash2 className="size-4" />
                                    </Button>
                                  </PermissionGate>
                                </div>
                              </div>
                            );
                          })}
                      </div>
                    )}
                  </CardContent>
                ) : null}
              </Card>
            );
          })}
        </div>
      )}

      {/* New pump dialog */}
      <Dialog open={pumpOpen} onOpenChange={setPumpOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New pump</DialogTitle>
            <DialogDescription>Add a dispensing unit to the selected station.</DialogDescription>
          </DialogHeader>
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              submitPump();
            }}
          >
            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="pump_number">Number</Label>
                <Input
                  id="pump_number"
                  type="number"
                  min="1"
                  value={pumpForm.number}
                  onChange={(e) => setPumpForm({ ...pumpForm, number: e.target.value })}
                  required
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="pump_name">Name (optional)</Label>
                <Input
                  id="pump_name"
                  value={pumpForm.name}
                  onChange={(e) => setPumpForm({ ...pumpForm, name: e.target.value })}
                  placeholder="Forecourt A"
                />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="pump_make">Manufacturer</Label>
                <Input
                  id="pump_make"
                  value={pumpForm.manufacturer}
                  onChange={(e) => setPumpForm({ ...pumpForm, manufacturer: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="pump_model">Model</Label>
                <Input
                  id="pump_model"
                  value={pumpForm.model}
                  onChange={(e) => setPumpForm({ ...pumpForm, model: e.target.value })}
                />
              </div>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="pump_serial">Serial number</Label>
              <Input
                id="pump_serial"
                value={pumpForm.serial_number}
                onChange={(e) => setPumpForm({ ...pumpForm, serial_number: e.target.value })}
              />
            </div>
            {pumpError ? (
              <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                {pumpError}
              </p>
            ) : null}
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setPumpOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={createPump.isPending}>
                {createPump.isPending ? 'Saving…' : 'Save'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* New nozzle dialog */}
      <Dialog open={nozzleOpen} onOpenChange={setNozzleOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New nozzle</DialogTitle>
            <DialogDescription>
              Pick the tank this nozzle draws from — the product is locked to it.
            </DialogDescription>
          </DialogHeader>
          {nozzleForm ? (
            <form
              className="flex flex-col gap-3"
              onSubmit={(e) => {
                e.preventDefault();
                submitNozzle();
              }}
            >
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="nozzle_tank">Tank</Label>
                <select
                  id="nozzle_tank"
                  className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                  value={nozzleForm.tank_id}
                  onChange={(e) => onNozzleTankChange(e.target.value)}
                  disabled={noTanks}
                >
                  <option value="">Select…</option>
                  {(tanks.data?.items ?? []).map((t: Tank) => {
                    const p = productLookup.get(t.product_id);
                    return (
                      <option key={t.id} value={t.id}>
                        {t.name} ({t.code}) — {p?.name ?? 'product'}
                      </option>
                    );
                  })}
                </select>
                {noTanks ? (
                  <p className="text-xs text-muted-foreground">
                    This station has no tanks yet — add one under Tanks first.
                  </p>
                ) : null}
              </div>

              <div className="flex flex-col gap-1.5">
                <Label>Product (from tank)</Label>
                <div className="flex h-10 items-center gap-2 rounded-md border border-border bg-muted px-3 text-sm">
                  {lockedProduct ? (
                    <>
                      <span
                        className="inline-block size-3 rounded-full border border-border"
                        style={{ backgroundColor: lockedProduct.color }}
                        aria-hidden
                      />
                      {lockedProduct.name}
                    </>
                  ) : (
                    <span className="text-muted-foreground">Pick a tank first</span>
                  )}
                </div>
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="nozzle_number">Number</Label>
                  <Input
                    id="nozzle_number"
                    type="number"
                    min="1"
                    value={nozzleForm.number}
                    onChange={(e) => setNozzleForm({ ...nozzleForm, number: e.target.value })}
                    required
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="nozzle_price">Price</Label>
                  <Input
                    id="nozzle_price"
                    type="number"
                    step="0.01"
                    min="0"
                    value={nozzleForm.default_price}
                    onChange={(e) =>
                      setNozzleForm({ ...nozzleForm, default_price: e.target.value })
                    }
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="nozzle_dp">Meter decimals</Label>
                  <Input
                    id="nozzle_dp"
                    type="number"
                    min="0"
                    max="4"
                    value={nozzleForm.meter_decimal_places}
                    onChange={(e) =>
                      setNozzleForm({ ...nozzleForm, meter_decimal_places: e.target.value })
                    }
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="nozzle_initial_meter">Initial meter</Label>
                  <Input
                    id="nozzle_initial_meter"
                    type="number"
                    inputMode="decimal"
                    step={nozzleMeterStep}
                    min="0"
                    value={nozzleForm.initial_meter_reading}
                    onChange={(e) =>
                      setNozzleForm({ ...nozzleForm, initial_meter_reading: e.target.value })
                    }
                    placeholder="Optional"
                  />
                </div>
              </div>

              <div className="flex flex-col gap-1.5">
                <Label htmlFor="nozzle_initial_note">Initial meter note</Label>
                <Input
                  id="nozzle_initial_note"
                  value={nozzleForm.initial_meter_note}
                  onChange={(e) =>
                    setNozzleForm({ ...nozzleForm, initial_meter_note: e.target.value })
                  }
                  placeholder="Optional"
                />
              </div>

              {nozzleError ? (
                <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                  {nozzleError}
                </p>
              ) : null}
              <DialogFooter>
                <Button type="button" variant="ghost" onClick={() => setNozzleOpen(false)}>
                  Cancel
                </Button>
                <Button type="submit" disabled={createNozzle.isPending || noTanks}>
                  {createNozzle.isPending ? 'Saving…' : 'Save'}
                </Button>
              </DialogFooter>
            </form>
          ) : null}
        </DialogContent>
      </Dialog>

      {/* Nozzle initial meter dialog */}
      <Dialog open={meterOpen} onOpenChange={setMeterOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Initial meter</DialogTitle>
            <DialogDescription>{meterForm?.label}</DialogDescription>
          </DialogHeader>
          {meterForm ? (
            <form
              className="flex flex-col gap-3"
              onSubmit={(e) => {
                e.preventDefault();
                submitMeter();
              }}
            >
              {meterForm.current ? (
                <div className="rounded-md border border-border bg-muted px-3 py-2 text-sm">
                  Current baseline{' '}
                  <span className="font-mono tabular-nums">{meterForm.current}</span>
                </div>
              ) : null}
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="initial_meter_reading">Meter reading</Label>
                <Input
                  id="initial_meter_reading"
                  type="number"
                  inputMode="decimal"
                  step={adjustMeterStep}
                  min="0"
                  value={meterForm.reading}
                  onChange={(e) => setMeterForm({ ...meterForm, reading: e.target.value })}
                  required
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="initial_meter_note">Note</Label>
                <Input
                  id="initial_meter_note"
                  value={meterForm.note}
                  onChange={(e) => setMeterForm({ ...meterForm, note: e.target.value })}
                  placeholder="Service, replacement, or correction note"
                />
              </div>
              {meterError ? (
                <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                  {meterError}
                </p>
              ) : null}
              <DialogFooter>
                <Button type="button" variant="ghost" onClick={() => setMeterOpen(false)}>
                  Cancel
                </Button>
                <Button type="submit" disabled={setNozzleMeter.isPending}>
                  {setNozzleMeter.isPending ? 'Saving…' : 'Save'}
                </Button>
              </DialogFooter>
            </form>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}
