'use client';

import * as React from 'react';
import { useTheme } from 'next-themes';
import {
  AlertTriangle,
  Database,
  DollarSign,
  Download,
  FileText,
  Fuel,
  Search,
  ShieldAlert,
} from 'lucide-react';

import {
  AreaChart,
  Badge,
  BarChart,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
  CategoricalBarChart,
  DataQualityBanner,
  DataQualityCard,
  DataTable,
  EmptyState,
  ErrorState,
  ExportButtonGroup,
  FilterBar,
  FilterField,
  Input,
  InsightCard,
  Label,
  LineChart,
  LoadingState,
  MetricCard,
  PageHeader,
  PumpCard,
  ReconciliationWaterfall,
  ReportCategoryCard,
  RiskAlertCard,
  RiskBadge,
  Separator,
  ShiftTimeline,
  Skeleton,
  Sparkline,
  Stat,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  TankVisual,
  type DataTableColumn,
} from '@fuelgrid/ui';

/* ------------------------------------------------------------------ *
 * Living style guide
 *
 * A comprehensive design-system reference rendering every @fuelgrid/ui
 * component grouped by category, each with its key states shown side by
 * side. This page intentionally uses representative STATIC example props
 * — it is a design reference, not a product screen, so it has no data
 * fetching. Verify it renders in light, dark and navy.
 * ------------------------------------------------------------------ */

/** Section wrapper: a titled band with a hairline rule. */
function Section({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <section className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-semibold tracking-tight text-foreground">{title}</h2>
        {description ? <p className="text-sm text-muted-foreground">{description}</p> : null}
      </div>
      <Separator />
      {children}
    </section>
  );
}

/** A labelled example tile so each state reads as its own specimen. */
function Specimen({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-2">
      <span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground/70">
        {label}
      </span>
      <div className="flex min-h-[2.5rem] flex-wrap items-start gap-3">{children}</div>
    </div>
  );
}

const TOKEN_SWATCHES: { name: string; className: string; fg?: string }[] = [
  { name: 'background', className: 'bg-background', fg: 'text-foreground' },
  { name: 'surface', className: 'bg-surface', fg: 'text-foreground' },
  { name: 'card', className: 'bg-card', fg: 'text-card-foreground' },
  { name: 'popover', className: 'bg-popover', fg: 'text-popover-foreground' },
  { name: 'muted', className: 'bg-muted', fg: 'text-muted-foreground' },
  { name: 'accent', className: 'bg-accent', fg: 'text-accent-foreground' },
  { name: 'accent-muted', className: 'bg-accent-muted', fg: 'text-accent' },
  { name: 'secondary', className: 'bg-secondary', fg: 'text-secondary-foreground' },
  { name: 'success', className: 'bg-success', fg: 'text-success-foreground' },
  { name: 'warning', className: 'bg-warning', fg: 'text-warning-foreground' },
  { name: 'danger', className: 'bg-danger', fg: 'text-danger-foreground' },
  { name: 'info', className: 'bg-info', fg: 'text-info-foreground' },
];

const TYPE_SCALE: { label: string; className: string }[] = [
  { label: 'Display / 3xl', className: 'text-3xl font-semibold tracking-tight' },
  { label: 'Heading / 2xl', className: 'text-2xl font-semibold tracking-tight' },
  { label: 'Title / lg', className: 'text-lg font-semibold' },
  { label: 'Body / base', className: 'text-base' },
  { label: 'Small / sm', className: 'text-sm text-muted-foreground' },
  { label: 'Caption / xs', className: 'text-xs text-muted-foreground' },
  { label: 'Mono / tabular', className: 'font-mono tabular-nums text-base' },
];

const THEMES = [
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
  { value: 'navy', label: 'Navy' },
] as const;

const LINE_DATA = [
  { day: 'Mon', litres: 4200, revenue: 5800 },
  { day: 'Tue', litres: 3900, revenue: 5400 },
  { day: 'Wed', litres: 4600, revenue: 6300 },
  { day: 'Thu', litres: 5100, revenue: 7000 },
  { day: 'Fri', litres: 6200, revenue: 8500 },
  { day: 'Sat', litres: 7000, revenue: 9600 },
  { day: 'Sun', litres: 5500, revenue: 7600 },
];

const CATEGORY_DATA = [
  { product: 'Petrol', volume: 18200 },
  { product: 'Diesel', volume: 24100 },
  { product: 'Premium', volume: 6400 },
];

