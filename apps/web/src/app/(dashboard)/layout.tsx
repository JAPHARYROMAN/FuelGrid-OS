'use client';

import { useState } from 'react';

import { TooltipProvider } from '@fuelgrid/ui';

import { ProtectedRoute } from '@/components/auth/protected-route';
import { CommandPalette } from '@/components/layout/command-palette';
import { MobileNav } from '@/components/layout/mobile-nav';
import { Sidebar } from '@/components/layout/sidebar';
import { Topbar } from '@/components/layout/topbar';
import { Toaster } from '@/components/toaster';

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  const [commandOpen, setCommandOpen] = useState(false);
  const [mobileNavOpen, setMobileNavOpen] = useState(false);

  return (
    <ProtectedRoute>
      <TooltipProvider delayDuration={200}>
        <div className="flex h-screen overflow-hidden bg-background text-foreground">
          <Sidebar />
          <div className="flex min-w-0 flex-1 flex-col">
            <Topbar
              onOpenCommand={() => setCommandOpen(true)}
              onOpenMobileNav={() => setMobileNavOpen(true)}
            />
            <main className="flex-1 overflow-y-auto">
              <div className="mx-auto w-full max-w-[1440px] px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
                {children}
              </div>
            </main>
          </div>
        </div>
        <MobileNav open={mobileNavOpen} onOpenChange={setMobileNavOpen} />
        <CommandPalette open={commandOpen} onOpenChange={setCommandOpen} />
        <Toaster />
      </TooltipProvider>
    </ProtectedRoute>
  );
}
