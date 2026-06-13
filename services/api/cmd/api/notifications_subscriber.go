package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/email"
	"github.com/japharyroman/fuelgrid-os/internal/events"
	"github.com/japharyroman/fuelgrid-os/internal/identity/repo"
	"github.com/japharyroman/fuelgrid-os/internal/notifications"
)

// notifSpec describes the notification to raise for a given domain event. title
// and body are built from the event so the feed entry is self-explanatory.
type notifSpec struct {
	notifType string
	title     string
	body      string
	severity  string
}

// notifTarget is one notification an event should raise: the spec plus its
// audience. A nil userID is tenant-wide (every user in the tenant sees it);
// a set userID targets exactly that user's feed (Mobile Attendant Phase 7).
type notifTarget struct {
	spec   notifSpec
	userID *uuid.UUID
}

// subscribeNotifications wires the in-app notification subscriber onto the
// event bus. It subscribes to the operator-facing events (revenue recognized,
// shift closed, risk detection run, incident opened, approval requested) and
// the per-attendant workflow events (nozzle assigned/unassigned, closing
// reading corrected, collection receipt recorded, shift approved) and
// writes one notification row per target — tenant-wide for the operator feed,
// user-targeted for the attendant ones. Critical-severity notifications also
// fan out to the tenant's active users by email (best-effort).
//
// Delivery is at-least-once (a failed handler leaves the outbox event
// unpublished and every handler re-runs on the next tick), so each row is
// deduplicated on (tenant, source event id, target) via CreateFromEvent —
// a redelivery returns the existing row instead of double-creating, and the
// email fan-out is skipped for rows that already existed.
//
// Notifications are written with the OWNER pool (deps.DB) because the
// subscriber runs in the background outside any request, exactly like the
// revenue-journal consumer — so there is no RLS session to scope the insert.
func subscribeNotifications(
	bus events.Bus,
	notifRepo *notifications.Repo,
	userRepo *repo.UserRepo,
	sender email.Sender,
	logger *slog.Logger,
) {
	handler := func(ctx context.Context, e events.Event) error {
		if e.TenantID == nil {
			return nil // platform-level event, no tenant feed to write to
		}
		for _, target := range notifTargetsFor(e) {
			relType := e.AggregateType
			relID := e.AggregateID
			created, fresh, err := notifRepo.CreateFromEvent(ctx, *e.TenantID, e.ID, notifications.CreateInput{
				UserID:            target.userID,
				Type:              target.spec.notifType,
				Title:             target.spec.title,
				Body:              target.spec.body,
				Severity:          target.spec.severity,
				RelatedEntityType: strPtr(relType),
				RelatedEntityID:   strPtr(relID),
			})
			if err != nil {
				// Returning the error leaves the outbox event unpublished so the
				// publisher retries it on the next tick (at-least-once). Rows
				// already created by this attempt are protected by the dedupe.
				return err
			}
			if !fresh {
				continue // outbox redelivery — the row (and any email) already went out
			}
			logger.Info("notification created",
				"event_type", e.Type, "notification_id", created.ID,
				"severity", target.spec.severity, "targeted", target.userID != nil)

			if target.spec.severity == notifications.SeverityCritical {
				emailTenantUsers(ctx, userRepo, sender, *e.TenantID, target.spec, logger)
			}
		}
		return nil
	}

	// One handler per event type of interest. The mapping (event ->
	// notifTargets) lives in notifTargetsFor so it can be unit-tested in
	// isolation without a database or the bus.
	for _, et := range subscribedEventTypes {
		bus.Subscribe(et, handler)
	}
}

// subscribedEventTypes is the set of domain events that raise an in-app
// notification. It is the single source of truth the wiring loops over and the
// mapping test enumerates, so the two can never drift.
var subscribedEventTypes = []string{
	"RevenueRecognized",
	"ShiftClosed",
	"RiskDetectionRun",
	"IncidentOpened",
	"ApprovalRequested",
	"ReadingVerificationCorrected",
	"ShiftNozzleAssigned",
	"ShiftNozzleUnassigned",
	"CashCollectionConfirmed",
	"ShiftApproved",
}

