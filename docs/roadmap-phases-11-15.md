# Phases 11–15 — Intelligence, Integrations & Production Hardening

Phases 1–10 built the **system of record**: FuelGrid OS captures every liter and shilling (Phases 1–6), turns them into financial and credit control (Phases 7–9), and flags what's wrong (Phase 10). Those phases write the truth.

Phases 11–15 are the **platform and intelligence layers** that turn that truth into foresight, conversation, connection to the physical and financial world, field reach, and production-grade reliability:

- **Phase 11 — Forecasting & Automation:** predict what happens next and act on rules automatically.
- **Phase 12 — AI Assistant:** ask the system questions in plain language and get evidence-cited answers.
- **Phase 13 — Hardware & External Integrations:** connect to pumps, tank gauges, POS, banks, mobile money, and accounting.
- **Phase 14 — Mobile & Offline OS:** run the forecourt from a phone, online or not.
- **Phase 15 — Enterprise Hardening:** make all of it fast, observable, recoverable, and compliant.

These layers are **largely additive**. They consume the audited event stream and the ledgers the earlier phases produced; they do not change the core write paths. A forecast is derived from the stock ledger, an AI answer cites reconciliations, a webhook fires off the outbox — the source of truth stays exactly where Phases 1–10 put it.

This combined roadmap is organised as **five categories (one per phase), each with four stages** — twenty stages total. Stages are numbered globally (1–20) for reference.

## Stack decisions (carried forward from Phases 1–10)

All prior patterns continue: one tx wraps business change + audit + outbox; `tenantID`-first repos with RLS; composite tenant FKs; the `requirePermission`/`authorizeStation`/`requirePermissionHeld` authorization model; one-concern migrations; decimal money/litre precision; shadcn-style UI over a hand-written `@fuelgrid/sdk`.

