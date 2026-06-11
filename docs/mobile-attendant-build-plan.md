# Mobile Attendant App — Gap Analysis & Build Plan

Companion to [mobile-attendant-app-prd.md](mobile-attendant-app-prd.md). Reconciled against live code on 2026-06-11 (main @ d9f14c2). Source-of-truth hierarchy applies: code > migrations > tests > OpenAPI > SDK > routes > docs.

## Delivery model

The attendant app is a **mobile-first PWA route group inside apps/web** — an extension of the main app, not a separate codebase. It reuses the httpOnly-cookie BFF auth, @fuelgrid/sdk, and the design system, and ships with the existing deploy. Attendants install it by scanning a **QR code on the main app's landing/login page**. Supervisor-side actions (verify readings, confirm collections) live in the existing desktop surfaces; the backend workflows below serve both.

## Gap analysis summary

| PRD capability | Status | Where / what's missing |
| --- | --- | --- |
| Shift lifecycle (open/close/approve, backdated) | EXISTS | 0015; shifts_handlers.go; backdated workflow @ d9f14c2 |
| 3-team rotation, attendants auto-populated on open | EXISTS | 0077 workforce; shift_attendants |
| Nozzle→attendant assignment per shift | EXISTS | 0015 shift_nozzle_assignments; assign/unassign endpoints |
| Meter readings (opening/closing, rollback + scale validation, correction chain) | EXISTS | 0016; internal/readings/meter.go; supersedes_id |
| Shift close snapshot (litres × price = expected) + cash submission + variance + exceptions | EXISTS | 0018 shift_close_lines, cash_submissions, shift_exceptions |
| Notifications feed + preferences | EXISTS | 0078, 0092; no shift-event publishers yet |
| Incidents (issue reporting backbone) | EXISTS | 0013; not attendant-self-service yet |
| Active price per station/product | EXISTS | 0044 price_changes (close uses nozzle default_price — note) |
| **Attendance / check-in / roll call** | **MISSING** | no table, no endpoints — Phase 0 |
| **Attendant assignment confirmation** | **MISSING** | no confirm step — Phase 0 |
| **Supervisor reading verification (dual-value: submitted / verified / final, originals preserved)** | **MISSING** | correction chain ≠ approval workflow — Phase 0 |
| **Supervisor collection confirmation (received amount, shortage/excess + reason)** | **MISSING** | submission exists, receipt doesn't — Phase 0 |
| **Handover chain (next opening from previous final approved closing; next-shift lock)** | **MISSING** | Phase 0 |
| "Today's assigned shift" for an attendant (rotation-aware) | PARTIAL | ActiveShiftForAttendant returns latest, not roster — Phase 1 |
| Mobile attendant UX (guided wizard, large controls) | PARTIAL | my-shift page approximates it; full flow is Phase 1–4 |
| QR install on login page; PWA manifest | PARTIAL | manifest.ts exists; QR + install affordance Phase 1 |
| Offline queue + sync + conflicts; service worker | MISSING | Phase 6 |
| i18n (English/Swahili), large-text/high-contrast modes | MISSING | Phase 6 |
| Notification publishers for shift events; reports feeds | MISSING | Phase 7 |

## Build sequencing

Backend foundations come first because every screen depends on them; the mobile UI then composes them phase by phase.

- **Phase 0 — Backend workflow foundations** _(in progress, branch `feat/mobile-attendant-phase0`)_: shift_attendance check-in/out (self-scoped); nozzle-assignment `confirmed_at` + confirm endpoint; `reading_verifications` dual-value model (batch approve / verify-correct with reason, SoD verifier ≠ recorder, shift approval gated on verification, close-line recompute on correction); `collection_receipts` (received amount, difference, reason required on shortage/excess, SoD receiver ≠ submitter, approval gated on receipt); handover chain (next-shift open blocked while previous closed-but-unapproved, override audited; expected-opening-readings endpoint derived from previous final approved closings; opening capture rejected below expected). Migrations 0098+ (0096/0097 reserved by other in-flight work).
- **Phase 1 — Mobile shell + entry**: `/attendant` mobile-first route group (login reuse, My Shift home with next-action driver, attendance screen); QR code + install affordance on the main login/landing page; rotation-aware "today's shift" endpoint.
- **Phase 2 — Opening flow**: assignment view + confirm; opening verification screen against expected readings; readiness checklist; open shift.
- **Phase 3 — Closing + review**: closing readings wizard with live litres calculation; submission lock; supervisor review status screen (approved / corrected with both values + reason / rejected).
- **Phase 4 — Collections**: expected collection display (per nozzle/product basis); submit collections with difference + reason; supervisor receipt status; shift-complete screen.
- **Phase 5 — Handover enforcement UX**: completion states, check-out, next-shift unlock visibility; supervisor-side surfaces for verification/receipt queues.
- **Phase 6 — Offline + accessibility hardening**: service worker, offline action queue with idempotency keys, sync/conflict states; en/sw i18n; large-text + high-contrast modes; low-end device pass.
- **Phase 7 — Notifications + reporting integration**: outbox-driven notification publishers for shift events (assignment, correction, receipt, completion); attendance/variance/correction report feeds.

## Standing constraints

- **No AI** — deterministic rules, validations, approvals, calculations only.
- Decimal strings end-to-end; SQL `::numeric` arithmetic; no float on money/litres.
- Every new tenant table: RLS enabled + tenant_isolation policy; audit + outbox in the same transaction; originals never mutated (corrections are new rows).
- SoD throughout: verifier ≠ recorder, receiver ≠ submitter.
- Migration numbers 0096 and 0097 are reserved by other in-flight pipelines; mobile work starts at 0098.
- Attendant endpoints are self-scoped (membership-checked), never granted station-wide read.
