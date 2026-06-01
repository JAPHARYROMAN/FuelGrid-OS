import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import {
  DataQualityBanner,
  DataQualityCard,
  ExportButtonGroup,
  FilterBar,
  FilterField,
  InsightCard,
  MetricCard,
  ReconciliationWaterfall,
  ReportCategoryCard,
  RiskBadge,
  ShiftTimeline,
} from '../index';

// SSR markup smoke tests, matching the existing harness (no @testing-library).
// They pin the behavior we can assert deterministically: rendered text, the
// over-tolerance branch, empty-list guards, and disabled/permission states.

describe('MetricCard', () => {
  it('renders label, value and trend', () => {
    const html = renderToStaticMarkup(
      <MetricCard
        label="Gross revenue"
        value="2,950.00"
        unit="USD"
        trend="up"
        trendValue="+4.2%"
      />,
    );
    expect(html).toContain('Gross revenue');
    expect(html).toContain('2,950.00');
    expect(html).toContain('+4.2%');
  });

  it('renders a skeleton instead of the value while loading', () => {
    const html = renderToStaticMarkup(<MetricCard label="x" value="1" loading />);
    expect(html).not.toContain('>1<');
  });
});

describe('ReportCategoryCard', () => {
  it('renders title, metric and an alert pill', () => {
    const html = renderToStaticMarkup(
      <ReportCategoryCard
        title="Stock Reconciliation"
        metricLabel="Over tolerance"
        metricValue={3}
        alertCount={3}
      />,
    );
    expect(html).toContain('Stock Reconciliation');
    expect(html).toContain('Over tolerance');
    expect(html).toContain('3 alerts');
  });

  it('omits the alert pill when alertCount is 0', () => {
    const html = renderToStaticMarkup(<ReportCategoryCard title="Sales" alertCount={0} />);
    expect(html).not.toContain('alert');
  });

  it('renders an anchor when href is set', () => {
    const html = renderToStaticMarkup(<ReportCategoryCard title="Sales" href="/reports/sales" />);
    expect(html).toContain('href="/reports/sales"');
  });
});

describe('DataQuality surfaces', () => {
  it('banner renders each message', () => {
    const html = renderToStaticMarkup(
      <DataQualityBanner level="warning" messages={['Missing dip readings', 'Provisional FX']} />,
    );
    expect(html).toContain('Missing dip readings');
    expect(html).toContain('Provisional FX');
  });

  it('banner renders nothing with no messages', () => {
    expect(renderToStaticMarkup(<DataQualityBanner messages={[]} />)).toBe('');
  });

  it('card renders nothing with no messages', () => {
    expect(renderToStaticMarkup(<DataQualityCard messages={[]} />)).toBe('');
  });
});

describe('InsightCard', () => {
  it('renders message, severity label and recommended action', () => {
    const html = renderToStaticMarkup(
      <InsightCard
        severity="critical"
        message="Variance exceeds tolerance"
        recommendedAction="Re-dip tank 3"
      />,
    );
    expect(html).toContain('Variance exceeds tolerance');
    expect(html).toContain('Critical');
    expect(html).toContain('Re-dip tank 3');
  });
});

describe('RiskBadge', () => {
  it('defaults its label to the severity', () => {
    const html = renderToStaticMarkup(<RiskBadge severity="high" />);
    expect(html).toContain('high');
  });
});

describe('ReconciliationWaterfall', () => {
  it('flags over-tolerance when |variance| exceeds tolerance', () => {
    const html = renderToStaticMarkup(
      <ReconciliationWaterfall
        openingStock="10000.000"
        deliveries="5000.000"
        sales="4000.000"
        adjustments="0.000"
        expectedClosing="11000.000"
        actualClosing="10800.000"
        variance="-200.000"
        tolerance="50.000"
      />,
    );
    expect(html).toContain('Over tolerance');
    expect(html).toContain('Variance');
    expect(html).toContain('Expected closing');
  });

  it('reports within-tolerance when variance is inside the band', () => {
    const html = renderToStaticMarkup(
      <ReconciliationWaterfall
        openingStock="10000.000"
        expectedClosing="10000.000"
        actualClosing="10010.000"
        variance="10.000"
        tolerance="50.000"
      />,
    );
    expect(html).toContain('Within tolerance');
  });
});

describe('ShiftTimeline', () => {
  it('renders milestones with timestamps', () => {
    const html = renderToStaticMarkup(
      <ShiftTimeline
        milestones={[
          { label: 'Opened', timestamp: '08:00', status: 'done' },
          { label: 'Closed', timestamp: '20:00', status: 'current' },
        ]}
      />,
    );
    expect(html).toContain('Opened');
    expect(html).toContain('08:00');
    expect(html).toContain('Closed');
  });

  it('renders an empty hint with no milestones', () => {
    const html = renderToStaticMarkup(<ShiftTimeline milestones={[]} />);
    expect(html).toContain('No shift activity');
  });
});

describe('FilterBar', () => {
  it('renders children and actions', () => {
    const html = renderToStaticMarkup(
      <FilterBar actions={<button type="button">Reset</button>}>
        <FilterField label="Station">picker</FilterField>
      </FilterBar>,
    );
    expect(html).toContain('Station');
    expect(html).toContain('Reset');
  });
});

describe('ExportButtonGroup', () => {
  it('renders a button per action', () => {
    const html = renderToStaticMarkup(
      <ExportButtonGroup
        actions={[
          { format: 'csv', onDownload: () => {} },
          { format: 'pdf', onDownload: () => {} },
        ]}
      />,
    );
    expect(html).toContain('CSV');
    expect(html).toContain('PDF');
  });

  it('shows a denial note and disables when permission is false', () => {
    const html = renderToStaticMarkup(
      <ExportButtonGroup permitted={false} actions={[{ format: 'csv', onDownload: () => {} }]} />,
    );
    expect(html).toContain('permission');
    expect(html).toContain('disabled');
  });
});
