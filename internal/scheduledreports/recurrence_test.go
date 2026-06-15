package scheduledreports

import (
	"testing"
	"time"
)

func intp(v int) *int { return &v }

// TestNextRunAfterDaily: a daily schedule lands on today at hh:mm when that is in
// the future, otherwise tomorrow; the result is always strictly after `after`.
func TestNextRunAfterDaily(t *testing.T) {
	t.Parallel()
	loc := time.UTC
	s := Schedule{Frequency: FrequencyDaily, Hour: 22, Minute: 30}

	// Before the daily time today -> today 22:30.
	after := time.Date(2026, 6, 15, 9, 0, 0, 0, loc)
	got, err := s.NextRunAfter(after)
	if err != nil {
		t.Fatalf("NextRunAfter: %v", err)
	}
	want := time.Date(2026, 6, 15, 22, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("daily before-time: got %s want %s", got, want)
	}

	// Exactly at the daily time -> tomorrow (strictly after).
	at := time.Date(2026, 6, 15, 22, 30, 0, 0, loc)
	got, _ = s.NextRunAfter(at)
	want = time.Date(2026, 6, 16, 22, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("daily at-time: got %s want %s", got, want)
	}

	// After the daily time -> tomorrow.
	after = time.Date(2026, 6, 15, 23, 0, 0, 0, loc)
	got, _ = s.NextRunAfter(after)
	want = time.Date(2026, 6, 16, 22, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("daily after-time: got %s want %s", got, want)
	}
}

// TestNextRunAfterWeekly: a weekly schedule resolves to the next occurrence of the
// target weekday at hh:mm, strictly after `after`.
func TestNextRunAfterWeekly(t *testing.T) {
	t.Parallel()
	loc := time.UTC
	// Monday = 1. 2026-06-15 is a Monday.
	s := Schedule{Frequency: FrequencyWeekly, Hour: 8, Minute: 0, DayOfWeek: intp(1)}

	// On the target Monday, before 08:00 -> same day 08:00.
	after := time.Date(2026, 6, 15, 7, 0, 0, 0, loc) // Mon
	got, err := s.NextRunAfter(after)
	if err != nil {
		t.Fatalf("weekly: %v", err)
	}
	want := time.Date(2026, 6, 15, 8, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("weekly same-day: got %s want %s", got, want)
	}

	// On the target Monday, after 08:00 -> next Monday.
	after = time.Date(2026, 6, 15, 9, 0, 0, 0, loc)
	got, _ = s.NextRunAfter(after)
	want = time.Date(2026, 6, 22, 8, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("weekly next-week: got %s want %s", got, want)
	}

	// On a Wednesday -> the upcoming Monday.
	after = time.Date(2026, 6, 17, 12, 0, 0, 0, loc) // Wed
	got, _ = s.NextRunAfter(after)
	want = time.Date(2026, 6, 22, 8, 0, 0, 0, loc)
	if got.Weekday() != time.Monday || !got.Equal(want) {
		t.Fatalf("weekly from-wed: got %s (%s) want %s", got, got.Weekday(), want)
	}
}

// TestNextRunAfterMonthly: a monthly schedule lands on the clamped day-of-month at
// hh:mm, rolling to the next month when already past, and clamps day 31 to the
// month's actual length.
func TestNextRunAfterMonthly(t *testing.T) {
	t.Parallel()
	loc := time.UTC
	s := Schedule{Frequency: FrequencyMonthly, Hour: 6, Minute: 0, DayOfMonth: intp(1)}

	// Mid-month -> the 1st of next month.
	after := time.Date(2026, 6, 15, 9, 0, 0, 0, loc)
	got, err := s.NextRunAfter(after)
	if err != nil {
		t.Fatalf("monthly: %v", err)
	}
	want := time.Date(2026, 7, 1, 6, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("monthly mid-month: got %s want %s", got, want)
	}

	// day_of_month=31 in a 30-day month clamps to the 30th.
	s31 := Schedule{Frequency: FrequencyMonthly, Hour: 6, Minute: 0, DayOfMonth: intp(31)}
	after = time.Date(2026, 4, 10, 0, 0, 0, 0, loc) // April has 30 days
	got, _ = s31.NextRunAfter(after)
	want = time.Date(2026, 4, 30, 6, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("monthly clamp-april: got %s want %s", got, want)
	}

	// day_of_month=31 in February 2026 (28 days) clamps to the 28th.
	after = time.Date(2026, 2, 5, 0, 0, 0, 0, loc)
	got, _ = s31.NextRunAfter(after)
	want = time.Date(2026, 2, 28, 6, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("monthly clamp-feb: got %s want %s", got, want)
	}
}

