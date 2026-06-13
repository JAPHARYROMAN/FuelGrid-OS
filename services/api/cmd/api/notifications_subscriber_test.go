package main

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/events"
	"github.com/japharyroman/fuelgrid-os/internal/notifications"
)

// TestNotifSpecFor pins the tenant-wide event->notification mapping: every
// operator event produces the expected notification type and severity, and an
// unknown type is a no-op. This is the subscriber's core contract — change it
// and the feed (and the critical-severity email fan-out) changes.
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

// TestNotifTargetsFor pins the per-attendant mappings (Mobile Attendant Phase
// 7): each workflow event resolves the affected attendant from its payload and
// produces a USER-TARGETED notification with the expected type and severity
// (correction/rejection/shortage = warning).
func TestNotifTargetsFor(t *testing.T) {
	t.Parallel()
	attendant := uuid.New()

	cases := []struct {
		name         string
		eventType    string
		payload      string
		wantType     string
		wantSeverity string
		wantUser     *uuid.UUID
	}{
		{
			name:      "nozzle assigned targets the attendant",
			eventType: "ShiftNozzleAssigned",
			payload:   `{"id":"` + uuid.NewString() + `","attendant_id":"` + attendant.String() + `"}`,
			wantType:  "assignment.created", wantSeverity: notifications.SeverityInfo, wantUser: &attendant,
		},
		{
			name:      "nozzle unassigned targets the attendant",
			eventType: "ShiftNozzleUnassigned",
			payload:   `{"attendant_id":"` + attendant.String() + `"}`,
			wantType:  "assignment.changed", wantSeverity: notifications.SeverityInfo, wantUser: &attendant,
		},
		{
			name:      "reading corrected targets the recorder, warning",
			eventType: "ReadingVerificationCorrected",
			payload: `{"recorded_by":"` + attendant.String() + `",` +
				`"verification":{"attendant_submitted_reading":"1500.000","final_approved_reading":"1490.000","reason":"misread"}}`,
			wantType: "reading.corrected", wantSeverity: notifications.SeverityWarning, wantUser: &attendant,
		},
		{
			name:      "reading rejected targets the recorder, warning",
			eventType: "ReadingVerificationRejected",
			payload:   `{"recorded_by":"` + attendant.String() + `","verification":{"reason":"implausible"}}`,
			wantType:  "reading.rejected", wantSeverity: notifications.SeverityWarning, wantUser: &attendant,
		},
		{
			name:      "receipt with shortage targets the submitter, warning",
			eventType: "CashCollectionConfirmed",
			payload: `{"submitted_by":"` + attendant.String() + `","expected_amount":"100000.00",` +
				`"supervisor_received_total":"95000.00","difference":"-5000.00","status":"approved_with_difference","reason":"till short"}`,
			wantType: "collection.receipt_recorded", wantSeverity: notifications.SeverityWarning, wantUser: &attendant,
		},
		{
			name:      "receipt with excess targets the submitter, warning",
			eventType: "CashCollectionConfirmed",
			payload: `{"submitted_by":"` + attendant.String() + `","expected_amount":"100000.00",` +
				`"supervisor_received_total":"101000.00","difference":"1000.00","status":"approved_with_difference","reason":"extra note"}`,
			wantType: "collection.receipt_recorded", wantSeverity: notifications.SeverityWarning, wantUser: &attendant,
		},
		{
			name:      "exact receipt targets the submitter, success",
			eventType: "CashCollectionConfirmed",
			payload: `{"submitted_by":"` + attendant.String() + `","expected_amount":"100000.00",` +
				`"supervisor_received_total":"100000.00","difference":"0.00","status":"received"}`,
			wantType: "collection.receipt_recorded", wantSeverity: notifications.SeveritySuccess, wantUser: &attendant,
		},
		{
			name:      "rejected receipt targets the submitter, warning",
			eventType: "CashCollectionConfirmed",
			payload: `{"submitted_by":"` + attendant.String() + `","expected_amount":"100000.00",` +
				`"supervisor_received_total":"100000.00","difference":"0.00","status":"rejected","reason":"recount"}`,
			wantType: "collection.receipt_recorded", wantSeverity: notifications.SeverityWarning, wantUser: &attendant,
		},
		{
			name:      "shift approved targets each checked-in attendant",
			eventType: "ShiftApproved",
			payload:   `{"checked_in_attendant_ids":["` + attendant.String() + `"]}`,
			wantType:  "shift.approved", wantSeverity: notifications.SeveritySuccess, wantUser: &attendant,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			targets := notifTargetsFor(events.Event{
				ID: uuid.New(), Type: tc.eventType, Payload: json.RawMessage(tc.payload),
			})
			if len(targets) != 1 {
				t.Fatalf("notifTargetsFor(%s) returned %d targets, want 1", tc.eventType, len(targets))
			}
			got := targets[0]
			if got.spec.notifType != tc.wantType {
				t.Errorf("notifType = %q, want %q", got.spec.notifType, tc.wantType)
			}
			if got.spec.severity != tc.wantSeverity {
				t.Errorf("severity = %q, want %q", got.spec.severity, tc.wantSeverity)
			}
			if got.spec.title == "" || got.spec.body == "" {
				t.Errorf("title/body must be non-empty: title=%q body=%q", got.spec.title, got.spec.body)
			}
			switch {
			case tc.wantUser == nil:
				if got.userID != nil {
					t.Errorf("userID = %v, want tenant-wide (nil)", got.userID)
				}
			case got.userID == nil || *got.userID != *tc.wantUser:
				t.Errorf("userID = %v, want %v", got.userID, tc.wantUser)
			}
		})
	}
}