// notifTargetsFor maps a domain event to the notification(s) it should raise.
// It is a pure function (no DB, no bus) so the event->notification contract is
// unit tested directly. An unmapped event type (or a mapped one whose payload
// carries no resolvable target) returns no targets, so it is a silent no-op
// rather than a bogus feed entry.
func notifTargetsFor(e events.Event) []notifTarget {
	switch e.Type {
	case "ReadingVerificationCorrected":
		return readingVerificationTargets(e)
	case "ShiftNozzleAssigned":
		return nozzleAssignedTargets(e)
	case "ShiftNozzleUnassigned":
		return nozzleUnassignedTargets(e)
	case "CashCollectionConfirmed":
		return collectionReceiptTargets(e)
	case "ShiftApproved":
		return shiftApprovedTargets(e)
	default:
		spec, ok := notifSpecFor(e)
		if !ok {
			return nil
		}
		return []notifTarget{{spec: spec}} // tenant-wide
	}
}

// notifSpecFor maps the tenant-wide operator events to their notification.
// The bool is false for an event type that raises no tenant-wide notification.
func notifSpecFor(e events.Event) (notifSpec, bool) {
	switch e.Type {
	case "RevenueRecognized":
		// Informational confirmation that a shift's sales were recognized.
		return notifSpec{
			notifType: "revenue.recognized",
			title:     "Revenue recognized",
			body:      "A shift's sales have been recognized into revenue.",
			severity:  notifications.SeverityInfo,
		}, true
	case "ShiftClosed":
		// Warning when the close carried a cash variance, else a success notice.
		sev := notifications.SeveritySuccess
		body := "A shift has been closed."
		if shiftClosedHasVariance(e.Payload) {
			sev = notifications.SeverityWarning
			body = "A shift was closed with a cash variance — review the close summary."
		}
		return notifSpec{
			notifType: "shift.closed",
			title:     "Shift closed",
			body:      body,
			severity:  sev,
		}, true
	case "RiskDetectionRun":
		// A detection run produced alerts. Critical so it both shows in the feed
		// and emails the tenant's users.
		return notifSpec{
			notifType: "risk.alert_raised",
			title:     "Risk alert raised",
			body:      "Risk detection raised one or more alerts — review the risk queue.",
			severity:  notifications.SeverityCritical,
		}, true
	case "IncidentOpened":
		// Operational issue logged (including attendant self-reports, Phase 7).
		// Critical: the supervisor-facing feed entry plus the email fan-out.
		return notifSpec{
			notifType: "incident.opened",
			title:     "Incident opened",
			body:      "An incident was opened — check the incidents queue.",
			severity:  notifications.SeverityCritical,
		}, true
	case "ApprovalRequested":
		// Something needs a decision. Warning severity.
		return notifSpec{
			notifType: "approval.requested",
			title:     "Approval requested",
			body:      "An approval request is awaiting your decision.",
			severity:  notifications.SeverityWarning,
		}, true
	default:
		return notifSpec{}, false
	}
}

// --- per-attendant mappings (Mobile Attendant Phase 7) ---

