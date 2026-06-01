package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/email"
)

// fakeSender is a non-console Sender that records sends; it lets the gating
// tests prove the jobs short-circuit BEFORE any send/DB call without standing
// up Postgres.
type fakeSender struct {
	driver string
	sent   []email.Message
	err    error
}

func (f *fakeSender) Send(_ context.Context, m email.Message) error {
	f.sent = append(f.sent, m)
	return f.err
}
func (f *fakeSender) Driver() string {
	if f.driver == "" {
		return "smtp"
	}
	return f.driver
}

func atHour(h int) func() time.Time {
	return func() time.Time { return time.Date(2026, 6, 1, h, 30, 0, 0, time.UTC) }
}

// TestReportConfigured: a digest is "configured" only with at least one
// recipient AND a real (non-console) sender. No recipients, a nil sender, or
// the console driver all mean a safe no-op.
func TestReportConfigured(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		d    ReportDeps
		want bool
	}{
		{"no recipients", ReportDeps{Email: &fakeSender{}}, false},
		{"nil sender", ReportDeps{Recipients: []string{"a@b.test"}}, false},
		{"console sender", ReportDeps{Recipients: []string{"a@b.test"}, Email: &fakeSender{driver: "console"}}, false},
		{"smtp + recipients", ReportDeps{Recipients: []string{"a@b.test"}, Email: &fakeSender{}}, true},
	}
	for _, tc := range cases {
		if got := tc.d.configured(); got != tc.want {
			t.Errorf("%s: configured() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestDailyDigestNoOpWhenUnconfigured: with no recipients the job returns a
// skip detail and never touches the (nil) pool or sender.
func TestDailyDigestNoOpWhenUnconfigured(t *testing.T) {
	t.Parallel()
	job := dailyDigestJob(ReportDeps{SendHour: 6, now: atHour(8)})
	detail, err := job(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(detail, "skipped") {
		t.Fatalf("expected a skip detail, got %q", detail)
	}
}

// TestDigestSkipsBeforeSendHour: a configured job whose clock is before the
// send hour skips without sending (so the early-morning ticks are quiet).
func TestDigestSkipsBeforeSendHour(t *testing.T) {
	t.Parallel()
	fs := &fakeSender{}
	d := ReportDeps{Recipients: []string{"ops@b.test"}, Email: fs, SendHour: 6, now: atHour(3)}
	for _, job := range []JobFunc{dailyDigestJob(d), monthlyPnLJob(d)} {
		detail, err := job(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(detail, "before send hour") {
			t.Fatalf("expected before-send-hour skip, got %q", detail)
		}
	}
	if len(fs.sent) != 0 {
		t.Fatalf("expected no sends before the send hour, got %d", len(fs.sent))
	}
}

// TestPriorMonthBounds: priorMonth returns the previous calendar month's first
// instant and last second, regardless of which day of the current month it is.
func TestPriorMonthBounds(t *testing.T) {
	t.Parallel()
	from, to := priorMonth(time.Date(2026, 3, 15, 9, 0, 0, 0, time.UTC))
	if from.Year() != 2026 || from.Month() != time.February || from.Day() != 1 {
		t.Fatalf("from = %s, want 2026-02-01", from.Format("2006-01-02"))
	}
	if to.Year() != 2026 || to.Month() != time.February || to.Day() != 28 {
		t.Fatalf("to = %s, want 2026-02-28", to.Format("2006-01-02"))
	}
	// Year boundary: January's prior month is the previous December.
	from2, _ := priorMonth(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
	if from2.Year() != 2025 || from2.Month() != time.December {
		t.Fatalf("Jan prior month = %s, want 2025-12", from2.Format("2006-01"))
	}
}

// TestStartOfDayAndMonth truncate to the expected boundaries in the value's
// own location.
func TestStartOfDayAndMonth(t *testing.T) {
	t.Parallel()
	ref := time.Date(2026, 6, 15, 14, 22, 33, 0, time.UTC)
	if d := startOfDay(ref); d.Hour() != 0 || d.Day() != 15 || d.Minute() != 0 {
		t.Fatalf("startOfDay = %s", d)
	}
	if m := startOfMonth(ref); m.Day() != 1 || m.Hour() != 0 {
		t.Fatalf("startOfMonth = %s", m)
	}
}
