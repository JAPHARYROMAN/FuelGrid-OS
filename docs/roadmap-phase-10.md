# Phase 10 - Risk, Fraud & Intelligence

The phase where FuelGrid OS becomes a risk brain for fuel operations. Earlier phases capture the facts: readings, inventory movements, procurement, deliveries, sales, payments, finance, customer credit, fleet fueling, and enterprise approvals. Phase 10 turns those facts into alerts, risk scores, investigations, and recommended actions.

Phase 10 starts with deterministic rules and explainable scoring. It should not make irreversible business decisions by itself. It identifies risk, shows evidence, routes investigations, and recommends action. Automation, forecasting, and AI copilots come later.

## Stack decisions carried forward

All Phase-10 work continues the patterns locked in earlier phases:

| Concern | Continued choice |
|---|---|
| Backend transactions | One tx wraps the business change, audit entry, and outbox event |
| Tenant scoping | Every repo query takes `tenantID` first; RLS remains the safety net |
| Tenant-bound FKs | Children carry `(tenant_id, ...)` composite FKs onto parent unique keys |
| Authorization | Risk visibility follows station, region, company, tenant, and investigation assignment scope |
| Numeric precision | Money, litres, rates, and score components keep decimal discipline; no accounting values in floating point |
| Evidence | Risk alerts and scores must link back to immutable source facts, not copied narrative alone |
| Corrections | Closing an alert or case never rewrites the source operational event; it records a disposition |
| Frontend | shadcn-style primitives in `@fuelgrid/ui`; TanStack Query over the hand-written `@fuelgrid/sdk` |

New conventions specific to Phase 10:

| Concern | Convention |
|---|---|
| Explainability | Every alert must say which rule fired, what data triggered it, severity, score contribution, and suggested next step. |
| Detection packs | Fuel loss, cash shortage, delivery discrepancy, suspicious edit, attendant pattern, supplier pattern, customer credit pattern, and station score detections are versioned packs. |
| Alert lifecycle | Alerts move `open -> acknowledged -> investigating -> resolved -> dismissed`; severe alerts can be escalated. |
| Case lifecycle | Investigations move `open -> assigned -> in_review -> action_required -> resolved -> closed`. |
| Risk score | Scores are derived from alert history, recent unresolved issues, severity, recurrence, and operational exposure. Scores are advisory, not punitive automation. |
| Feedback | Every dismissal/resolution captures a disposition so rule tuning can reduce noise. |

---

## Category A - Risk data foundation

The normalized signal layer that makes detection explainable.

### Stage 1 - Risk signal registry

**Goal:** Operational events become typed risk signals with stable dimensions.

- [ ] Migration `risk_signal_types` and `risk_signals` with source event/document links, tenant, station, actor, product, supplier, customer, vehicle, driver, amount, litres, score fields, occurred_at, and metadata
- [ ] Signal types for readings, stock movements, reconciliations, deliveries, invoices, payments, expenses, credit authorizations, odometer warnings, approvals, and corrections
- [ ] Idempotent signal ingestion from outbox events and backfill commands
- [ ] Signal retention policy with source references retained even if detailed metadata is archived later
- [ ] Permission `risk_signal.admin` for rebuild/replay tools
- [ ] Audit + outbox: `risk_signal.ingested`, `risk_signal.backfilled`

**Done when:** A historical station day can be backfilled into risk signals without duplicating signals or losing source links.

---

### Stage 2 - Rule engine

**Goal:** Risk rules can be defined, versioned, tested, and executed against normalized signals.

- [ ] Migration `risk_rules`, `risk_rule_versions`, `risk_rule_conditions`, `risk_rule_runs`, and `risk_rule_run_results`
- [ ] Rule types: threshold, trend, recurrence, cross-check, ratio, sequence, missing-event, duplicate-event, and manual-review
- [ ] Rule lifecycle: `draft -> active -> paused -> retired`; version changes never mutate historical runs
- [ ] Dry-run endpoint for a rule over a date range and scoped stations
- [ ] Execution modes: event-triggered for immediate checks and scheduled batch for lookback windows
- [ ] Permissions: `risk_rule.manage`, `risk_rule.activate`, `risk_rule.read`
- [ ] Audit + outbox: `risk_rule.created`, `risk_rule.activated`, `risk_rule.paused`, `risk_rule.retired`

**Done when:** A tenant can activate a fuel-loss threshold rule, dry-run it against historical data, and see which source facts would trigger alerts.

---

### Stage 3 - Alert lifecycle and routing

**Goal:** Rule results become manageable alerts routed to the right people.