// readingVerificationTargets notifies the RECORDER of a closing reading that a
// supervisor corrected it — PRD §7.8 "notify attendant of supervisor decision".
// The recorder rides the event payload's recorded_by (added additively in Phase
// 7); an old-shaped payload without it falls back to the Phase 3 tenant-wide
// entry so in-flight events still surface.
//
// Only the "corrected" decision is wired: the verification write path emits
// ReadingVerificationApproved and ReadingVerificationCorrected, and there is no
// producer of a "rejected" decision today. A rejection mapping is intentionally
// NOT subscribed (it would be dead wiring giving false confidence) — when a
// rejection write path is added it must both emit the event AND register it in
// subscribedEventTypes.
func readingVerificationTargets(e events.Event) []notifTarget {
	var p struct {
		RecordedBy   *uuid.UUID `json:"recorded_by"`
		Verification struct {
			AttendantSubmittedReading string  `json:"attendant_submitted_reading"`
			FinalApprovedReading      string  `json:"final_approved_reading"`
			Reason                    *string `json:"reason"`
		} `json:"verification"`
	}
	_ = json.Unmarshal(e.Payload, &p)

	body := "A supervisor corrected your submitted closing meter reading"
	if p.Verification.AttendantSubmittedReading != "" && p.Verification.FinalApprovedReading != "" {
		body += ": submitted " + p.Verification.AttendantSubmittedReading +
			", final " + p.Verification.FinalApprovedReading
	}
	body += "."
	if p.Verification.Reason != nil && *p.Verification.Reason != "" {
		body += " Reason: " + *p.Verification.Reason + "."
	}
	body += " Check the review status of your shift readings."

	spec := notifSpec{
		notifType: "reading.corrected",
		title:     "Closing reading corrected",
		body:      body,
		severity:  notifications.SeverityWarning,
	}
	if p.RecordedBy == nil || *p.RecordedBy == uuid.Nil {
		// Old payload shape (pre-Phase 7) — keep the tenant-wide behaviour so
		// the decision is still visible rather than silently dropped.
		return []notifTarget{{spec: spec}}
	}
	return []notifTarget{{spec: spec, userID: p.RecordedBy}}
}

// nozzleAssignedTargets notifies the attendant a nozzle was assigned to (PRD
// §7.4 "assignment changes must notify the attendant"). The payload is the
// nozzle-assignment DTO, which has always carried attendant_id.
func nozzleAssignedTargets(e events.Event) []notifTarget {
	var p struct {
		AttendantID *uuid.UUID `json:"attendant_id"`
	}
	_ = json.Unmarshal(e.Payload, &p)
	if p.AttendantID == nil || *p.AttendantID == uuid.Nil {
		return nil // not resolvable — a personal notification has no fallback audience
	}
	return []notifTarget{{
		spec: notifSpec{
			notifType: "assignment.created",
			title:     "Nozzle assigned to you",
			body:      "You have been assigned a nozzle for your shift — review and confirm it on My Shift.",
			severity:  notifications.SeverityInfo,
		},
		userID: p.AttendantID,
	}}
}

// nozzleUnassignedTargets notifies the attendant whose nozzle assignment was
// removed (a reassignment is delete + recreate, so this is the "assignment
// changed" half). The attendant id was added to the event payload additively
// in Phase 7; older events carried no payload and resolve no target.
func nozzleUnassignedTargets(e events.Event) []notifTarget {
	var p struct {
		AttendantID *uuid.UUID `json:"attendant_id"`
	}
	_ = json.Unmarshal(e.Payload, &p)
	if p.AttendantID == nil || *p.AttendantID == uuid.Nil {
		return nil
	}
	return []notifTarget{{
		spec: notifSpec{
			notifType: "assignment.changed",
			title:     "Nozzle assignment changed",
			body:      "A nozzle assignment was removed from you for your shift — check My Shift for your current assignments.",
			severity:  notifications.SeverityInfo,
		},
		userID: p.AttendantID,
	}}
}

// collectionReceiptTargets notifies the SUBMITTER of a cash submission that a
// supervisor recorded the collection receipt: received vs expected, the
// shortage/excess with reason when they differ (warning severity), a plain
// confirmation otherwise. The submitter rides the payload's submitted_by
// (added additively in Phase 7).
func collectionReceiptTargets(e events.Event) []notifTarget {
	var p struct {
		SubmittedBy             *uuid.UUID `json:"submitted_by"`
		ExpectedAmount          string     `json:"expected_amount"`
		SupervisorReceivedTotal string     `json:"supervisor_received_total"`
		Difference              string     `json:"difference"`
		Status                  string     `json:"status"`
		Reason                  *string    `json:"reason"`
	}
	_ = json.Unmarshal(e.Payload, &p)
	if p.SubmittedBy == nil || *p.SubmittedBy == uuid.Nil {
		return nil
	}

	body := "A supervisor recorded your cash handover: received " +
		p.SupervisorReceivedTotal + " against expected " + p.ExpectedAmount + "."
	severity := notifications.SeveritySuccess
	switch {
	case p.Status == "rejected":
		severity = notifications.SeverityWarning
		body += " The handover was rejected."
	case decimalIsNegative(p.Difference):
		severity = notifications.SeverityWarning
		body += " Shortage of " + strings.TrimPrefix(p.Difference, "-") + "."
	case !decimalIsZero(p.Difference):
		severity = notifications.SeverityWarning
		body += " Excess of " + p.Difference + "."
	}
	if p.Reason != nil && *p.Reason != "" {
		body += " Reason: " + *p.Reason + "."
	}

	return []notifTarget{{
		spec: notifSpec{
			notifType: "collection.receipt_recorded",
			title:     "Collection receipt recorded",
			body:      body,
			severity:  severity,
		},
		userID: p.SubmittedBy,
	}}
}

