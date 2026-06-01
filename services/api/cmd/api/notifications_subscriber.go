package main

import (
	"context"
	"encoding/json"
	"log/slog"

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

// subscribeNotifications wires the in-app notification subscriber onto the event
// bus. It subscribes to the operator-facing events (revenue recognized, shift
// closed, risk detection run, incident opened, approval requested) and, for
// each, writes a tenant-wide notification. Critical-severity notifications also
// fan out to the tenant's active users by email (best-effort).
//
// Notifications are written with the OWNER pool (deps.DB) because the subscriber
// runs in the background outside any request, exactly like the revenue-journal
// consumer — so there is no RLS session to scope the insert.
func subscribeNotifications(
	bus events.Bus,
	notifRepo *notifications.Repo,
	userRepo *repo.UserRepo,
	sender email.Sender,
	logger *slog.Logger,
) {
	handle := func(buildSpec func(e events.Event) (notifSpec, bool)) events.Handler {
		return func(ctx context.Context, e events.Event) error {
			if e.TenantID == nil {
				return nil // platform-level event, no tenant feed to write to
			}
			spec, ok := buildSpec(e)
			if !ok {
				return nil
			}
			relType := e.AggregateType
			relID := e.AggregateID
			created, err := notifRepo.Create(ctx, *e.TenantID, notifications.CreateInput{
				// UserID nil => tenant-wide: every user in the tenant sees it.
				Type:              spec.notifType,
				Title:             spec.title,
				Body:              spec.body,
				Severity:          spec.severity,
				RelatedEntityType: strPtr(relType),
				RelatedEntityID:   strPtr(relID),
			})
			if err != nil {
				// Returning the error leaves the outbox event unpublished so the
				// publisher retries it on the next tick (at-least-once).
				return err
			}
			logger.Info("notification created",
				"event_type", e.Type, "notification_id", created.ID, "severity", spec.severity)

			if spec.severity == notifications.SeverityCritical {
				emailTenantUsers(ctx, userRepo, sender, *e.TenantID, spec, logger)
			}
			return nil
		}
	}

	// Revenue recognized — informational confirmation that a shift's sales were
	// recognized into revenue.
	bus.Subscribe("RevenueRecognized", handle(func(e events.Event) (notifSpec, bool) {
		return notifSpec{
			notifType: "revenue.recognized",
			title:     "Revenue recognized",
			body:      "A shift's sales have been recognized into revenue.",
			severity:  notifications.SeverityInfo,
		}, true
	}))

	// Shift closed — surfaced as a warning when the close carried a cash
	// variance, otherwise a plain success notice.
	bus.Subscribe("ShiftClosed", handle(func(e events.Event) (notifSpec, bool) {
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
	}))

	// Risk alert raised — a detection run produced alerts. Critical so it both
	// shows in the feed and emails the tenant's users.
	bus.Subscribe("RiskDetectionRun", handle(func(e events.Event) (notifSpec, bool) {
		return notifSpec{
			notifType: "risk.alert_raised",
			title:     "Risk alert raised",
			body:      "Risk detection raised one or more alerts — review the risk queue.",
			severity:  notifications.SeverityCritical,
		}, true
	}))

	// Incident opened — operational issue logged. Critical.
	bus.Subscribe("IncidentOpened", handle(func(e events.Event) (notifSpec, bool) {
		return notifSpec{
			notifType: "incident.opened",
			title:     "Incident opened",
			body:      "An incident was opened — check the incidents queue.",
			severity:  notifications.SeverityCritical,
		}, true
	}))

	// Approval requested — something needs a decision. Warning severity.
	bus.Subscribe("ApprovalRequested", handle(func(e events.Event) (notifSpec, bool) {
		return notifSpec{
			notifType: "approval.requested",
			title:     "Approval requested",
			body:      "An approval request is awaiting your decision.",
			severity:  notifications.SeverityWarning,
		}, true
	}))
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
