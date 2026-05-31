import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { PumpCard, TankVisual } from '@fuelgrid/ui';

// These dashboard primitives consume the decimal-STRING money/litre contract
// from the SDK. The regression we guard against is Number(x).toFixed() style
// drift surfacing as "NaN" in the UI. Tests assert the formatted output and
// that "NaN" never appears in the rendered tree.

describe('PumpCard', () => {
  it('renders a decimal-string nozzle price with grouping, no NaN', () => {
    render(
      <PumpCard
        number={3}
        status="active"
        nozzles={[
          {
            id: 'n1',
            number: 1,
            productName: 'Diesel',
            productColor: '#123456',
            tankCode: 'T-01',
            price: '2950.00',
          },
        ]}
      />,
    );

    expect(screen.getByText('Pump 3')).toBeInTheDocument();
    expect(screen.getByText('Diesel')).toBeInTheDocument();
    expect(screen.getByText('2,950.00')).toBeInTheDocument();
    expect(document.body.textContent).not.toContain('NaN');
  });

  it('renders an empty-nozzle pump without crashing or NaN', () => {
    render(<PumpCard number={1} status="inactive" nozzles={[]} />);

    expect(screen.getByText('No nozzles configured.')).toBeInTheDocument();
    expect(document.body.textContent).not.toContain('NaN');
  });
});

describe('TankVisual', () => {
  it('renders decimal-string litre dimensions and a numeric reading without NaN', () => {
    render(
      <TankVisual
        name="Main Diesel"
        code="T-01"
        color="#123456"
        capacityLitres="50000.000"
        safeMinLitres="5000.000"
        safeMaxLitres="45000.000"
        currentLitres="25800.000"
      />,
    );

    expect(screen.getByText('Main Diesel')).toBeInTheDocument();
    // Capacity always renders; current renders because a reading is present.
    expect(screen.getByText('50,000 L')).toBeInTheDocument();
    expect(screen.getByText('25,800 L')).toBeInTheDocument();
    expect(document.body.textContent).not.toContain('NaN');
  });

  it('shows an awaiting-reading placeholder (em dash, not NaN) when no current level', () => {
    render(
      <TankVisual
        name="Spare"
        code="T-02"
        color="#abcdef"
        capacityLitres="30000.000"
        safeMinLitres="3000.000"
        safeMaxLitres="27000.000"
        currentLitres={null}
      />,
    );

    expect(screen.getByText('30,000 L')).toBeInTheDocument();
    expect(screen.getByText('awaiting reading')).toBeInTheDocument();
    expect(document.body.textContent).not.toContain('NaN');
  });
});
