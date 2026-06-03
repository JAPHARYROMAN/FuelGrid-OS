'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import {
  Activity,
  AlertCircle,
  AlertTriangle,
  BarChart3,
  Bell,
  Building2,
  CalendarClock,
  ClipboardCheck,
  Database,
  DollarSign,
  Droplet,
  Fuel,
  Gauge,
  LayoutDashboard,
  ListChecks,
  Palette,
  Receipt,
  Scale,
  ScrollText,
  ServerCog,
  Settings,
  ShieldCheck,
  Smartphone,
  Sparkles,
  Tag,
  Truck,
  Users,
  UserCog,
  Wallet,
} from 'lucide-react';

import { cn } from '@fuelgrid/ui';

interface NavItem {
  label: string;
  href: string;
  icon: React.ComponentType<{ className?: string }>;
}

interface NavGroup {
  label: string;
  items: NavItem[];
}

/**
 * Grouped navigation. Sections give the 20+ destinations a legible hierarchy
 * instead of one long flat list — the single biggest "clutter" fix in the
 * shell. Most entries are Phase-2+ placeholders; seeing the full surface keeps
 * the information architecture honest.
 */
const navGroups: NavGroup[] = [
  {
    label: 'Overview',
    items: [
      { label: 'Command Center', href: '/command-center', icon: LayoutDashboard },
      { label: 'Enterprise', href: '/enterprise', icon: Building2 },
    ],
  },
  {
    label: 'Operations',
    items: [
      { label: 'My Shift', href: '/my-shift', icon: Gauge },
      { label: 'Operations', href: '/operations', icon: ClipboardCheck },
      { label: 'Stations', href: '/stations', icon: Building2 },
      { label: 'Tanks', href: '/tanks', icon: Database },
      { label: 'Pumps', href: '/pumps', icon: Fuel },
      { label: 'Employees', href: '/settings/employees', icon: UserCog },
      { label: 'Teams & Roster', href: '/settings/teams', icon: CalendarClock },
    ],
  },
  {
    label: 'Commerce',
    items: [
      { label: 'Sales', href: '/sales', icon: BarChart3 },
      { label: 'Revenue', href: '/revenue', icon: Receipt },
      { label: 'Payments', href: '/payments', icon: Smartphone },
      { label: 'Pricing', href: '/settings/pricing', icon: Tag },
      { label: 'Inventory', href: '/inventory', icon: Droplet },
      { label: 'Reconciliation', href: '/reconciliation', icon: Scale },
      { label: 'Adjustments', href: '/inventory/adjustments', icon: ClipboardCheck },
      { label: 'Procurement', href: '/procurement', icon: Truck },
      { label: 'Customers', href: '/customers', icon: Users },
    ],
  },
  {
    label: 'Finance',
    items: [
      { label: 'Finance', href: '/finance', icon: DollarSign },
      { label: 'Expenses', href: '/expenses', icon: Wallet },
      { label: 'Payables Aging', href: '/payables/aging', icon: Receipt },
      { label: 'Credit Invoices', href: '/credit/invoices', icon: Receipt },
      { label: 'Reports', href: '/reports', icon: BarChart3 },
    ],
  },
  {
    label: 'Monitoring',
    items: [
      { label: 'Notifications', href: '/notifications', icon: Bell },
      { label: 'Observability', href: '/observability', icon: Activity },
      { label: 'Incidents', href: '/incidents', icon: AlertTriangle },
      { label: 'Alerts', href: '/alerts', icon: AlertCircle },
      { label: 'Risk', href: '/risk', icon: ShieldCheck },
      { label: 'Audit', href: '/audit', icon: ShieldCheck },
      { label: 'Audit log', href: '/audit-log', icon: ScrollText },
    ],
  },
  {
    label: 'System',
    items: [
      { label: 'Setup', href: '/setup', icon: ListChecks },
      { label: 'System', href: '/settings/system', icon: ServerCog },
      { label: 'Automation', href: '/automation', icon: Sparkles },
      { label: 'Design system', href: '/style', icon: Palette },
      { label: 'Settings', href: '/settings', icon: Settings },
    ],
  },
];

/** Brand lockup, shared by the desktop sidebar and the mobile drawer header. */
export function SidebarBrand() {
  return (
    <div className="flex h-16 items-center gap-2.5 px-5">
      <span className="flex size-8 items-center justify-center rounded-lg bg-accent text-accent-foreground shadow-elev-sm">
        <Fuel className="size-4" />
      </span>
      <div className="flex flex-col leading-none">
        <span className="text-[15px] font-semibold tracking-tight text-foreground">FuelGrid</span>
        <span className="text-[11px] text-muted-foreground">Operations</span>
      </div>
    </div>
  );
}

/**
 * The grouped nav list. Shared between the persistent desktop sidebar and the
 * mobile slide-over sheet. `onNavigate` lets the sheet close itself when a
 * link is tapped.
 */
export function SidebarNav({ onNavigate }: { onNavigate?: () => void }) {
  const pathname = usePathname();

  return (
    <nav className="flex-1 overflow-y-auto px-3 pb-6">
      {navGroups.map((group) => (
        <div key={group.label} className="mb-5">
          <p className="px-3 pb-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/70">
            {group.label}
          </p>
          <div className="flex flex-col gap-0.5">
            {group.items.map((item) => {
              const active = pathname === item.href || pathname.startsWith(item.href + '/');
              const Icon = item.icon;
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  onClick={onNavigate}
                  aria-current={active ? 'page' : undefined}
                  className={cn(
                    'group relative flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors',
                    active
                      ? 'bg-accent-muted/70 font-medium text-foreground'
                      : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                  )}
                >
                  {active ? (
                    <span className="absolute left-0 top-1/2 h-4 w-0.5 -translate-y-1/2 rounded-full bg-accent" />
                  ) : null}
                  <Icon
                    className={cn(
                      'size-[18px] shrink-0 transition-colors',
                      active ? 'text-accent' : 'text-muted-foreground group-hover:text-foreground',
                    )}
                  />
                  <span className="truncate">{item.label}</span>
                </Link>
              );
            })}
          </div>
        </div>
      ))}
    </nav>
  );
}

export function Sidebar() {
  return (
    <aside className="hidden w-64 shrink-0 flex-col border-r border-border bg-surface lg:flex">
      <SidebarBrand />
      <SidebarNav />
    </aside>
  );
}
