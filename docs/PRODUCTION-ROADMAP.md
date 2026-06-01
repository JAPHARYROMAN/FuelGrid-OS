# FuelGrid OS — Production Roadmap

**Status: ~70% to production.** Core fuel-operations workflows are functional end-to-end;
the foundations (multi-tenant + RLS, decimal-safe money, OpenAPI-enforced contract, CI/CD
pipeline, redesigned UI) are solid. What remains is **deployment infrastructure, integrations,
notifications, scheduling, reporting, and launch hardening** — not core feature plumbing.

This is a high-level, phase-based plan. Phases are ordered by what unblocks a real-world
pilot launch. Each phase lists its goal, the main workstreams, rough magnitude, and a
"done when" bar.

---

## Where we are today (the honest baseline)

**Done / solid**
- 32 backend domain packages; ~180 handlers; all 8 "overview" dashboards compute **real** data (no stubs).
- 18 functional dashboard routes (revenue, inventory, reconciliation, operations/shifts, procurement,
  finance, enterprise, risk, incidents, customers, my-shift, settings/\*, station + pump detail, profile).
- Multi-tenant with Postgres RLS enforced; decimal-safe money everywhere; httpOnly-cookie session via BFF.
- OpenAPI covers all 244 routes with a CI contract gate; CI = lint/typecheck/test/build + race + migrations
  apply/rollback + Trivy scan + SBOM + Playwright e2e + coverage gates.
- Redesigned, high-end UI ("Refined Console": neutral + indigo, Geist, dark/light), applied across every route.
- Transactional outbox + in-process event bus; CD workflow builds & pushes the image to GHCR.

**Gaps blocking production** (detail in phases below)
- No configured deployment target (no IaC, host, managed DB/Redis, TLS, DNS, secrets, backups).
- No outbound notifications (email/SMS/push); SMTP is config-only, not sending.
- No scheduler/worker for recurring jobs (daily revenue close, AR/AP aging, risk runs, report delivery).
- Reporting is audit-CSV only — no exports/PDF/scheduled reports; charts are minimal.
- No external integrations (payments, accounting export, bank feed, fuel cards, ERP).
- 6 sidebar links have no page (`/sales`, `/tanks`, `/pumps`, `/reports`, `/alerts`, `/assistant`).
- Monitoring rules exist but no live Prometheus/Grafana wiring or alert routing.
- Test depth: e2e smoke only; no per-module e2e, load, or performance testing.

---

## Phase 0 — Finish & land the UI (in flight)

**Goal:** the redesign is the app; no dead ends in the nav.