- [ ] Migration `risk_alerts`, `risk_alert_events`, `risk_alert_assignments`, and `risk_alert_evidence`
- [ ] Alert lifecycle: `open -> acknowledged -> investigating -> resolved -> dismissed`; `escalated` flag and escalation metadata
- [ ] Severity model: `info`, `low`, `medium`, `high`, `critical`
- [ ] Routing rules by alert type, station, region, severity, amount/litre exposure, and role
- [ ] Dispositions: true issue, data entry error, expected operation, duplicate, false positive, resolved by correction, escalated to case
- [ ] Permissions: `risk_alert.read`, `risk_alert.manage`, `risk_alert.assign`, `risk_alert.resolve`
- [ ] Audit + outbox: `risk_alert.opened`, `risk_alert.acknowledged`, `risk_alert.assigned`, `risk_alert.resolved`, `risk_alert.dismissed`

**Done when:** A rule breach creates one alert with evidence, route, owner, severity, status history, and a disposition when closed.

---

## Category B - Detection packs

The first production-ready risk rules for fuel businesses.

### Stage 4 - Fuel loss detection

**Goal:** Detect suspicious stock loss and unexplained tank variance.

- [ ] Rules over Phase-4 reconciliations: variance litres beyond tolerance, repeated negative variance, sudden dip drop without sale/transfer/delivery, unexplained adjustment, stock movement reversal frequency
- [ ] Compare meter sales, dip readings, delivery receipts, stock movements, and reconciliation adjustments by tank/product/day
- [ ] Severity based on litres, value at landed cost or sale price, recurrence, and unresolved history
- [ ] Evidence pack: opening/closing dip, expected litres, sales litres, receipt litres, variance, actor, station, tank, product, prior variance trend
- [ ] Recommended actions: review dip photos/readings, inspect tank, check meter readings, investigate attendant shift, create incident
- [ ] Audit + outbox: `risk.fuel_loss_detected`

**Done when:** A station with repeated unexplained PMS losses produces explainable alerts with source readings and recommended review steps.

---

### Stage 5 - Cash shortage detection

**Goal:** Detect cash shortages, payment anomalies, and repeated variance patterns.

- [ ] Rules over Phase-3/6/7 cash facts: expected cash vs counted cash, repeated shortage by attendant/shift/station, late cash submission, payment method mix anomaly, void/refund concentration
- [ ] Severity based on amount, recurrence, proximity to prior incidents, and whether finance posted a cash over/short entry
- [ ] Evidence pack: shift close, payment breakdown, cash reconciliation, attendant, supervisor, variance, comments, corrections
- [ ] Recommended actions: review shift close, require supervisor recount, open investigation, adjust cash tolerance, train attendant
- [ ] Alert suppression for approved known events such as documented cash pickup timing
- [ ] Audit + outbox: `risk.cash_shortage_detected`

**Done when:** Repeated small cash shortages by the same attendant or station become visible before they accumulate into a major loss.

---

### Stage 6 - Delivery and procurement discrepancy detection

**Goal:** Detect risky supplier, delivery, and invoice patterns.

- [ ] Rules over Phase-5 procurement: repeated short deliveries, price variance beyond tolerance, invoice quantity above receipt, frequent discrepancy overrides, supplier-specific variance trend, late delivery trend
- [ ] Cross-check PO, goods receipt, tank dip movement, stock movement, supplier invoice, discrepancy resolution, and approver
- [ ] Severity based on litres/value exposure, supplier recurrence, station recurrence, and whether invoice was approved after override
- [ ] Evidence pack: PO, receipt, tank, stock movement, invoice, discrepancy, resolver, approver, supplier history
- [ ] Recommended actions: review supplier performance, require second approver, inspect receiving process, adjust procurement tolerance
- [ ] Audit + outbox: `risk.delivery_discrepancy_detected`

**Done when:** A supplier with recurring short deliveries or price mismatches gets flagged with evidence across PO, receipt, invoice, and approval.

---

### Stage 7 - Suspicious edit and correction detection

**Goal:** Detect risky changes to operational and financial history.

- [ ] Rules over audit logs and correction workflows: late correction, repeated reversals, correction after supervisor approval, manual override frequency, permission escalation before sensitive action, period-close adjustment spike
- [ ] Sensitive domains: readings, shift close, reconciliation, delivery, invoice, payment, journal entry, customer credit override, price rollout
- [ ] Severity based on timing, actor role, affected amount/litres, repeated pattern, and proximity to audit/period close
- [ ] Evidence pack: original record, correction/reversal, actor, approver, reason, timestamp, source IP/session when available
- [ ] Recommended actions: review audit trail, require manager signoff, temporarily tighten workflow permission, open case
- [ ] Audit + outbox: `risk.suspicious_edit_detected`

**Done when:** Late or repeated corrections to sensitive records generate explainable alerts without blocking legitimate correction flows.

---

