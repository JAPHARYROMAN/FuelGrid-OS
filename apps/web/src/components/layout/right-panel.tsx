import { Sparkles } from 'lucide-react';

import { EmptyState } from '@fuelgrid/ui';

/**
 * Right insight panel. Wired with the AI assistant + recommended-actions
 * surface in later stages; for now it documents where intelligence will
 * surface and gives the dashboard the three-column shape called out in
 * docs/ui-ux.md §7.2.
 */
export function RightPanel() {
  return (
    <aside className="hidden w-80 shrink-0 border-l border-border bg-card/30 p-4 xl:block">
      <div className="mb-3 flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
        <Sparkles className="size-3.5" />
        <span>Insights</span>
      </div>
      <EmptyState
        title="Quiet for now"
        description="Recommended actions, alerts, and AI explanations land here once the risk and AI surfaces ship."
        icon={<Sparkles className="size-7" />}
      />
    </aside>
  );
}