// TestNotifTargetsForFanOut: a ShiftApproved event with several checked-in
// attendants produces one targeted notification per attendant, de-duplicated.
func TestNotifTargetsForFanOut(t *testing.T) {
	t.Parallel()
	a, b := uuid.New(), uuid.New()
	payload := `{"checked_in_attendant_ids":["` + a.String() + `","` + b.String() + `","` + a.String() + `"]}`
	targets := notifTargetsFor(events.Event{Type: "ShiftApproved", Payload: json.RawMessage(payload)})
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2 (deduplicated)", len(targets))
	}
	got := map[uuid.UUID]bool{}
	for _, tg := range targets {
		if tg.userID == nil {
			t.Fatal("shift.approved target must be user-targeted")
		}
		got[*tg.userID] = true
	}
	if !got[a] || !got[b] {
		t.Fatalf("targets = %v, want both %s and %s", got, a, b)
	}
}

// TestNotifTargetsForFallbacks: payloads that cannot resolve a personal target
// degrade safely — corrected readings fall back to the Phase 3 tenant-wide
// entry (so an in-flight pre-Phase-7 event still surfaces), while the purely
// personal mappings are a silent no-op.
func TestNotifTargetsForFallbacks(t *testing.T) {
	t.Parallel()

	// Old-shaped correction payload (no recorded_by): tenant-wide fallback.
	targets := notifTargetsFor(events.Event{
		Type:    "ReadingVerificationCorrected",
		Payload: json.RawMessage(`{"verification":{"reason":"misread"}}`),
	})
	if len(targets) != 1 || targets[0].userID != nil {
		t.Fatalf("corrected without recorded_by = %+v, want one tenant-wide target", targets)
	}
	if targets[0].spec.notifType != "reading.corrected" || targets[0].spec.severity != notifications.SeverityWarning {
		t.Fatalf("fallback spec = %+v, want reading.corrected/warning", targets[0].spec)
	}

	// Personal mappings without a resolvable user: no notification at all.
	for _, et := range []string{"ShiftNozzleAssigned", "ShiftNozzleUnassigned", "CashCollectionConfirmed", "ShiftApproved"} {
		if got := notifTargetsFor(events.Event{Type: et, Payload: json.RawMessage(`{}`)}); len(got) != 0 {
			t.Errorf("notifTargetsFor(%s, empty payload) = %+v, want none", et, got)
		}
	}

	// Unknown event type: silent no-op.
	if got := notifTargetsFor(events.Event{Type: "SomethingElseHappened"}); len(got) != 0 {
		t.Fatalf("unmapped event produced targets: %+v", got)
	}
}

// TestSubscribedEventTypesAllMap guards against the wiring list and the mapping
// drifting apart: every type the subscriber registers must produce at least one
// target given a representative payload.
func TestSubscribedEventTypesAllMap(t *testing.T) {
	t.Parallel()
	u := uuid.NewString()
	representative := map[string]string{
		"ReadingVerificationCorrected": `{"recorded_by":"` + u + `"}`,
		"ReadingVerificationRejected":  `{"recorded_by":"` + u + `"}`,
		"ShiftNozzleAssigned":          `{"attendant_id":"` + u + `"}`,
		"ShiftNozzleUnassigned":        `{"attendant_id":"` + u + `"}`,
		"CashCollectionConfirmed":      `{"submitted_by":"` + u + `"}`,
		"ShiftApproved":                `{"checked_in_attendant_ids":["` + u + `"]}`,
	}
	for _, et := range subscribedEventTypes {
		payload := representative[et]
		targets := notifTargetsFor(events.Event{Type: et, Payload: json.RawMessage(payload)})
		if len(targets) == 0 {
			t.Errorf("subscribed event %q has no mapping in notifTargetsFor", et)
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

func TestDecimalSignHelpers(t *testing.T) {
	t.Parallel()
	if !decimalIsZero("") || !decimalIsZero("0") || !decimalIsZero("0.00") || !decimalIsZero("-0.00") {
		t.Error("decimalIsZero must accept empty/zero forms")
	}
	if decimalIsZero("5.00") || decimalIsZero("-5.00") {
		t.Error("decimalIsZero must reject non-zero figures")
	}
	if !decimalIsNegative("-5000.00") {
		t.Error("decimalIsNegative(-5000.00) = false, want true")
	}
	if decimalIsNegative("-0.00") || decimalIsNegative("1000.00") || decimalIsNegative("") {
		t.Error("decimalIsNegative must reject negative-zero, positive, empty")
	}
}
