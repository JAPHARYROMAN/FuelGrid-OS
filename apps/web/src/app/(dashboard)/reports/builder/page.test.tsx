import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

const getBuilderDatasets = vi.fn();
const previewBuilderReport = vi.fn();
const createReportTemplate = vi.fn();
const listReportTemplates = vi.fn();
const updateReportTemplate = vi.fn();
const deleteReportTemplate = vi.fn();
const runReportTemplate = vi.fn();
const me = vi.fn();

vi.mock('@/lib/api', () => ({
  api: {
    getBuilderDatasets: (...a: unknown[]) => getBuilderDatasets(...a),
    previewBuilderReport: (...a: unknown[]) => previewBuilderReport(...a),
    createReportTemplate: (...a: unknown[]) => createReportTemplate(...a),
    listReportTemplates: (...a: unknown[]) => listReportTemplates(...a),
    updateReportTemplate: (...a: unknown[]) => updateReportTemplate(...a),
    deleteReportTemplate: (...a: unknown[]) => deleteReportTemplate(...a),
    runReportTemplate: (...a: unknown[]) => runReportTemplate(...a),
    me: (...a: unknown[]) => me(...a),
    exportReport: vi.fn(),
  },
}));

// usePermission('reports.builder') gates the page; usePermissions() backs the
// gallery's admin flag. Both come from the same mocked hook module.
let builderPermitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => builderPermitted,
  usePermissions: () => ({ data: { is_system_admin: false } }),
}));

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}));

import ReportBuilderPage from './page';

// Two datasets the actor MAY use. `revenue_days` carries a SENSITIVE measure that
// the backend would normally strip for a non-holder — here it is already absent
// from the response (the contract: GET /datasets strips it), so it never appears
// as an option. A third dataset the actor CANNOT use is simply not in the list.
const DATASETS = {
  generated_at: '2026-06-15T00:00:00Z',
  aggregates: ['sum', 'avg', 'count', 'min', 'max'],
  datasets: [
    {
      key: 'revenue_days',
      name: 'Daily Revenue',
      description: 'Per-station, per-day revenue rollup.',
      required_permission: 'revenue.read',
      sensitive_permission: 'margin.view',
      dimensions: [
        { id: 'business_date', label: 'Business date', type: 'date' },
        { id: 'station_id', label: 'Station', type: 'uuid' },
      ],
      measures: [
        {
          id: 'gross_revenue',
          label: 'Gross revenue',
          allowed_aggs: ['sum', 'avg', 'min', 'max'],
          decimal: true,
          unit: 'TZS',
          sensitive: false,
        },
        {
          id: 'day_count',
          label: 'Days',
          allowed_aggs: ['count', 'sum'],
          decimal: false,
          unit: 'count',
          sensitive: false,
        },
      ],
      filters: [
        {
          id: 'status',
          label: 'Lock status',
          type: 'text',
          operators: ['eq', 'ne'],
        },
      ],
    },
    {
      key: 'shifts',
      name: 'Shifts',
      description: 'Shift lifecycle by station and status.',
      required_permission: 'station.read',
      dimensions: [{ id: 'status', label: 'Status', type: 'text' }],
      measures: [
        {
          id: 'shift_count',
          label: 'Shifts',
          allowed_aggs: ['count', 'sum'],
          decimal: false,
          unit: 'count',
          sensitive: false,
        },
      ],
      filters: [],
    },
  ],
};