- Merge the redesign PR (#65) to `main` once CI is green.
- Resolve the 6 orphan nav links: build `/sales`, `/reports`, `/alerts` (or fold into existing pages),
  point `/tanks` & `/pumps` at their `settings` equivalents, and either build or hide `/assistant`.
- UI polish pass: command palette restyle, Radix dropdowns/tooltips, sortable/sticky data tables,
  micro-interactions, mobile slide-over sidebar, kill the residual CSP console warning.
- Richer charts on revenue/finance/enterprise (the libraries — recharts/visx — are already installed).

**Magnitude:** Small–Medium. **Done when:** every nav item resolves to a real screen, mobile works,
the UI passes an accessibility pass, and #65 is on `main`.

---

## Phase 1 — Deployment & Infrastructure  ⛔ CRITICAL PATH

**Goal:** the app runs on real, reproducible infrastructure with staging + production.

- **Pick the target** (recommend a managed PaaS first — Fly.io / Render / Railway, or AWS ECS/Fargate
  if AWS is required). Decide region(s) based on customer geography.
- **Infrastructure as Code** (Terraform): app runtime, **managed Postgres** (with PITR backups),
  **managed Redis**, network/security groups, secrets store.
- **Secrets management**: move `DATABASE_URL` / `DATABASE_APP_URL` (the non-owner RLS role),
  `AUTH_PASSWORD_PEPPER`, `PLATFORM_ADMIN_TOKEN`, Sentry DSN, etc. into the platform's secret store;
  wire the CD workflow's `DATABASE_URL` / `DEPLOY_HEALTH_URL` secrets so deploy + migrate + `/readyz`
  smoke actually run.
- **TLS + DNS**: domain, certificates (ACME/Let's Encrypt or platform-managed), HTTPS-only.
- **Environments**: staging (auto-deploy on `main`) + production (tag/approval gated), with seeded
  staging data and a clean production bootstrap (owner role + RLS app role + first tenant provisioning).
- **Backups & DR**: automated DB backups, restore drill, Redis persistence policy, runbook.
- **Web app hosting**: deploy the Next.js app (same platform or Vercel); set `API_ORIGIN` server-side.

**Magnitude:** Large. **Done when:** a tagged release auto-builds, migrates, and deploys to production
behind TLS; `/readyz` is green; a restore-from-backup has been rehearsed.

---

## Phase 2 — Notifications & messaging

**Goal:** the system can reach users — the outbox finally drives something user-visible.

- **Transactional email** (SES/SendGrid/Postmark): user invites, password reset, MFA, account changes.
  (SMTP config exists but nothing sends today.)
- **Event-driven notifications**: subscribe handlers to the existing outbox/bus for the events that
  matter — revenue recognized, shift closed with variance, reconciliation out of tolerance, risk alert
  raised, incident opened, approval requested, credit limit breached.
- **In-app notification center** (the topbar bell + a feed) backed by a `notifications` table.
- **SMS** (Twilio/Africa's Talking) for critical/operational alerts and optional 2FA.
- **Digest emails**: daily station summary, weekly P&L, month-end close package (depends on Phase 3 + 4).

**Magnitude:** Medium–Large. **Done when:** invite/reset emails send in prod, and at least the
risk/variance/approval events produce in-app + email notifications.

---

## Phase 3 — Scheduling & background workers

**Goal:** recurring business processes run on their own, not only when someone clicks a button.

- A **scheduler/worker** process (Go cron loop, or a job queue like River/Asynq backed by Postgres/Redis),
  deployed alongside the API, multi-instance-safe (leader election or queue locks).
- **Scheduled jobs**: daily revenue compute + close reminders, AR/AP aging bucket refresh, periodic risk
  detection + score recompute, enterprise projection rebuilds, reconciliation-tolerance monitor,
  scheduled report delivery, outbox dead-letter sweeper, session/token cleanup.
- **Operational guards**: per-job metrics, retries with backoff, alerting on job failure/lag.

**Magnitude:** Medium. **Done when:** the manual "rebuild projections / run detection / compute day"
actions also run automatically on schedule, with visibility into job health.

---

## Phase 4 — Reporting, exports & analytics

**Goal:** operators can get data *out* — for management, audits, and accounting.

- **Exports** for the core domains (revenue, inventory, reconciliation, P&L, AR/AP) as CSV/XLSX, and
  **PDF** for formal documents (shift report, daily close, month-end package, invoices/statements).
- **Scheduled report packages** (ties into Phases 2 & 3): emailed daily/weekly/monthly.
- **The `/reports` screen**: a library of standard reports with date/station filters and download.
- **Analytics depth**: richer charts (revenue trend, tender mix over time, station performance, tank
  level history, variance heatmap); consider a read-model/materialized-view layer for fast aggregates
  as data grows.

**Magnitude:** Medium–Large. **Done when:** a manager can pull a month-end close pack and a daily
station report as PDF/Excel, on demand and on schedule.

---

## Phase 5 — External integrations

**Goal:** FuelGrid connects to the financial and operational systems a real business already uses.
Prioritize by target market.

- **Payments / mobile money** (e.g. M-Pesa / Flutterwave / Stripe) — collections, reconciliation of
  electronic tenders against the revenue ledger.
- **Accounting export** — QuickBooks Online / Xero connector, or a clean GL export (journal entries →
  standard format) for the customer's accountant.
- **Bank feed / statement import** — reconcile cash deposits against shift submissions.
- **Fuel cards / fleet** (the `fleet` domain exists) — card transaction import & matching.
- **(Later) ERP** — NetSuite/SAP/Odoo for enterprise customers.

**Magnitude:** Large (each connector is a project). **Done when:** at least one payment path and one
accounting export are live for the pilot customer.

---

## Phase 6 — Security, compliance & data governance

**Goal:** safe to hold a real business's money data.

- **MFA rollout** (TOTP was scaffolded/deferred) — enforce for admin/finance roles.
- **Threat model + pen test**; fix findings. Tighten rate limits/lockouts per real traffic.
- **Data governance**: PII inventory, retention policies (audit log, sessions), tenant data
  export & deletion (privacy requests), encryption-at-rest confirmation, key/secret rotation.
- **Access reviews**: least-privilege DB roles confirmed in prod, admin/break-glass procedures.
- **Supply chain**: keep Trivy/SBOM gates; add dependency update automation; pin & verify images.
- **Compliance posture** (only if required by market): SOC 2 / ISO controls, DPA templates.

**Magnitude:** Medium–Large. **Done when:** MFA is enforceable, a pen test is passed, and tenant
data export/delete + secret rotation are documented and tested.

---

## Phase 7 — Observability, QA & launch readiness

**Goal:** we can see it, trust it, and operate it.

- **Observability wiring**: connect the existing Prometheus alert rules to a real Prometheus/Grafana
  (or a hosted APM), define SLOs + dashboards, route alerts to Slack/PagerDuty, confirm Sentry in prod.
- **Test depth**: per-module Playwright e2e (login→shift→close, procurement→receive, reconciliation,
  finance close), API contract/integration coverage, **load testing** to find the breaking point,
  DB query/index review (N+1 audit) under realistic data volumes.
- **Performance**: pagination/virtualization on big lists, caching hot reads, image/bundle budgets.
- **Operability**: on-call runbooks, incident process, status page, log retention.
- **Product polish**: onboarding/tenant-provisioning flow, empty-state guidance, in-app help,
  i18n/currency/timezone correctness (company currency + station timezone already modeled),
  accessibility (WCAG) audit.
- **Pilot**: run a real station (or a controlled beta) end-to-end before GA.

**Magnitude:** Medium–Large. **Done when:** dashboards + alerts are live, e2e covers the critical
journeys, load tests pass the target, and a pilot has run a full operating cycle.

---

## Phase 8 — Post-launch / scale (future)

- **AI Assistant** (`/assistant`) — natural-language ops queries over the data.
- **Kafka migration** — the transactional outbox is already shaped for this when event volume warrants.
- **Mobile** — the app is an installable PWA; add offline/service-worker (deferred), or a native shell.
- **Advanced BI / data warehouse**, multi-region, marketplace of integrations.

---

## Critical path to a pilot launch

```
Phase 0 (finish UI)  →  Phase 1 (deploy infra)  →  Phase 2 (email/notifications)
                                                 →  Phase 3 (scheduled jobs)
                                                 →  Phase 4 (reports/exports)
                                                 →  Phase 5 (one payment + accounting export)
                                                 →  Phase 6/7 (MFA, monitoring, e2e, pilot)
```

**Phase 1 is the hard blocker** — nothing ships to a customer until there's a real, backed-up,
TLS-secured deployment. Phases 2–5 can run partly in parallel once infra exists. A lean **pilot
launch** needs: Phase 1, the must-have of Phase 2 (auth emails), the highest-value bits of Phase 4
(daily/close reports), MFA + monitoring from Phase 6/7, and at least one integration from Phase 5
that the pilot customer requires.