interface DemoRow {
  id: string;
  station: string;
  product: string;
  litres: string;
  variance: string;
}

const TABLE_ROWS: DemoRow[] = [
  { id: '1', station: 'ACC-01', product: 'Diesel', litres: '12,400.000', variance: '-12.000' },
  { id: '2', station: 'ACC-02', product: 'Petrol', litres: '9,820.500', variance: '+4.250' },
  { id: '3', station: 'KMS-01', product: 'Premium', litres: '3,110.000', variance: '0.000' },
];

const TABLE_COLUMNS: DataTableColumn<DemoRow>[] = [
  { id: 'station', header: 'Station', cell: (r) => r.station, sortValue: (r) => r.station },
  { id: 'product', header: 'Product', cell: (r) => r.product, sortValue: (r) => r.product },
  {
    id: 'litres',
    header: 'Litres',
    align: 'right',
    cell: (r) => <span className="font-mono tabular-nums">{r.litres}</span>,
    sortValue: (r) => Number(r.litres.replace(/,/g, '')),
  },
  {
    id: 'variance',
    header: 'Variance',
    align: 'right',
    cell: (r) => <span className="font-mono tabular-nums">{r.variance}</span>,
    sortValue: (r) => Number(r.variance.replace(/[,+]/g, '')),
  },
];