// The preview/run envelope the builder renders through the shared shell.
const PREVIEW_ENVELOPE = {
  metadata: { report_key: 'custom:revenue_days', title: 'Daily Revenue' },
  filters_used: { dataset: 'revenue_days', viz: 'bar' },
  data_quality: [],
  summary: [
    { label: 'Rows', value: '2', unit: 'count' },
    { label: 'Columns', value: '2', unit: 'count' },
  ],
  chart_data: {
    viz: 'bar',
    columns: [
      { key: 'business_date', label: 'Business date', dimension: true, decimal: false },
      {
        key: 'gross_revenue',
        label: 'Gross revenue',
        dimension: false,
        decimal: true,
        unit: 'TZS',
      },
    ],
    rows: [
      ['2026-06-01', '1200000.00'],
      ['2026-06-02', '1350000.00'],
    ],
  },
  table: {
    columns: ['Business date', 'Gross revenue'],
    rows: [
      ['2026-06-01', '1200000.00'],
      ['2026-06-02', '1350000.00'],
    ],
  },
  insights: [],
  recommended_actions: [],
  drilldown: [],
  export_options: [],
};

const TEMPLATE = {
  id: 'tpl-1',
  name: 'Net revenue by day',
  description: 'Daily gross revenue trend',
  dataset_key: 'revenue_days',
  spec: {
    dataset: 'revenue_days',
    dimensions: ['business_date'],
    measures: [{ measure: 'gross_revenue', agg: 'sum' }],
    viz: 'bar',
  },
  required_permission: 'revenue.read',
  shared_scope: 'private',
  shared_roles: [],
  created_by: 'user-1',
  is_system: false,
  created_at: '2026-06-10T00:00:00Z',
  updated_at: '2026-06-10T00:00:00Z',
};

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ReportBuilderPage />
    </QueryClientProvider>,
  );
}

