import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { ReportCatalog } from '@fuelgrid/sdk';

// The hub draws on the catalog, the station/region selectors and the exports
// rail. Mock each SDK method the page (and its child components) call.
const getReportCatalog = vi.fn();
const listStations = vi.fn();
const listRegions = vi.fn();
const listExportJobs = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    getReportCatalog: (...a: unknown[]) => getReportCatalog(...a),
    listStations: (...a: unknown[]) => listStations(...a),
    listRegions: (...a: unknown[]) => listRegions(...a),
    listExportJobs: (...a: unknown[]) => listExportJobs(...a),
  },
}));

// usePermission is a UX hint; grant export access so the Exports rail queries.
vi.mock('@/hooks/use-permissions', () => ({ usePermission: () => true }));

import ReportsPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ReportsPage />
    </QueryClientProvider>,
  );
}

// A catalog covering every availability state: a LIVE category with a real
// figure + alert pill, a PARTIAL one, a PLACEHOLDER one, and a LIVE one whose
// sensitive metric is gated away (value null + honest reason). The catalog is
// already permission-filtered server-side, so any category the actor cannot see
// is simply absent — modelled here by omitting it.
const catalog: ReportCatalog = {
  generated_at: '2026-06-13T00:00:00Z',
  categories: [
    {
      key: 'risk-loss',
      name: 'Risk and Loss',
      description: 'Fuel loss, variance patterns and open risk alerts.',
      icon: 'shield-alert',
      sort_order: 120,
      required_permission: 'risk.read',
      availability: 'live',
      target_route: '/reports/fuel-loss',
      metric: { label: 'Open risk alerts', value: '2', unit: 'count' },
      alert_count: 2,
      reports: [],
    },
    {
      key: 'finance',
      name: 'Finance',
      description: 'Profit & loss, expenses and the financial statement.',
      icon: 'banknote',
      sort_order: 90,
      required_permission: 'finance.read',
      availability: 'live',
      target_route: '/reports/profitability',
      metric: {
        label: 'Outstanding payables',
        value: null,
        unit: 'TZS',
        reason: 'Requires margin.view to see supplier cost / payables exposure.',
      },
      alert_count: 0,
      reports: [],
    },
    {
      key: 'pump',
      name: 'Pump',
      description: 'Pump and nozzle throughput and utilisation.',
      icon: 'gauge',
      sort_order: 50,
      required_permission: 'revenue.read',
      availability: 'partial',
      target_route: '/reports/pump',
      metric: {
        label: 'Pump throughput',
        value: null,
        reason: 'Per-station report — pump throughput is computed per station.',
      },
      alert_count: 0,
      reports: [],
    },
    {
      key: 'tank',
      name: 'Tank',
      description: 'Live tank telemetry: water, temperature and level trends.',
      icon: 'database',
      sort_order: 40,
      required_permission: 'inventory.read',
      availability: 'placeholder',
      target_route: '/reports/tank',
      metric: {
        label: 'Live tank telemetry',
        value: null,
        reason: 'No ATG / sensor feed connected — live tank telemetry is not available.',
      },
      alert_count: 0,
      reports: [],
    },
  ],
  data_quality: [
    {
      category_key: 'risk-loss',
      level: 'warning',
      message: 'Risk and Loss: 2 open risk alert(s) need review.',
    },
    { category_key: 'tank', level: 'info', message: 'Tank: live tank telemetry is not available.' },
  ],
};

const stations = {
  items: [
    { id: 'st-1', code: 'MIK-01', name: 'Mikocheni', region_id: 'rg-1' },
    { id: 'st-2', code: 'MSA-01', name: 'Mombasa', region_id: 'rg-2' },
  ],
  count: 2,
};
const regions = {
  items: [
    { id: 'rg-1', code: 'DAR', name: 'Dar', status: 'active' },
    { id: 'rg-2', code: 'CST', name: 'Coast', status: 'active' },
  ],
  count: 2,
};

beforeEach(() => {
  vi.clearAllMocks();
  getReportCatalog.mockResolvedValue(catalog);
  listStations.mockResolvedValue(stations);
  listRegions.mockResolvedValue(regions);
  listExportJobs.mockResolvedValue({ items: [], count: 0 });
});

