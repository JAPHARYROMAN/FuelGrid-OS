'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';

import { cn } from '@fuelgrid/ui';

const nav = [
  { href: '/settings/companies', label: 'Companies' },
  { href: '/settings/regions', label: 'Regions' },
  { href: '/settings/stations', label: 'Stations' },
  { href: '/settings/products', label: 'Products' },
  { href: '/settings/suppliers', label: 'Suppliers' },
  { href: '/settings/customers', label: 'Customers' },
  { href: '/settings/tanks', label: 'Tanks' },
  { href: '/settings/pumps', label: 'Pumps' },
  { href: '/settings/employees', label: 'Employees' },
  { href: '/settings/teams', label: 'Teams & Rotation' },
  { href: '/settings/users', label: 'Users' },
  { href: '/settings/roles', label: 'Roles' },
];

export default function SettingsLayout({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
        <p className="text-sm text-muted-foreground">
          Configure the tenant: companies, regions, stations, users, and role grants.
        </p>
      </header>

      <nav className="flex gap-1 border-b border-border">
        {nav.map((item) => {
          const active = pathname === item.href || pathname.startsWith(item.href + '/');
          return (
            <Link
              key={item.href}
              href={item.href}
              className={cn(
                '-mb-px border-b-2 px-3 py-2 text-sm transition-colors',
                active
                  ? 'border-accent text-foreground'
                  : 'border-transparent text-muted-foreground hover:text-foreground',
              )}
            >
              {item.label}
            </Link>
          );
        })}
      </nav>

      <section>{children}</section>
    </div>
  );
}
