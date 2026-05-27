import type { NextConfig } from 'next';

const nextConfig: NextConfig = {
  reactStrictMode: true,
  /**
   * apps/web imports source from sibling workspace packages
   * (@fuelgrid/ui, @fuelgrid/sdk). transpilePackages tells Next.js to
   * compile their TypeScript instead of treating them as published
   * pre-built modules.
   */
  transpilePackages: ['@fuelgrid/ui', '@fuelgrid/sdk'],
};

export default nextConfig;
