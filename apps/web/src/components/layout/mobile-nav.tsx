'use client';

import { Sheet, SheetContent, SheetTitle } from '@fuelgrid/ui';

import { SidebarBrand, SidebarNav } from './sidebar';

interface MobileNavProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * MobileNav — the slide-over navigation drawer for screens narrower than `lg`,
 * opened from the topbar hamburger. Reuses the exact same brand + grouped nav
 * as the desktop sidebar so there is one source of truth for navigation.
 * Tapping any link closes the sheet (via SidebarNav's onNavigate).
 */
export function MobileNav({ open, onOpenChange }: MobileNavProps) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="left" className="p-0">
        <SheetTitle className="sr-only">Navigation</SheetTitle>
        <SidebarBrand />
        <SidebarNav onNavigate={() => onOpenChange(false)} />
      </SheetContent>
    </Sheet>
  );
}
