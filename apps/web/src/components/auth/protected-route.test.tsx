import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { ProtectedRoute } from './protected-route';

describe('ProtectedRoute', () => {
  it('renders children without consulting a client-side session hint', () => {
    render(
      <ProtectedRoute>
        <div>secret dashboard</div>
      </ProtectedRoute>,
    );

    expect(screen.getByText('secret dashboard')).toBeInTheDocument();
  });
});
