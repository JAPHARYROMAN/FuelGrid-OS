'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import {
  Activity,
  AlertCircle,
  BarChart3,
  Building2,
  Database,
  DollarSign,
  Droplet,
  Fuel,
  LayoutDashboard,
  Settings,
  ShieldCheck,
  Sparkles,
  Truck,
  Users,
} from 'lucide-react';

import { cn } from '@fuelgrid/ui';

interface NavItem {
  label: string;
  href: string;
  icon: React.ComponentType<{ className?: string }>;
}

const navItems: NavItem[] = [
  { label: 'Command Center', href: '/command-center', icon: LayoutDashboard },
  { label: 'Stations', href: '/stations', icon: Building2 },
  { label: 'Tanks', href: '/tanks', icon: Database },
  { label: 'Pumps', href: '/pumps', icon: Fuel },
  { label: 'Shifts', href: '/shifts', icon: Activity },
  { label: 'Sales', href: '/sales', icon: BarChart3 },
  { label: 'Inventory', href: '/inventory', icon: Droplet },
  { label: 'Deliveries', href: '/deliveries', icon: Truck },
  { label: 'Customers', href: '/customers', icon: Users },
  { label: 'Finance', href: '/finance', icon: DollarSign },
  { label: 'Reports', href: '/reports', icon: BarChart3 },
  { label: 'Alerts', href: '/alerts', icon: AlertCircle },
  { label: 'AI Assistant', href: '/assistant', icon: Sparkles },
  { label: 'Audit', href: '/audit', icon: ShieldCheck },
  { label: 'Settings', href: '/settings', icon: Settings },
];

/**
 * Sidebar nav. Most entries are visual placeholders for Phase-2+ work —
 * clicking them lands on a 404 today, which is intentional: the shell is
 * how operators will eventually navigate the OS, and seeing the full
 * surface early keeps the design honest.
 */
export function Sidebar() {
  const pathname = usePathname();

  return (
    <aside className="hidden w-60 shrink-0 flex-col gap-1 border-r border-border bg-card/40 p-3 lg:flex">
      <div className="px-3 py-4">
        <span className="text-sm font-semibold tracking-tight text-foreground">FuelGrid OS</span>
        <p className="text-[11px] uppercase tracking-wider text-muted-foreground">command center</p>
      </div>

      <nav className="flex flex-col gap-0.5">
        {navItems.map((item) => {
          const active = pathname === item.href || pathname.startsWith(item.href + '/');
          const Icon = item.icon;
          return (
            <Link
              key={item.href}
              href={item.href}
              className={cn(
                'flex items-center gap-2.5 rounded-md px-3 py-2 text-sm transition-colors',
                active
                  ? 'bg-accent/15 text-foreground'
                  : 'text-muted-foreground hover:bg-muted hover:text-foreground',
              )}
            >
              <Icon className="size-4" />
              <span>{item.label}</span>
            </Link>
          );
        })}
      </nav>
    </aside>
  );
}
