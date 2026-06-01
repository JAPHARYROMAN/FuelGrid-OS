# FuelGrid OS — Redesign Spec ("Refined Console")

Authoritative guide for converting every page to the new design system. The
foundation (tokens, fonts, shell, component library) is already built on the
`redesign/ui` branch. Your job on a page is **presentation only** — match these
patterns; never change behaviour.

## Reference pages (the gold standard — read them first)

- `apps/web/src/app/(dashboard)/command-center/page.tsx` — PageHeader + Stat grid + Card lists + Skeleton loading.
- `apps/web/src/app/(dashboard)/stations/page.tsx` — PageHeader + card grid + Skeleton/Empty/Error states.

Match their structure, spacing, and tone exactly.

## Design language

- Neutral greys + a single **indigo** accent. Status colors (success/warning/danger/info) are for STATE ONLY — never decoration.
- **Geist** type is already wired. Numbers/money/litres render in `font-mono tabular-nums`.
- Generous rhythm, sharp hierarchy, subtle depth. Calm, not flashy. Dark and light are both first-class — only use semantic token classes (never hard-coded colors), so both themes work automatically.

## Components available from `@fuelgrid/ui`

`Button` (variant: primary|secondary|ghost|danger|outline; size: sm|md|lg|icon),
`Card`/`CardHeader`/`CardTitle`/`CardDescription`/`CardContent`/`CardFooter`,
`Input`, `Label`, `Badge` (tone: neutral|success|warning|danger|info|accent),
`Table`/`TableHeader`/`TableBody`/`TableRow`/`TableHead`/`TableCell`,
`Dialog`* , `EmptyState`/`LoadingState`/`ErrorState`,
`Stat` (props: label, value, unit?, delta?, direction?: up|down|flat, hint?, icon?),
`Skeleton`, `Separator`, `PageHeader` (props: title, description?, eyebrow?, actions?),
`TankVisual`, `PumpCard`, and `formatMoney` / `formatLitres`.

## Required page patterns

1. **Root wrapper:** `<div className="flex flex-col gap-7">`.
2. **Header:** every page starts with `<PageHeader eyebrow="<Section>" title="<Title>" description="<one line>" actions={<primary actions>} />`. Remove any ad-hoc `<header><h1>…` blocks.
3. **KPIs:** when a page shows headline numbers, use a `Stat` grid:
   `<section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">` with `<Stat ... icon={<LucideIcon />} />`. Put money/litre values through `formatMoney`/`formatLitres`.
4. **Tables/lists:** wrap in a `Card`; use the `Table` primitives. Right-align numeric columns and render them `font-mono tabular-nums`. Status → `Badge` with the right tone.
5. **Loading:** use `Skeleton` blocks shaped like the content (stat cards → `h-[120px]`, rows → `h-14`), NOT a bare centered spinner.
6. **Error:** `<ErrorState title description onRetry={() => query.refetch()} />`.
7. **Empty:** `<EmptyState title description icon={<LucideIcon/>} action?/>`.
8. **Icons:** lucide-react, sized `size-4`/`size-[18px]`, often in an accent chip: `<span className="flex size-9 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">`.
9. **Forms:** Label + Input + Button; group with `flex flex-col gap-4`; keep field validation/errors.

## HARD RULES (do not break)

- **Behaviour is frozen.** Preserve every `useQuery`/`useMutation`/`queryKey`, form schema, submit handler, validation, toast, permission gate (`PermissionGate`/`usePermission`), route param, and prop. Only change JSX/markup/className.
- **Do not invent data.** Only render fields the API/types actually provide.
- **Do not edit** `packages/ui/**`, `apps/web/src/app/globals.css`, the shell (`components/layout/**`, `(dashboard)/layout.tsx`, `(auth)/layout.tsx`), `lib/**`, `stores/**`, or middleware. Those are done. Touch only your assigned `page.tsx` files (you MAY add a co-located component file inside the same route folder if it helps).
- **Only semantic token classes** (`bg-card`, `text-muted-foreground`, `border-border`, `text-accent`, `bg-accent-muted`, `text-success`…). Never hard-coded hex/`bg-blue-500`/`bg-navy`.

## Verify before you push

From the worktree root: `pnpm -C apps/web typecheck` and `pnpm -C apps/web lint` MUST pass. Run `pnpm -C apps/web build` if feasible. Fix anything you broke.
