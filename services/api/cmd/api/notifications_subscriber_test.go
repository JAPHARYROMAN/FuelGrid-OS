package main

import (
	"encoding/json"
	"testing"

	"github.com/japharyroman/fuelgrid-os/internal/events"
	"github.com/japharyroman/fuelgrid-os/internal/notifications"
)

// TestNotifSpecFor pins the event->notification mapping: every subscribed event
// type produces the expected notification type and severity, and an unknown
// type is a no-op. This is the subscriber's core contract — change it and the
// feed (and the critical-severity email fan-out) changes.
func TestNotifSpecFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		eventType    string
		payload      string
		wantType     string
		wantSeverity string
	}{
		{"RevenueRecognized", "", "revenue.recognized", notifications.SeverityInfo},
		{"ShiftClosed", "", "shift.closed", notifications.SeveritySuccess},
		{"ShiftClosed", `{"expected_cash":"1500.00"}`, "shift.closed", notifications.SeverityWarning},
		{"RiskDetectionRun", "", "risk.alert_raised", notifications.SeverityCritical},
		{"IncidentOpened", "", "incident.opened", notifications.SeverityCritical},
		{"ApprovalRequested", "", "approval.requested", notifications.SeverityWarning},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.eventType+"/"+tc.wantSeverity, func(t *testing.T) {
			t.Parallel()
			spec, ok := notifSpecFor(events.Event{Type: tc.eventType, Payload: json.RawMessage(tc.payload)})
			if !ok {
				t.Fatalf("notifSpecFor(%q) returned ok=false, want a spec", tc.eventType)
			}
			if spec.notifType != tc.wantType {
				t.Errorf("notifType = %q, want %q", spec.notifType, tc.wantType)
			}
			if spec.severity != tc.wantSeverity {
				t.Errorf("severity = %q, want %q", spec.severity, tc.wantSeverity)
			}
			if spec.title == "" || spec.body == "" {
				t.Errorf("title/body must be non-empty: title=%q body=%q", spec.title, spec.body)
			}
		})
	}
}

// TestNotifSpecForUnknownEvent ensures an event type the subscriber does not map
// is a silent no-op (ok=false), never a bogus feed entry.
func TestNotifSpecForUnknownEvent(t *testing.T) {
	t.Parallel()
	if _, ok := notifSpecFor(events.Event{Type: "SomethingElseHappened"}); ok {
		t.Fatal("notifSpecFor returned ok=true for an unmapped event type")
	}
}

// TestSubscribedEventTypesAllMap guards against the wiring list and the mapping
// drifting apart: every type the subscriber registers must produce a spec.
func TestSubscribedEventTypesAllMap(t *testing.T) {
	t.Parallel()
	for _, et := range subscribedEventTypes {
		if _, ok := notifSpecFor(events.Event{Type: et}); !ok {
			t.Errorf("subscribed event %q has no mapping in notifSpecFor", et)
		}
	}
}

func TestShiftClosedHasVariance(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		payload string
		want    bool
	}{
		{"empty payload", "", false},
		{"zero expected, no variance", `{"expected_cash":"0.00"}`, false},
		{"non-zero expected", `{"expected_cash":"1500.00"}`, true},
		{"explicit non-zero variance", `{"expected_cash":"0.00","variance":"25.00"}`, true},
		{"explicit zero variance", `{"variance":"0.00","expected_cash":"0"}`, false},
		{"garbage", `not json`, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := shiftClosedHasVariance(json.RawMessage(tc.payload))
			if got != tc.want {
				t.Fatalf("shiftClosedHasVariance(%q) = %v, want %v", tc.payload, got, tc.want)
			}
		})
	}
}