describe('Reports home', () => {
  it('renders a live card with its key metric and alert pill', async () => {
    renderPage();
    expect(await screen.findByText('Risk and Loss')).toBeInTheDocument();
    // The card's key-metric label is present (the count "2" also appears in the
    // hero band, so assert the label rather than the ambiguous bare value).
    expect(screen.getByText('Open risk alerts')).toBeInTheDocument();
    // Alert pill is present and pluralised.
    expect(screen.getByText('2 alerts')).toBeInTheDocument();
    // The card links to its report (with hub context appended).
    const link = screen.getAllByRole('link', { name: /risk and loss/i })[0]!;
    expect(link.getAttribute('href')).toContain('/reports/fuel-loss');
  });

  it('marks a placeholder category coming-soon and never links it', async () => {
    renderPage();
    expect(await screen.findByText('Tank')).toBeInTheDocument();
    // Coming-soon copy is shown; no link to /reports/tank exists.
    expect(screen.getAllByText('Coming soon').length).toBeGreaterThan(0);
    expect(screen.queryByRole('link', { name: /tank/i })).toBeNull();
  });

  it('shows the honest gated reason instead of a fabricated number', async () => {
    renderPage();
    expect(await screen.findByText('Finance')).toBeInTheDocument();
    expect(screen.getByText(/Requires margin\.view/)).toBeInTheDocument();
  });

  it('marks a partial category as limited but still openable', async () => {
    renderPage();
    expect(await screen.findByText('Pump')).toBeInTheDocument();
    expect(screen.getByText('Limited')).toBeInTheDocument();
  });

  it('filters the cards via the search box', async () => {
    renderPage();
    await screen.findByText('Risk and Loss');
    const search = screen.getByLabelText('Search reports');
    fireEvent.change(search, { target: { value: 'pump' } });
    expect(screen.getByText('Pump')).toBeInTheDocument();
    expect(screen.queryByText('Risk and Loss')).toBeNull();
    expect(screen.queryByText('Finance')).toBeNull();
  });

  it('hides permission-filtered categories the server omits', async () => {
    renderPage();
    await screen.findByText('Risk and Loss');
    // The fixture catalog (already server-filtered) has no Executive card.
    expect(screen.queryByText('Executive')).toBeNull();
  });

  it('emits a custom date range from the date-range picker', async () => {
    renderPage();
    await screen.findByText('Risk and Loss');
    const select = screen.getByLabelText('Date range');
    fireEvent.change(select, { target: { value: 'custom' } });
    // Custom from/to inputs appear once Custom is selected.
    const from = screen.getByLabelText('From date');
    const to = screen.getByLabelText('To date');
    fireEvent.change(from, { target: { value: '2026-01-01' } });
    fireEvent.change(to, { target: { value: '2026-01-31' } });
    expect((from as HTMLInputElement).value).toBe('2026-01-01');
    expect((to as HTMLInputElement).value).toBe('2026-01-31');
  });

  it('renders the hub data-quality warnings band', async () => {
    renderPage();
    expect(await screen.findByText(/2 open risk alert\(s\) need review/)).toBeInTheDocument();
  });

  it('renders rails with honest empty states (no fake rows)', async () => {
    renderPage();
    await screen.findByText('Risk and Loss');
    expect(screen.getByText('Recent reports')).toBeInTheDocument();
    expect(screen.getByText('Locked')).toBeInTheDocument();
    // Exports rail loaded an empty export_jobs list → honest empty copy.
    expect(await screen.findByText(/No exports yet/)).toBeInTheDocument();
  });

  it('carries the selected station into a report link as default context', async () => {
    renderPage();
    await screen.findByText('Risk and Loss');
    const link = screen.getAllByRole('link', { name: /risk and loss/i })[0]!;
    // Default station (first accessible) is appended as ?station_id=…
    expect(link.getAttribute('href')).toContain('station_id=st-1');
  });

  it('shows a station/region grouped selector over accessible stations', async () => {
    renderPage();
    await screen.findByText('Risk and Loss');
    // Two regions in scope → a Region selector appears alongside Station.
    expect(await screen.findByLabelText('Region')).toBeInTheDocument();
    const station = screen.getByLabelText('Station') as HTMLSelectElement;
    expect(within(station).getByText('MIK-01 — Mikocheni')).toBeInTheDocument();
  });
});