// shiftApprovedTargets notifies every attendant who checked in to the approved
// shift that it is finalized. The ids ride the payload's
// checked_in_attendant_ids (added additively in Phase 7); a shift approved
// before anyone checked in (or an old-shaped payload) resolves no targets.
func shiftApprovedTargets(e events.Event) []notifTarget {
	var p struct {
		CheckedInAttendantIDs []uuid.UUID `json:"checked_in_attendant_ids"`
	}
	_ = json.Unmarshal(e.Payload, &p)
	out := make([]notifTarget, 0, len(p.CheckedInAttendantIDs))
	seen := map[uuid.UUID]bool{}
	for i := range p.CheckedInAttendantIDs {
		id := p.CheckedInAttendantIDs[i]
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		uid := id
		out = append(out, notifTarget{
			spec: notifSpec{
				notifType: "shift.approved",
				title:     "Shift approved",
				body:      "Your shift has been approved and finalized — readings and collections are confirmed.",
				severity:  notifications.SeveritySuccess,
			},
			userID: &uid,
		})
	}
	return out
}

// decimalIsZero reports whether a decimal string is zero (or absent). It only
// inspects the digits — no float parse — so "0", "0.00", "0.0000" all match.
func decimalIsZero(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		switch r {
		case '0', '.', '-', '+':
			// still zero-compatible
		default:
			return false
		}
	}
	return true
}

// decimalIsNegative reports whether a decimal string is strictly negative.
func decimalIsNegative(s string) bool {
	return strings.HasPrefix(s, "-") && !decimalIsZero(s)
}

// emailTenantUsers sends a notification email to every active user in the
// tenant. Best-effort: a user lookup or send failure is logged and swallowed so
// it never leaves the outbox event unpublished (the notification row already
// landed).
func emailTenantUsers(
	ctx context.Context,
	userRepo *repo.UserRepo,
	sender email.Sender,
	tenantID uuid.UUID,
	spec notifSpec,
	logger *slog.Logger,
) {
	if sender == nil || userRepo == nil {
		return
	}
	users, err := userRepo.List(ctx, tenantID)
	if err != nil {
		logger.Warn("notification email: list tenant users", "error", err)
		return
	}
	for _, u := range users {
		if u.Status != "active" || u.Email == "" {
			continue
		}
		if serr := sender.Send(ctx, email.Message{
			To:      u.Email,
			Subject: "[FuelGrid OS] " + spec.title,
			Body:    spec.body,
		}); serr != nil {
			logger.Warn("notification email not delivered", "to", u.Email, "error", serr)
		}
	}
}

// strPtr returns a pointer to s, or nil when s is empty (so an absent
// aggregate type/id stores as NULL rather than "").
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// shiftClosedHasVariance reports whether a ShiftClosed event payload indicates a
// cash variance. The close handler records expected_cash in the payload; a
// non-empty, non-zero expected figure means there is cash to reconcile, which
// the feed flags as a warning to review. Parsing failures default to false so a
// shape change can never crash the subscriber.
func shiftClosedHasVariance(payload json.RawMessage) bool {
	if len(payload) == 0 {
		return false
	}
	var p struct {
		ExpectedCash string `json:"expected_cash"`
		Variance     string `json:"variance"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return false
	}
	switch p.Variance {
	case "", "0", "0.00", "0.0000":
		// fall through to expected-cash heuristic
	default:
		return true
	}
	switch p.ExpectedCash {
	case "", "0", "0.00", "0.0000":
		return false
	default:
		return true
	}
}