// TestNextRunAfterCron: the 5-field cron escape hatch resolves the next matching
// minute, and rejects malformed expressions.
func TestNextRunAfterCron(t *testing.T) {
	t.Parallel()
	loc := time.UTC

	// "30 22 * * *" — daily at 22:30.
	s := Schedule{Frequency: FrequencyCron, Cron: "30 22 * * *"}
	after := time.Date(2026, 6, 15, 9, 0, 0, 0, loc)
	got, err := s.NextRunAfter(after)
	if err != nil {
		t.Fatalf("cron: %v", err)
	}
	want := time.Date(2026, 6, 15, 22, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("cron daily: got %s want %s", got, want)
	}

	// "0 9 * * 1" — Mondays at 09:00. From a Wednesday -> the upcoming Monday.
	sMon := Schedule{Frequency: FrequencyCron, Cron: "0 9 * * 1"}
	after = time.Date(2026, 6, 17, 12, 0, 0, 0, loc) // Wed
	got, _ = sMon.NextRunAfter(after)
	if got.Weekday() != time.Monday || got.Hour() != 9 || got.Minute() != 0 {
		t.Fatalf("cron monday: got %s (%s)", got, got.Weekday())
	}

	// Malformed: wrong field count + out-of-range value.
	for _, bad := range []string{"30 22 * *", "60 22 * * *", "* 24 * * *", "abc"} {
		if err := (Schedule{Frequency: FrequencyCron, Cron: bad}).Validate(); err == nil {
			t.Fatalf("expected %q to be rejected", bad)
		}
	}
}

// TestValidate rejects malformed schedules across all frequencies.
func TestValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		s    Schedule
		ok   bool
	}{
		{"daily ok", Schedule{Frequency: FrequencyDaily, Hour: 6, Minute: 0}, true},
		{"daily bad hour", Schedule{Frequency: FrequencyDaily, Hour: 24}, false},
		{"weekly needs dow", Schedule{Frequency: FrequencyWeekly, Hour: 6}, false},
		{"weekly bad dow", Schedule{Frequency: FrequencyWeekly, Hour: 6, DayOfWeek: intp(9)}, false},
		{"weekly ok", Schedule{Frequency: FrequencyWeekly, Hour: 6, DayOfWeek: intp(0)}, true},
		{"monthly needs dom", Schedule{Frequency: FrequencyMonthly, Hour: 6}, false},
		{"monthly bad dom", Schedule{Frequency: FrequencyMonthly, Hour: 6, DayOfMonth: intp(0)}, false},
		{"monthly ok", Schedule{Frequency: FrequencyMonthly, Hour: 6, DayOfMonth: intp(15)}, true},
		{"unknown freq", Schedule{Frequency: "yearly"}, false},
		{"cron ok", Schedule{Frequency: FrequencyCron, Cron: "0 0 1 1 *"}, true},
	}
	for _, c := range cases {
		err := c.s.Validate()
		if c.ok && err != nil {
			t.Fatalf("%s: expected valid, got %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Fatalf("%s: expected invalid, got nil", c.name)
		}
	}
}

// TestPeriodKey: the idempotency key is the coarse logical period, so two ticks in
// the same period share a key and the run-ledger UNIQUE collapses them.
func TestPeriodKey(t *testing.T) {
	t.Parallel()
	loc := time.UTC
	daily := Schedule{Frequency: FrequencyDaily}
	a := time.Date(2026, 6, 15, 22, 30, 0, 0, loc)
	b := time.Date(2026, 6, 15, 22, 31, 0, 0, loc) // same day, one minute later
	if daily.PeriodKey(a) != daily.PeriodKey(b) {
		t.Fatalf("daily period keys differ within the same day: %q vs %q", daily.PeriodKey(a), daily.PeriodKey(b))
	}
	if daily.PeriodKey(a) != "2026-06-15" {
		t.Fatalf("daily period key = %q want 2026-06-15", daily.PeriodKey(a))
	}

	monthly := Schedule{Frequency: FrequencyMonthly}
	if monthly.PeriodKey(a) != "2026-06" {
		t.Fatalf("monthly period key = %q want 2026-06", monthly.PeriodKey(a))
	}

	weekly := Schedule{Frequency: FrequencyWeekly}
	wk := weekly.PeriodKey(a)
	if wk == "" || wk[4] != '-' {
		t.Fatalf("weekly period key malformed: %q", wk)
	}

	// Different days yield different daily keys (so the next period is a fresh run).
	c := time.Date(2026, 6, 16, 0, 0, 0, 0, loc)
	if daily.PeriodKey(a) == daily.PeriodKey(c) {
		t.Fatalf("daily period keys should differ across days")
	}
}