### Stage 8 - Attendant, customer, supplier, and station patterns

**Goal:** Surface repeated behavior patterns that single-event rules miss.

- [ ] Attendant patterns: repeated cash shortages, high void rate, frequent nozzle exceptions, late shift close, repeated reading corrections
- [ ] Customer/fleet patterns: abnormal consumption spike, repeated credit overrides, odometer inconsistencies, credential sharing indicators
- [ ] Supplier patterns: recurring short delivery, recurring invoice mismatch, unusual price movement, late delivery recurrence
- [ ] Station patterns: high stock loss, high cash variance, overdue closes, frequent approval overrides, exception backlog
- [ ] Pattern window configuration: 7, 14, 30, 90 days with minimum sample sizes to reduce noise
- [ ] Audit + outbox: `risk.pattern_detected`

**Done when:** Risk users can see recurring patterns across people, suppliers, customers, and stations, not just isolated incidents.

---

## Category C - Risk scoring and intelligence views

Turning alerts and patterns into prioritized attention.

### Stage 9 - Risk scoring model

**Goal:** Stations, attendants, suppliers, customers, vehicles, and products receive explainable risk scores.

- [ ] Migration `risk_score_models`, `risk_score_runs`, `risk_scores`, and `risk_score_components`
- [ ] Score dimensions: station, attendant, supplier, customer, vehicle, driver, product, and tenant aggregate
- [ ] Component inputs: unresolved alerts, severity, recurrence, exposure amount/litres, recent trend, override count, correction count, and stale controls
- [ ] Score bands: low, watch, elevated, high, critical
- [ ] Store component breakdown so users can see why a score changed
- [ ] Permissions: `risk_score.read`, `risk_score.admin`
- [ ] Audit + outbox: `risk_score.calculated`, `risk_score.band_changed`

**Done when:** A station risk score can be explained by its top contributing alerts and recent operational patterns.

---

### Stage 10 - Risk dashboard

**Goal:** Risk managers get one workspace for alerts, scores, trends, and urgent action.

- [ ] Route `/risk`: open alerts by severity, risk score leaders, new escalations, unresolved cases, top exposure, station heat map, trend charts
- [ ] Backend `GET /api/v1/risk/overview` with tenant/company/region/station/date filters
- [ ] Drill-down to alert, case, station, attendant, supplier, customer, and source evidence
- [ ] Queue filters: assigned to me, critical, overdue, unassigned, dismissed, resolved
- [ ] Permission gate `risk.read`
- [ ] Mobile responsive review view for owners/managers

**Done when:** A manager can open `/risk`, identify the highest-risk stations or alerts, and drill into evidence and actions.

---

## Category D - Investigation workflow

The workflow for turning alerts into accountable follow-up.

### Stage 11 - Investigation cases

**Goal:** Related alerts and evidence can be grouped into a formal investigation case.

- [ ] Migration `investigation_cases`, `investigation_case_alerts`, `investigation_case_evidence`, `investigation_case_comments`, and `investigation_case_actions`
- [ ] Case lifecycle: `open -> assigned -> in_review -> action_required -> resolved -> closed`
- [ ] Case types: fuel loss, cash shortage, procurement discrepancy, suspicious edit, credit/fleet abuse, station performance, other
- [ ] Assignment to user/team with due date, severity, scope, and confidentiality flag
- [ ] Permissions: `investigation.read`, `investigation.manage`, `investigation.assign`, `investigation.close`
- [ ] Audit + outbox: `investigation.opened`, `investigation.assigned`, `investigation.evidence_added`, `investigation.resolved`, `investigation.closed`

**Done when:** A critical alert can be escalated into a case, assigned, worked with evidence/comments/actions, and closed with a resolution.

---

### Stage 12 - Evidence timeline

**Goal:** Investigators see the full operational timeline around an issue.

- [ ] Timeline builder that pulls source facts from audit, readings, shifts, stock movements, deliveries, invoices, sales, payments, finance, credit, approvals, and alerts
- [ ] Evidence pinning: important source records can be attached to a case with note and actor
- [ ] Immutable evidence references; source correction creates a new timeline event rather than altering prior evidence
- [ ] Export case evidence package with audit metadata and source references
- [ ] Permission `investigation.export`
- [ ] Audit + outbox: `investigation.evidence_exported`

**Done when:** An investigator can reconstruct what happened before, during, and after a risky event without manually opening every module.

---

### Stage 13 - Recommended actions

**Goal:** Alerts and cases provide practical next steps without automating irreversible decisions.

