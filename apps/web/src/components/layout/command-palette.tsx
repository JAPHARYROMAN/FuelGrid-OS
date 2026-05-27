'use client';

import * as React from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { Command as CommandPrimitive } from 'cmdk';
import { Search } from 'lucide-react';

import { cn } from '@fuelgrid/ui';

interface CommandPaletteProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * Skeleton command palette. It opens, it accepts text, it shows an
 * empty state. Concrete commands (jump-to-station, run-report,
 * ask-AI-assistant) plug in once those features exist.
 */
export function CommandPalette({ open, onOpenChange }: CommandPaletteProps) {
  // Ctrl/Cmd+K toggles the palette from anywhere in the app.
  React.useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        onOpenChange(!open);
      }
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [open, onOpenChange]);

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-background/70 backdrop-blur-sm" />
        <Dialog.Content
          className={cn(
            'fixed left-1/2 top-[20%] z-50 w-[min(640px,calc(100%-2rem))] -translate-x-1/2',
            'overflow-hidden rounded-xl border border-border bg-card shadow-xl',
          )}
        >
          <Dialog.Title className="sr-only">Command palette</Dialog.Title>
          <Dialog.Description className="sr-only">
            Search across the FuelGrid OS surface.
          </Dialog.Description>

          <CommandPrimitive className="flex flex-col">
            <div className="flex items-center gap-2 border-b border-border px-3 py-2.5">
              <Search className="size-4 text-muted-foreground" />
              <CommandPrimitive.Input
                placeholder="Search…"
                className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
              />
            </div>
            <CommandPrimitive.List className="max-h-80 overflow-y-auto p-2">
              <CommandPrimitive.Empty className="p-6 text-center text-sm text-muted-foreground">
                No commands yet. Wired to global search once the data layer for stations, customers,
                and reports lands.
              </CommandPrimitive.Empty>
            </CommandPrimitive.List>
          </CommandPrimitive>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
