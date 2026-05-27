import type { Metadata } from 'next';

import { Providers } from './providers';
import './globals.css';

const appName = process.env.NEXT_PUBLIC_APP_NAME ?? 'FuelGrid OS';

export const metadata: Metadata = {
  title: appName,
  description: 'The operating system for modern fuel businesses.',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className="font-sans antialiased">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