export default function StyleGuidePage() {
  const { theme, setTheme, resolvedTheme } = useTheme();
  const active = theme ?? resolvedTheme ?? 'dark';

  return (
    <div className="flex flex-col gap-10">
      <PageHeader
        eyebrow="Design system"
        title="Living style guide"
        description="Every @fuelgrid/ui component, grouped by category, with its key states shown side by side. Switch themes to verify light, dark and navy."
        actions={
          <div className="flex items-center gap-1.5">
            {THEMES.map((t) => (
              <Button
                key={t.value}
                variant={active === t.value ? 'primary' : 'outline'}
                size="sm"
                onClick={() => setTheme(t.value)}
              >
                {t.label}
              </Button>
            ))}
          </div>
        }
      />

      {/* ---------------- Foundations ---------------- */}
      <Section
        title="Foundations"
        description="Color tokens, typography scale and the three themes."
      >
        <div className="flex flex-col gap-6">
          <Specimen label="Color tokens">
            <div className="grid w-full grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
              {TOKEN_SWATCHES.map((s) => (
                <div
                  key={s.name}
                  className={`flex h-20 flex-col justify-end rounded-lg border border-border/60 p-3 ${s.className} ${s.fg ?? ''}`}
                >
                  <span className="text-xs font-medium">{s.name}</span>
                </div>
              ))}
            </div>
          </Specimen>

          <Specimen label="Typography scale">
            <div className="flex w-full flex-col gap-2">
              {TYPE_SCALE.map((t) => (
                <div key={t.label} className="flex items-baseline gap-4">
                  <span className="w-32 shrink-0 text-[11px] uppercase tracking-wider text-muted-foreground/70">
                    {t.label}
                  </span>
                  <span className={t.className}>The quick brown fox 1,234.56</span>
                </div>
              ))}
            </div>
          </Specimen>

          <Specimen label="Themes">
            <div className="flex flex-wrap gap-2">
              {THEMES.map((t) => (
                <Button
                  key={t.value}
                  variant={active === t.value ? 'primary' : 'secondary'}
                  size="sm"
                  onClick={() => setTheme(t.value)}
                >
                  {t.label}
                </Button>
              ))}
            </div>
          </Specimen>
        </div>
      </Section>

      {/* ---------------- Buttons & Inputs ---------------- */}
      <Section
        title="Buttons & inputs"
        description="Actions and form controls across their states."
      >
        <div className="flex flex-col gap-6">
          <Specimen label="Button variants">
            <Button variant="primary">Primary</Button>
            <Button variant="secondary">Secondary</Button>
            <Button variant="outline">Outline</Button>
            <Button variant="ghost">Ghost</Button>
            <Button variant="danger">Danger</Button>
          </Specimen>
          <Specimen label="Button sizes">
            <Button size="sm">Small</Button>
            <Button size="md">Medium</Button>
            <Button size="lg">Large</Button>
            <Button size="icon" aria-label="Search">
              <Search />
            </Button>
          </Specimen>
          <Specimen label="Disabled">
            <Button disabled>Primary</Button>
            <Button variant="secondary" disabled>
              Secondary
            </Button>
            <Button variant="danger" disabled>
              Danger
            </Button>
          </Specimen>
          <Specimen label="Badges">
            <Badge tone="neutral">Neutral</Badge>
            <Badge tone="success">Success</Badge>
            <Badge tone="warning">Warning</Badge>
            <Badge tone="danger">Danger</Badge>
            <Badge tone="info">Info</Badge>
            <Badge tone="accent">Accent</Badge>
          </Specimen>
          <Specimen label="Input — default / focus hint / disabled">
            <div className="flex w-full max-w-md flex-col gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="sg-input">Station name</Label>
                <Input id="sg-input" placeholder="e.g. Accra Ring Road" />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="sg-input-disabled">Disabled</Label>
                <Input id="sg-input-disabled" placeholder="Read only" disabled />
              </div>
            </div>
          </Specimen>
          <Specimen label="FilterBar">
            <FilterBar
              actions={
                <Button variant="ghost" size="sm">
                  Reset
                </Button>
              }
            >
              <FilterField label="Station">
                <Input placeholder="All stations" className="h-8 w-40" />
              </FilterField>
              <FilterField label="Product">
                <Input placeholder="All products" className="h-8 w-40" />
              </FilterField>
            </FilterBar>
          </Specimen>
          <Specimen label="ExportButtonGroup — permitted / denied">
            <ExportButtonGroup
              actions={[
                { format: 'csv', onDownload: () => {} },
                { format: 'pdf', onDownload: () => {} },
              ]}
            />
            <ExportButtonGroup
              permitted={false}
              actions={[{ format: 'csv', onDownload: () => {} }]}
            />
          </Specimen>
        </div>
      </Section>

      {/* ---------------- Cards ---------------- */}
      <Section title="Cards" description="Metric tiles, hub cards and insight surfaces.">
        <div className="flex flex-col gap-6">
          <Specimen label="Stat / MetricCard — default & loading">
            <div className="grid w-full grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
              <Stat label="Open shifts" value="14" delta="+3" hint="vs yesterday" />
              <MetricCard
                label="Gross revenue"
                value="2,950.00"
                unit="USD"
                trend="up"
                trendValue="+4.2%"
                hint="vs last week"
                icon={<DollarSign />}
              >
                <Sparkline data={LINE_DATA} valueKey="revenue" />
              </MetricCard>
              <MetricCard
                label="Litres sold"
                value="42,180"
                unit="L"
                trend="down"
                trendValue="-1.8%"
                icon={<Fuel />}
              />
              <MetricCard label="Pending close" value="0" loading icon={<Database />} />
            </div>
          </Specimen>

          <Specimen label="ReportCategoryCard — default / loading / alert">
            <div className="grid w-full grid-cols-1 gap-4 lg:grid-cols-3">
              <ReportCategoryCard
                icon={<FileText />}
                title="Sales summary"
                description="Daily gross by product"
                metricLabel="Latest gross"
                metricValue="12,940.00"
                actions={
                  <Button size="sm" variant="outline">
                    <Download /> CSV
                  </Button>
                }
              />
              <ReportCategoryCard
                icon={<Database />}
                title="Stock reconciliation"
                description="Closing dips vs expected"
                metricLabel="Latest"
                loading
              />
              <ReportCategoryCard
                icon={<ShieldAlert />}
                title="Risk register"
                description="Open alerts requiring action"
                metricLabel="Open"
                metricValue="3"
                alertCount={3}
              />
            </div>
          </Specimen>

          <Specimen label="InsightCard — info / warning / critical">
            <div className="flex w-full flex-col gap-3">
              <InsightCard
                severity="info"
                message="Diesel sell-through is 8% above the trailing 4-week mean."
                recommendedAction="Confirm the next delivery covers the higher run-rate."
              />
              <InsightCard
                severity="warning"
                message="Two pumps reported no transactions during the evening peak."
                recommendedAction="Check pump 4 + 6 connectivity."
              />
              <InsightCard
                severity="critical"
                message="Closing stock variance for tank 3 exceeds tolerance."
                recommendedAction="Re-dip tank 3 and recount deliveries."
              />
            </div>
          </Specimen>

          <Specimen label="DataQuality — banner & card">
            <div className="flex w-full flex-col gap-4 lg:flex-row">
              <DataQualityBanner
                level="warning"
                messages={['3 missing dip readings', 'FX rate is provisional']}
                className="flex-1"
              />
              <DataQualityCard
                level="info"
                title="Coverage"
                messages={['All shifts closed', '1 station awaiting reconciliation']}
                className="lg:w-80"
              />
            </div>
          </Specimen>

          <Specimen label="RiskAlertCard — critical / high / low / navigable">
            <div className="grid w-full grid-cols-1 gap-4 lg:grid-cols-2">
              <RiskAlertCard
                severity="critical"
                title="Stock variance over tolerance"
                description="Tank 3 closing dip is 240L below expected after the evening shift."
                metricLabel="Variance"
                metricValue="-240 L"
                recommendedAction="Re-dip tank 3 and recount deliveries."
                station="ACC-01"
                occurredAt="Today 08:42"
              />
              <RiskAlertCard
                severity="high"
                title="Pump downtime during peak"
                description="Pump 4 reported zero transactions for 42 minutes."
                metricLabel="Lost"
                metricValue="~310 L"
                station="ACC-02"
                occurredAt="Yesterday 18:10"
              />
              <RiskAlertCard
                severity="low"
                title="Shift closed late"
                description="Morning shift closed 35 minutes after schedule."
                station="KMS-01"
                occurredAt="Today 14:35"
              />
              <RiskAlertCard
                severity="medium"
                title="Price change pending approval"
                description="A pending diesel price change has been queued for review."
                recommendedAction="Approve or reject in Pricing."
                onClick={() => {}}
              />
            </div>
          </Specimen>

          <Specimen label="Card primitives">
            <Card className="w-full max-w-sm">
              <CardHeader>
                <CardTitle>Card title</CardTitle>
                <CardDescription>A description line for the card.</CardDescription>
              </CardHeader>
              <CardContent>
                <p className="text-sm text-muted-foreground">
                  Card content area. Use Card + sub-components to compose bespoke surfaces.
                </p>
              </CardContent>
              <CardFooter>
                <Button size="sm">Action</Button>
              </CardFooter>
            </Card>
          </Specimen>
        </div>
      </Section>

      {/* ---------------- Data ---------------- */}
      <Section
        title="Data"
        description="Tables and the sortable DataTable, including the empty state."
      >
        <div className="flex flex-col gap-6">
          <Specimen label="Table primitives">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Station</TableHead>
                  <TableHead>Product</TableHead>
                  <TableHead className="text-right">Litres</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {TABLE_ROWS.map((r) => (
                  <TableRow key={r.id}>
                    <TableCell>{r.station}</TableCell>
                    <TableCell>{r.product}</TableCell>
                    <TableCell className="text-right font-mono tabular-nums">{r.litres}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Specimen>

          <Specimen label="DataTable — sortable">
            <div className="w-full">
              <DataTable
                columns={TABLE_COLUMNS}
                rows={TABLE_ROWS}
                rowKey={(r) => r.id}
                defaultSort={{ columnId: 'litres', direction: 'desc' }}
              />
            </div>
          </Specimen>

          <Specimen label="DataTable — empty">
            <div className="w-full">
              <DataTable
                columns={TABLE_COLUMNS}
                rows={[]}
                rowKey={(r) => r.id}
                emptyContent={<span className="text-muted-foreground">No rows to display.</span>}
              />
            </div>
          </Specimen>
        </div>
      </Section>

      {/* ---------------- Domain visuals ---------------- */}
      <Section title="Domain visuals" description="Operational surfaces specific to fuel retail.">
        <div className="flex flex-col gap-6">
          <Specimen label="TankVisual — filled / awaiting reading">
            <div className="flex flex-wrap gap-6">
              <TankVisual
                name="Diesel A"
                code="T-01"
                color="#f59e0b"
                capacityLitres="30000.000"
                safeMinLitres="4000.000"
                safeMaxLitres="27000.000"
                currentLitres={18400}
                status="active"
              />
              <TankVisual
                name="Petrol B"
                code="T-02"
                color="#22c55e"
                capacityLitres="20000.000"
                safeMinLitres="3000.000"
                safeMaxLitres="18000.000"
                currentLitres={null}
                status="active"
              />
            </div>
          </Specimen>

          <Specimen label="PumpCard — static / interactive">
            <div className="flex flex-wrap gap-4">
              <PumpCard
                number={1}
                status="active"
                nozzles={[
                  {
                    id: 'n1',
                    number: 1,
                    productName: 'Diesel',
                    productColor: '#f59e0b',
                    tankCode: 'T-01',
                    price: '13.45',
                  },
                  {
                    id: 'n2',
                    number: 2,
                    productName: 'Petrol',
                    productColor: '#22c55e',
                    tankCode: 'T-02',
                    price: '14.20',
                  },
                ]}
              />
              <PumpCard
                number={2}
                status="offline"
                onActivate={() => {}}
                nozzles={[
                  {
                    id: 'n3',
                    number: 1,
                    productName: 'Premium',
                    productColor: '#6366f1',
                    tankCode: 'T-03',
                    price: '15.90',
                  },
                ]}
              />
            </div>
          </Specimen>

          <Specimen label="ReconciliationWaterfall — within / over tolerance">
            <div className="grid w-full grid-cols-1 gap-4 lg:grid-cols-2">
              <ReconciliationWaterfall
                openingStock="10000.000"
                deliveries="5000.000"
                sales="4000.000"
                adjustments="0.000"
                expectedClosing="11000.000"
                actualClosing="11010.000"
                variance="10.000"
                tolerance="50.000"
                unit="L"
              />
              <ReconciliationWaterfall
                openingStock="10000.000"
                deliveries="5000.000"
                sales="4000.000"
                adjustments="0.000"
                expectedClosing="11000.000"
                actualClosing="10800.000"
                variance="-200.000"
                tolerance="50.000"
                unit="L"
              />
            </div>
          </Specimen>

          <Specimen label="ShiftTimeline — populated / empty">
            <div className="grid w-full grid-cols-1 gap-4 lg:grid-cols-2">
              <ShiftTimeline
                milestones={[
                  { label: 'Shift opened', timestamp: '08:00', status: 'done' },
                  { label: 'Mid-shift dip', timestamp: '13:00', status: 'done' },
                  { label: 'Shift close', timestamp: '20:00', status: 'current' },
                  { label: 'Reconciled', status: 'pending' },
                ]}
              />
              <ShiftTimeline milestones={[]} />
            </div>
          </Specimen>

          <Specimen label="RiskBadge — five-level scale">
            <RiskBadge severity="critical" />
            <RiskBadge severity="high" />
            <RiskBadge severity="medium" />
            <RiskBadge severity="low" />
            <RiskBadge severity="info" />
          </Specimen>
        </div>
      </Section>

      {/* ---------------- States ---------------- */}
      <Section
        title="States"
        description="The mandatory empty / loading / error / skeleton surfaces every screen must support."
      >
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <EmptyState
            title="No alerts"
            description="Nothing needs your attention right now."
            action={<Button size="sm">Refresh</Button>}
          />
          <LoadingState title="Loading reports…" description="Fetching the latest figures." />
          <ErrorState
            title="Couldn't load data"
            description="The request failed. Try again in a moment."
            icon={<AlertTriangle className="size-8" />}
            onRetry={() => {}}
          />
          <Card>
            <CardHeader>
              <CardTitle>Skeleton</CardTitle>
              <CardDescription>Use while a known-shape surface loads.</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <Skeleton className="h-6 w-3/4 rounded-md" />
              <Skeleton className="h-6 w-1/2 rounded-md" />
              <Skeleton className="h-24 w-full rounded-lg" />
            </CardContent>
          </Card>
        </div>
      </Section>

      {/* ---------------- Charts ---------------- */}
      <Section title="Charts" description="Trend, area, categorical and inline sparkline charts.">
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle>LineChart</CardTitle>
            </CardHeader>
            <CardContent>
              <LineChart
                data={LINE_DATA}
                xKey="day"
                series={[
                  { key: 'revenue', label: 'Revenue' },
                  { key: 'litres', label: 'Litres' },
                ]}
              />
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>AreaChart</CardTitle>
            </CardHeader>
            <CardContent>
              <AreaChart
                data={LINE_DATA}
                xKey="day"
                series={[{ key: 'litres', label: 'Litres' }]}
              />
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>BarChart</CardTitle>
            </CardHeader>
            <CardContent>
              <BarChart
                data={LINE_DATA}
                xKey="day"
                series={[{ key: 'revenue', label: 'Revenue' }]}
              />
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>CategoricalBarChart</CardTitle>
            </CardHeader>
            <CardContent>
              <CategoricalBarChart
                data={CATEGORY_DATA}
                xKey="product"
                valueKey="volume"
                label="Volume"
                layout="vertical"
              />
            </CardContent>
          </Card>
        </div>
      </Section>
    </div>
  );
}