describe('ReportBuilderPage', () => {
  beforeEach(() => {
    builderPermitted = true;
    getBuilderDatasets.mockReset().mockResolvedValue(DATASETS);
    previewBuilderReport.mockReset().mockResolvedValue(PREVIEW_ENVELOPE);
    createReportTemplate.mockReset().mockResolvedValue({ id: 'tpl-new' });
    listReportTemplates.mockReset().mockResolvedValue({ items: [], count: 0, has_more: false });
    updateReportTemplate.mockReset().mockResolvedValue({ id: 'tpl-1' });
    deleteReportTemplate.mockReset().mockResolvedValue({ id: 'tpl-1', deleted: true });
    runReportTemplate.mockReset().mockResolvedValue(PREVIEW_ENVELOPE);
    me.mockReset().mockResolvedValue({ user_id: 'user-1', tenant_id: 't-1' });
  });

  afterEach(() => vi.clearAllMocks());

  it('lists only the datasets the actor may use (permission-filtered)', async () => {
    renderPage();
    expect(await screen.findByText('Daily Revenue')).toBeInTheDocument();
    expect(screen.getByText('Shifts')).toBeInTheDocument();
    // A dataset the actor cannot use is absent from getBuilderDatasets, so it
    // never appears as an option.
    expect(screen.queryByText('Audit Logs')).toBeNull();
  });

  it('hides the builder entirely when the actor lacks reports.builder', async () => {
    builderPermitted = false;
    renderPage();
    expect(await screen.findByText('No access to the report builder')).toBeInTheDocument();
    expect(getBuilderDatasets).not.toHaveBeenCalled();
  });

  it('offers ONLY the chosen dataset whitelist for dimensions, measures and filters', async () => {
    renderPage();
    // Pick the Daily Revenue dataset.
    fireEvent.click(await screen.findByRole('button', { name: /Daily Revenue/ }));

    // Dimensions offered are exactly the dataset's registry dimensions.
    expect(await screen.findByRole('button', { name: 'Business date' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Station' })).toBeInTheDocument();

    // The measure picker offers only the dataset's measures (the sensitive
    // margin measure is already stripped from the response, so it is not here).
    const measurePicker = screen.getByRole('combobox', { name: 'Add a measure' });
    const optionLabels = within(measurePicker)
      .getAllByRole('option')
      .map((o) => o.textContent);
    expect(optionLabels.join('|')).toContain('Gross revenue');
    expect(optionLabels.join('|')).toContain('Days');
    expect(optionLabels.join('|')).not.toContain('margin');

    // No free-text identifier input exists for columns — only selects/toggles.
    expect(screen.queryByPlaceholderText(/column|identifier|sql/i)).toBeNull();
  });

  it('constrains a measure aggregate to its per-measure allowlist', async () => {
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: /Daily Revenue/ }));

    // Add the gross_revenue measure; its agg select must offer only sum/avg/min/max.
    const picker = screen.getByRole('combobox', { name: 'Add a measure' });
    fireEvent.change(picker, { target: { value: 'gross_revenue' } });

    const aggSelect = await screen.findByRole('combobox', { name: 'Aggregate for Gross revenue' });
    const aggOptions = within(aggSelect)
      .getAllByRole('option')
      .map((o) => (o as HTMLOptionElement).value);
    expect(aggOptions).toEqual(['sum', 'avg', 'min', 'max']);
    // count is NOT allowed for this money measure, so it is absent.
    expect(aggOptions).not.toContain('count');
  });

  it('previews a spec and renders the returned envelope (table + summary)', async () => {
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: /Daily Revenue/ }));

    // Build a runnable spec: add a measure, then preview.
    fireEvent.change(screen.getByRole('combobox', { name: 'Add a measure' }), {
      target: { value: 'gross_revenue' },
    });
    fireEvent.click(await screen.findByRole('button', { name: /Preview/ }));

    await waitFor(() => expect(previewBuilderReport).toHaveBeenCalledTimes(1));
    // The spec POSTed references only whitelisted identifiers.
    const spec = previewBuilderReport.mock.calls[0]![0];
    expect(spec.dataset).toBe('revenue_days');
    expect(spec.measures).toEqual([{ measure: 'gross_revenue', agg: 'sum' }]);

    // The envelope renders: the preview heading, summary counts and the table rows.
    expect(await screen.findByRole('heading', { name: 'Preview' })).toBeInTheDocument();
    expect(screen.getByText('Rows')).toBeInTheDocument();
    expect(screen.getAllByText('2026-06-01').length).toBeGreaterThan(0);
  });

  it('saves a template with a share scope', async () => {
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: /Daily Revenue/ }));
    fireEvent.change(screen.getByRole('combobox', { name: 'Add a measure' }), {
      target: { value: 'gross_revenue' },
    });

    fireEvent.click(await screen.findByRole('button', { name: /Save as template/ }));

    // Fill the save dialog: name + share scope = tenant.
    fireEvent.change(await screen.findByLabelText('Name'), {
      target: { value: 'My daily revenue' },
    });
    fireEvent.change(screen.getByLabelText('Share scope'), { target: { value: 'tenant' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save template' }));

    await waitFor(() => expect(createReportTemplate).toHaveBeenCalledTimes(1));
    const input = createReportTemplate.mock.calls[0]![0];
    expect(input.name).toBe('My daily revenue');
    expect(input.shared_scope).toBe('tenant');
    expect(input.spec.dataset).toBe('revenue_days');
    expect(input.spec.measures).toEqual([{ measure: 'gross_revenue', agg: 'sum' }]);
  });

  it('renders the saved-template gallery and runs a template', async () => {
    listReportTemplates.mockResolvedValue({ items: [TEMPLATE], count: 1, has_more: false });
    renderPage();

    // The gallery card shows the template.
    expect(await screen.findByText('Net revenue by day')).toBeInTheDocument();
    const card = screen.getByTestId('template-card');
    // Owner sees edit/delete; private scope badge is shown.
    expect(within(card).getByText('Private')).toBeInTheDocument();

    // Running it re-checks the dataset permission server-side and renders the envelope.
    fireEvent.click(within(card).getByRole('button', { name: 'Run' }));
    await waitFor(() => expect(runReportTemplate).toHaveBeenCalledWith('tpl-1'));
    expect(await screen.findByRole('heading', { name: 'Preview' })).toBeInTheDocument();
  });

  it('shows an honest empty state when there are no saved templates', async () => {
    renderPage();
    expect(await screen.findByText('No saved reports yet')).toBeInTheDocument();
  });
});