Technologies these phases introduce (already named in the blueprint's architecture):

| Concern | Choice |
|---|---|
| Analytics store | **ClickHouse** for high-volume events, aggregations, and reporting; Postgres stays the transactional source of truth |
| Eventing | Domain events ride the existing **outbox**; projected to ClickHouse and delivered to webhooks. Kafka/NATS optional at chain/enterprise scale |
| AI | An LLM provider behind a **gateway**; retrieval scoped by the same policy layer as the API; no training on tenant data |
| Integrations | **Adapter/port** pattern; a credential vault; signed inbound webhooks; outbound effects via the outbox |
| Mobile / offline | **PWA** (Next.js) + encrypted local store + a sync engine |
| Observability | **OpenTelemetry** traces/metrics/logs; object storage for documents, photos, and backups |

New conventions specific to Phases 11–15:

| Concern | Convention |
|---|---|
| Derived, not authoritative | Forecasts, insights, risk scores, and analytics aggregates are **computed snapshots** over the source ledgers/events — versioned and reproducible, never the system of record. Deleting them and recomputing must yield the same answer. |
| Analytics offload | High-volume reads and aggregations move to ClickHouse via event projection; transactional writes never depend on the analytics store being up. |
| AI safety | Every AI data fetch is permission- and station-scoped through the **same policy layer** as the API. Answers cite the records they used. The assistant **recommends** actions but never mutates state except through a human-approved, separately-authorized command. Every query is audit-logged. |
| External effects via outbox | All outbound integration calls and webhooks ride the outbox: **at-least-once, idempotent, retried with backoff**. Inbound webhooks are signature-verified. Adapters are swappable behind a stable port interface. |
| Offline truth model | The device keeps an **append-only encrypted op-log**; the server stays the source of truth. Conflicting edits are preserved (never silently overwritten) and resolved by a supervisor, producing an audit entry — reusing the Phase-3 "supersede, never overwrite" discipline. |
| Production gates | Each capability ships behind **SLOs + observability**; sensitive actions stay audited; closed periods and locked records stay locked. |

---

## Category A — Forecasting & Automation (Phase 11)

Predict what happens next, and let rules act without a human in the loop.

### Stage 1 — Analytics data foundation

**Goal:** Domain events project into an analytical store so high-volume forecasting and reporting queries never touch the transactional path.

- [ ] Stand up ClickHouse; define a projection consumer that reads the outbox and writes analytical fact tables (sales, stock movements, reconciliations, deliveries, cash) keyed by tenant/station/product/day
- [ ] Materialized daily aggregates: liters sold, expected vs submitted cash, variance, deliveries, book/physical stock — per station, product, and day
- [ ] Backfill job to replay historical events into ClickHouse; idempotent + resumable
- [ ] Permission `analytics.read` (tenant-wide, station-filtered); reads never block on the projection lag
- [ ] Freshness + lag metrics exposed (how far behind real-time the projection is)

**Done when:** A station's 90-day daily sales/variance series returns from ClickHouse in milliseconds, matches the Postgres ledger to the cent, and survives a full replay-rebuild.

---

### Stage 2 — Demand forecasting & stockout prediction

**Goal:** Each tank gets a projected runout time and a recommended reorder point from its sales trend and seasonality.

- [ ] Forecasting service: per-tank demand model from the 14/30/90-day sales series with day-of-week + seasonal terms
- [ ] `forecast_snapshots` (tenant, station, tank/product, horizon, projected daily demand, projected runout time, confidence, model version, generated_at) — immutable, versioned
- [ ] Stockout prediction: hours-to-minimum given current book stock and projected demand
- [ ] Scheduled regeneration (e.g. nightly + after each reconciliation seal); each run is a new snapshot, not an overwrite
- [ ] Endpoint: get the latest forecast for a tank/station with its confidence + the data window it used

**Done when:** A tank trending toward its safe-minimum returns "projected to reach minimum in ~38h, high confidence," reproducible from the snapshot's recorded inputs.

---

### Stage 3 — Reorder recommendations & procurement planning

**Goal:** Forecasts become concrete order suggestions that feed the Phase-5 purchase-order flow.

- [ ] Reorder engine: recommended order quantity + delivery window per tank from projected runout, safe-max headroom, and supplier lead time
- [ ] Network procurement plan: aggregate recommendations across a region/chain into a consolidated suggested-order view
- [ ] One-click "raise PO from recommendation" that pre-fills a Phase-5 draft purchase order (human still approves)
- [ ] Permission `procurement.plan` (reuses supplier/PO scopes for the actual PO)
- [ ] Recommendations are advisory snapshots — accepting one is an audited action

**Done when:** A station with a 38h runout shows "order ~22,000 L, deliver before 10:00 tomorrow," and accepting it opens a pre-filled Phase-5 PO draft.

---

### Stage 4 — Rule engine, scheduled reports & escalation

**Goal:** Operators define declarative rules that watch metrics/events and fire actions automatically, including scheduled report delivery and escalation.

- [ ] `alert_rules` (tenant, scope, condition over a metric/event, threshold, severity, action, schedule, enabled) with a safe evaluator (no arbitrary code)
- [ ] Rule actions: raise an alert, notify a channel, create an investigation, escalate after N minutes unacknowledged
- [ ] Scheduled reports: pick a report + cadence + recipients/format; the scheduler renders and delivers (email/in-app/webhook) off the outbox
- [ ] Escalation workflow: unacknowledged high/critical alerts climb the role chain (supervisor → manager → regional) on a timer
- [ ] Permission `automation.manage`; every rule change is audited; rule firings are recorded

**Done when:** A "PMS variance > tolerance for 2 consecutive days" rule fires a high alert, escalates to the regional manager after 30 unacknowledged minutes, and a daily station report lands by email each morning.

---

## Category B — AI Assistant (Phase 12)

Ask the system anything; get an answer it can prove.

### Stage 5 — Permission-aware query gateway

**Goal:** A retrieval layer that answers structured questions over system data while enforcing the actor's exact RBAC + station scope.

- [ ] Query gateway: maps an intent (metric + filters + window) to scoped reads through the **same policy layer** the API uses — never a bypass
- [ ] A read-only tool/function catalogue the model may call (e.g. `getStationSales`, `getTankReconciliation`, `listOpenAlerts`), each permission-checked per call
- [ ] `ai_queries` audit table (actor, prompt, resolved tools + filters, scope applied, answer ref, latency)
- [ ] Hard guarantee: a station-restricted user can never retrieve another station's data through the assistant
- [ ] Permission `ai.use` (who may use the assistant at all)

**Done when:** An operator scoped to one station asks "compare my station to the network" and gets only their station's figures plus a clear "you don't have network access" note — verifiable in the query audit.

---

### Stage 6 — Conversational interface & evidence-cited answers

**Goal:** A chat surface that turns natural-language questions into scoped queries and returns answers that cite the records behind them.

- [ ] `/assistant` chat UI: streamed answers, suggested questions, inline charts/tables when the answer is quantitative
- [ ] NL → structured query via the Stage-5 gateway; the model only sees data the tools return
- [ ] Every answer renders a **"based on"** source list (the reconciliations, readings, deliveries it used)
- [ ] Uncertainty is surfaced ("data only covers May 24–26"), not hidden
- [ ] Conversation history per user, audit-logged

**Done when:** "Why did diesel losses rise yesterday?" returns a plain-language explanation citing the specific tank reconciliation, pump readings, and shift cash submissions it drew from.

---

### Stage 7 — Insight & report generation

**Goal:** The assistant drafts executive summaries, variance/forecast explanations, and report bodies on demand.

- [ ] Executive summary generator: network/station rollups in prose ("sold 184,200 L across 14 stations, up 8.4%…")
- [ ] Variance & forecast explainers that narrate Phase-10 risk events and Phase-11 forecasts in business language
- [ ] Report drafting: generate a report body the user can review, edit, and export (PDF/Excel) via the Phase-16 reporting surface
- [ ] `ai_insight_snapshots` (scope, generated text, source refs, model version, generated_at) for reproducibility/audit
- [ ] All generated text carries its evidence references; nothing is asserted without a backing record

**Done when:** A manager clicks "summarize yesterday" and gets an accurate, citation-backed paragraph plus a downloadable daily report draft.

---

### Stage 8 — Investigation assistant & guardrails

**Goal:** For a flagged risk event, the assistant assembles the evidence and recommends next actions — within strict safety rules.

- [ ] Investigation mode: given a Phase-10 alert, pull the related shifts, pump/tank readings, deliveries, attendants, and audit entries into one evidence pack
- [ ] Recommended-actions output (recalibrate pump, audit evening cash, require supervisor approval) — **recommendations only**
- [ ] Guardrails enforced and tested: respects permissions, never exposes restricted financials, cites records, no unsupported claims, never executes a state change itself, logs every query
- [ ] Any action the user accepts goes through the normal authorized, audited endpoint — never the assistant
- [ ] Red-team test suite for prompt-injection, scope-escape, and data-exfiltration attempts

**Done when:** Opening an investigation on a PMS-loss alert produces an evidence-backed brief and an action checklist, and the guardrail suite proves the assistant can't read out-of-scope data or mutate records.

---

## Category C — Hardware & External Integrations (Phase 13)

Connect FuelGrid OS to the physical and financial world.

### Stage 9 — Integration core & adapter framework

**Goal:** A stable port/adapter layer with credentials, retries, idempotency, and a full event log — the spine every integration plugs into.

- [ ] Adapter interface (port) + a connection registry (`integrations` table: type, status, config, tenant/station scope)
- [ ] Credential vault: encrypted-at-rest secrets, rotation, never logged
- [ ] `integration_events` log: every inbound/outbound call with payload refs, status, retries, latency
- [ ] Outbound delivery off the outbox with idempotency keys + exponential backoff; inbound webhooks signature-verified
- [ ] Permission `integration.manage`; a sandbox mode per adapter for safe testing

**Done when:** An adapter can be registered, exercised in sandbox, and every call it makes appears in the integration event log with retry/idempotency behavior proven.

---

### Stage 10 — Hardware adapters (pumps, tank gauges, POS, scanners)

**Goal:** Physical devices feed readings and transactions directly, reducing manual capture.

- [ ] Pump-controller adapter: live meter readings, transaction capture, pump/nozzle status, price push (where supported) — feeding the Phase-3 reading path
- [ ] Automatic tank gauge (ATG) adapter: real-time volume, water, temperature, ullage, leak/sensor-health alerts → an automated physical reading source alongside manual dips
- [ ] POS, QR/RFID reader, and weighbridge adapters for sale/auth/weight capture
- [ ] Reconcile device-sourced readings against manual ones; flag divergence (a Phase-10 signal)
- [ ] Device readings are attributed to the device identity and audited like any reading

**Done when:** An ATG-fed tank shows a live physical level that the daily reconciliation can use in place of (or cross-checked against) a manual dip, with divergence flagged.

---

### Stage 11 — Financial adapters (mobile money, banks, cards, accounting/ERP)

**Goal:** Money movements confirm and settle automatically, and finance data exports to accounting.

- [ ] Mobile-money adapter: payment confirmation, transaction reference, settlement matching against Phase-7 cash/settlement records, reversal tracking
- [ ] Bank + card-processor adapters: deposit confirmation and card settlement matching
- [ ] Accounting/ERP adapter: journal exports, chart-of-accounts mapping, tax mapping for customer invoices, supplier bills, expenses, payments
- [ ] Settlement reconciliation: auto-match external settlement batches to internal expectations; surface unmatched lines
- [ ] Tax/fiscal adapter hook for statutory receipts where required

**Done when:** A mobile-money payment auto-confirms and matches its Phase-7 cash submission, and a day's finance entries export to the configured accounting system with correct COA mapping.

---

### Stage 12 — Developer platform (public API, webhooks, OAuth)

**Goal:** External systems integrate with FuelGrid OS through a documented, secured public surface.

- [ ] Public REST (and gRPC) surface over the existing OpenAPI contract, versioned
- [ ] API keys + OAuth applications, scoped to permissions + stations, with rate limits
- [ ] Outbound webhooks for domain events (`sale.created`, `delivery.received`, `stock.low`, `shift.closed`, `invoice.generated`, `payment.received`, `alert.critical`) with HMAC signing + retry/replay
- [ ] `webhooks` + `webhook_deliveries` tables; a delivery dashboard with manual replay
- [ ] Developer docs + a sandbox tenant; permission `api.manage`

**Done when:** A third party registers an app, subscribes to `delivery.received`, and reliably receives signed, retried webhook deliveries it can verify — all visible in the delivery dashboard.

---

## Category D — Mobile & Offline OS (Phase 14)

Run the forecourt from a phone, with or without a network.

### Stage 13 — Mobile field app shell

**Goal:** A mobile-first PWA for field roles that reuses the existing API and the big-touch UX from the Phase-3 attendant console.

- [ ] Installable PWA shell with role-aware navigation for attendant, supervisor, station manager, delivery receiver, auditor
- [ ] Mobile workflows reusing existing endpoints: open shift, view assigned pump, enter meter readings, enter tank dips, submit cash, record expenses, approve shift
- [ ] Big buttons, large numbers, minimal typing, clear status, step-by-step flows
- [ ] Permission `mobile.use` (or reuse existing role permissions); responsive down to small phones

**Done when:** An attendant installs the PWA, opens their shift, captures readings, and submits cash from a phone — all against the live API, with the same authority checks as the web app.

---

### Stage 14 — Offline storage & capture

**Goal:** Field work continues without connectivity, queued locally and safely.

- [ ] Encrypted local store with an **append-only operation log** (open shift, readings, dips, cash, delivery capture, expenses, photos)
- [ ] Each queued op carries a client-generated idempotency key + device identity + local timestamp
- [ ] Clear offline UX: what's captured, what's pending sync, what failed
- [ ] No destructive offline edits — corrections are new ops (supersede), mirroring the server discipline
- [ ] Local data is wiped on logout/device deregistration

**Done when:** With the network disabled, an attendant captures a full shift's readings + cash; the ops persist locally as an ordered, encrypted queue with idempotency keys.

---

### Stage 15 — Sync engine & conflict resolution

**Goal:** Queued offline work syncs to the server as source of truth, with conflicts preserved and resolved by a human.

- [ ] Background sync: replay the local op-log to the server with idempotency (no double-posting on retry)
- [ ] Server-side validation re-runs every business rule (offline never bypasses authority/precision/lifecycle checks)
- [ ] Conflict detection (e.g. two devices edited the same reading): **preserve both**, show who entered what, require supervisor resolution, apply only the approved value, write an audit entry
- [ ] `offline_devices` + `sync_events`; a sync-status indicator + retry system
- [ ] Offline audit trail merges into the central audit log on sync

**Done when:** Two attendants edit the same reading offline; on sync the system keeps both, blocks silent overwrite, and a supervisor picks the winner — leaving a full audit trail.

---

### Stage 16 — Field workflows (fleet auth, document capture, mobile approvals)

**Goal:** The field-specific actions that only make sense on a device.

- [ ] QR/RFID fleet fueling authorization (Phase-8 credit/fleet) from the device, online or queued offline
- [ ] Delivery-note photo capture attached to the Phase-5 goods receipt (uploaded on sync to object storage)
- [ ] Mobile approval queue for supervisors/managers (approve shifts, resolve exceptions) with offline queuing
- [ ] Push notifications for alerts/approvals; device registration + push token management

**Done when:** A receiver photographs a delivery note that attaches to the goods receipt on sync, and a supervisor approves a shift from their phone — both surviving an offline-then-sync cycle.

---

## Category E — Enterprise Hardening (Phase 15)

Make all of it fast, observable, recoverable, and compliant.

### Stage 17 — Observability & SLOs

**Goal:** Every service is traceable and measured against explicit reliability targets.

- [ ] OpenTelemetry traces, metrics, and structured logs across API, projectors, sync, and integration workers, correlated by request/trace id
- [ ] Dashboards for the golden signals (latency, traffic, errors, saturation) per service + per integration
- [ ] Defined SLOs + error budgets for the critical paths (login, shift close, reconciliation, sync); alerting when budgets burn
- [ ] Business-health panels (projection lag, outbox depth, webhook failure rate, sync backlog)

**Done when:** A latency regression on shift-close shows up on a dashboard, trips an SLO-burn alert, and is traceable end-to-end to the slow span.

---

### Stage 18 — Reliability: backups & disaster recovery

**Goal:** No single failure loses data, and recovery is rehearsed.

- [ ] Automated Postgres backups + point-in-time recovery; ClickHouse + object-storage backup policy
- [ ] Documented, **tested** DR runbooks with target RPO/RTO; periodic restore drills
- [ ] Graceful degradation: the transactional core stays up when analytics/AI/integrations are down (they're additive)
- [ ] Multi-AZ readiness for the datastores; outbox guarantees no event loss across failover

**Done when:** A restore drill rebuilds the database to a point in time within the stated RPO/RTO, and a simulated ClickHouse/AI outage leaves shift-close and reconciliation fully working.

---

### Stage 19 — Performance & scale

**Goal:** The platform holds up at national-chain volume.

- [ ] Load tests modeling many stations × shifts × readings × sync at peak; published throughput/latency budgets
- [ ] Query + index optimization; heavy reporting offloaded to ClickHouse; read replicas where needed
- [ ] Caching strategy (Redis) for hot reads with correct invalidation; pagination/streaming for large lists
- [ ] Autoscaling-ready (stateless services, k8s-friendly); connection-pool + backpressure tuning

**Done when:** A load test at target chain scale meets the latency budget for the critical paths with headroom, and reporting queries stay fast under concurrent load.

---

### Stage 20 — Security & compliance hardening

**Goal:** The system meets the bar of a serious financial platform and proves it.

- [ ] Security review + penetration-test remediation; secret management + rotation; least-privilege service accounts
- [ ] Data retention + PII policy enforcement; encryption at rest and in transit verified end to end
- [ ] Period & record locking enforced platform-wide: closed days/financial periods are immutable, reopening requires authorized approval and leaves an audit trail
- [ ] Compliance + advanced-audit exports (record edit history, price changes, stock adjustments, approval logs, user activity, suspicious actions) in the required formats
- [ ] Admin configuration center: integrations, retention, security settings, API keys, audit settings

**Done when:** A penetration test finds no critical/high issues open, a closed financial period rejects edits without an authorized reopen, and an auditor can export a complete, tamper-evident activity report.

---

## Acceptance criteria (Phases 11–15)

Complete when **all** of the following are true:

1. Domain events project into ClickHouse and reconcile exactly to the Postgres ledger; analytics never block transactional writes.
2. Every tank has a reproducible demand forecast + stockout prediction that feeds one-click Phase-5 reorder drafts.
3. A safe rule engine fires alerts, escalations, and scheduled reports automatically, all audited.
4. The AI assistant answers in plain language, strictly within the user's permissions, citing the records it used, recommending — never executing — actions, with every query logged.
5. The adapter framework connects pumps, tank gauges, POS, mobile money, banks, and accounting; outbound webhooks + a public API are signed, rate-limited, and reliably delivered.
6. Field roles run shifts, readings, cash, fleet auth, and approvals from a mobile PWA — offline-capable, with idempotent sync and human conflict resolution.
7. The platform is observable against SLOs, backed up with tested DR, load-proven at chain scale, security-hardened, and compliance-ready with enforced period/record locking.

---

## Out of scope (intentionally)

- **New transactional source-of-truth behavior** — these phases are additive; they read the ledgers/events Phases 1–10 own. Any new write path belongs to its originating phase.
- **Training bespoke ML/LLM models on tenant data** — forecasting uses proven statistical/time-series methods; the assistant uses a hosted model with retrieval, not fine-tuning on customer data.
- **Native app-store binaries** — Phase 14 ships a PWA; packaged native apps (if ever needed) are a later distribution effort.
- **Vendor-specific certifications** (card-scheme PCI attestation, fiscal-authority device certification) — Phase 15 builds to the requirements; formal certification is an external program.
- **Autonomous AI actions** — the assistant never mutates state on its own; that line does not move.

---

## Cross-phase considerations

- **Everything here leans on the outbox + audit foundation** from Phase 1. Forecasts, AI evidence, webhooks, and offline sync are only trustworthy because every prior phase emitted clean, audited events.
- **The policy layer is the single chokepoint** for AI and API access — reusing it (not re-implementing it) is what keeps scope/permission guarantees intact across surfaces.
- **Derived snapshots stay reproducible**: forecasts, insights, and analytics can always be rebuilt from source. If they drift from the ledger, the ledger wins.
- **Offline reuses "supersede, never overwrite"** from Phase 3 — the same immutability discipline that made readings auditable makes offline sync safe.
- **Hardening is continuous, not final**: Phase 15 formalizes SLOs, DR, and locking, but every earlier phase already shipped audited, permission-gated, period-aware writes — Phase 15 raises the floor, it doesn't retrofit the foundation.
