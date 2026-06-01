import { Fuel } from 'lucide-react';

export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="relative grid min-h-screen place-items-center overflow-hidden bg-background p-6">
      {/* Ambient depth: a soft indigo glow over a faint grid. Calm, not flashy. */}
      <div className="bg-grid pointer-events-none absolute inset-0 opacity-60 [mask-image:radial-gradient(ellipse_60%_50%_at_50%_40%,black,transparent)]" />
      <div
        className="pointer-events-none absolute left-1/2 top-1/3 h-[420px] w-[620px] -translate-x-1/2 -translate-y-1/2 rounded-full opacity-40 blur-[120px]"
        style={{
          background: 'radial-gradient(circle, hsl(var(--color-accent) / 0.35), transparent 70%)',
        }}
      />

      <main className="relative w-full max-w-[400px]">
        <div className="mb-6 flex flex-col items-center gap-3 text-center">
          <span className="flex size-11 items-center justify-center rounded-xl bg-accent text-accent-foreground shadow-elev-md">
            <Fuel className="size-5" />
          </span>
          <div>
            <h1 className="text-lg font-semibold tracking-tight text-foreground">FuelGrid OS</h1>
            <p className="text-sm text-muted-foreground">
              The operating system for fuel businesses
            </p>
          </div>
        </div>
        {children}
      </main>
    </div>
  );
}