- [ ] Recommendation library by alert type, severity, workflow source, and role
- [ ] Actions: create incident, request recount, require second approval, suspend credential, place customer on hold, pause supplier, adjust tolerance, open training task, create finance adjustment review
- [ ] Action lifecycle: `suggested -> accepted -> completed -> dismissed`
- [ ] Actions call existing domain workflows where available and record source alert/case links
- [ ] Permission checks before action execution; no recommendation bypasses domain authorization
- [ ] Audit + outbox: `risk_recommendation.suggested`, `risk_recommendation.accepted`, `risk_recommendation.completed`, `risk_recommendation.dismissed`

**Done when:** A high-severity cash shortage alert suggests review/recount/investigation actions, and accepted actions are tracked to completion.

---

## Category E - Tuning, governance, and trust

Keeping risk useful and defensible.

### Stage 14 - Rule tuning and suppression

**Goal:** Risk users can reduce noise without hiding important problems.

- [ ] Migration `risk_rule_tuning`, `risk_suppressions`, and `risk_feedback`
- [ ] Tuning controls: threshold, lookback period, recurrence count, severity mapping, station/product/customer/supplier exceptions
- [ ] Suppression scopes: alert type, entity, station, date range, reason, approver, and expiry
- [ ] Feedback capture from alert dispositions and case outcomes
- [ ] Guard: critical system rules require elevated permission to suppress and always retain audit visibility
- [ ] Permissions: `risk_rule.tune`, `risk_alert.suppress`
- [ ] Audit + outbox: `risk_rule.tuned`, `risk_alert.suppressed`, `risk_feedback.recorded`

**Done when:** A noisy rule can be tuned or temporarily suppressed with expiry and reason, while keeping a full audit trail.

---

### Stage 15 - Risk governance and data quality

**Goal:** Risk outputs are trustworthy, explainable, and monitored.

- [ ] Data quality checks: missing signals, stale projections, rule run failures, source-event lag, orphan evidence, unscored active entities
- [ ] Governance dashboard for rule status, alert volume, dismissal rate, false positive rate, unresolved aging, and score distribution
- [ ] Versioned documentation field on each rule explaining purpose, owner, threshold rationale, and expected action
- [ ] Admin endpoint to pause all non-critical rule runs during incident response or data repair
- [ ] Permission `risk_governance.admin`
- [ ] Audit + outbox: `risk_governance.reviewed`, `risk_engine.paused`, `risk_engine.resumed`

**Done when:** Administrators can tell whether the risk engine is healthy, noisy, stale, or missing source data.

---

## Phase 10 acceptance criteria

Phase 10 is complete when all of the following are true:

1. Operational and financial source events become idempotent, source-linked risk signals.
2. Risk rules are versioned, dry-runnable, activatable, pausable, and auditable.
3. Alerts carry severity, rule explanation, evidence, assignment, lifecycle history, and disposition.
4. Detection packs exist for fuel loss, cash shortage, delivery/procurement discrepancy, suspicious edits, and recurring entity patterns.
5. Risk scores are explainable through score components and source alerts.
6. Risk dashboards prioritize attention across station, attendant, supplier, customer, product, and enterprise dimensions.
7. Alerts can escalate into investigation cases with evidence timelines and tracked actions.
8. Recommended actions route users back into existing domain workflows without bypassing permissions.
9. Rules can be tuned or suppressed with expiry, reason, permission, and audit trail.
10. Governance views expose rule health, data quality, alert volume, and false-positive feedback.

---

## Out of scope for Phase 10 intentionally

- Demand forecasting, stockout prediction, and automated replenishment recommendations - Phase 11.
- AI chat, natural-language analytics, generated investigation summaries, and autonomous reasoning assistants - Phase 12.
- Hardware integrations, pump/tank telemetry ingestion, POS adapters, and external fraud feeds - Phase 13.
- Offline mobile investigation workflow - Phase 14.
- Fully automated punitive actions such as firing users, permanently blocking suppliers, or writing finance adjustments without human approval.
- Black-box machine learning scores that cannot explain their source facts and score components.

---

## Cross-phase considerations

- Phase 10 relies on clean source links from Phases 3-9. Do not copy source facts without preserving the original document/event reference.
- Fuel loss rules depend on Phase-4 stock movements and reconciliations staying append-only.
- Delivery discrepancy rules depend on Phase-5 PO, receipt, invoice, discrepancy, and approval links.
- Cash shortage rules depend on Phase-6 payment facts and Phase-7 cash reconciliation postings.
- Customer/fleet risk depends on Phase-8 customer, vehicle, driver, credential, authorization, odometer, and credit hold dimensions.
- Enterprise risk views depend on Phase-9 hierarchy, station rankings, approvals, and projection freshness.
- Phase 11 and Phase 12 can build on risk signals, alerts, and cases, but should not change Phase-10 alert history or score history.

If any of these contracts change, Phase 10 signal ingestion and historical score comparability must be revisited before implementation.
