// Package scheduledreports is the data layer + recurrence model for per-tenant
// Scheduled Reports (Reports Center Phase 12 — blueprint §8).
//
// A scheduled report re-runs one catalog report on a recurrence and delivers the
// rendered file to a set of recipients over a channel (in-app / email / webhook).
// This file owns the RECURRENCE MODEL: a small, fully representable, deterministic
// shape (daily / weekly / monthly / 5-field cron) and the pure next-run / period-
// key computation the worker and the CRUD layer both use. No IO lives here, so the
// recurrence math is unit-testable in isolation.
package scheduledreports

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Frequency is the recurrence kind.
const (
	FrequencyDaily   = "daily"
	FrequencyWeekly  = "weekly"
	FrequencyMonthly = "monthly"
	FrequencyCron    = "cron"
)

// Schedule is the representable recurrence shape stored in scheduled_reports.schedule.
//
//	daily   : {"frequency":"daily","hour":22,"minute":30}
//	weekly  : {"frequency":"weekly","hour":8,"minute":0,"day_of_week":1}   // 0=Sun..6=Sat
//	monthly : {"frequency":"monthly","hour":6,"minute":0,"day_of_month":1} // clamped to month length
//	cron    : {"frequency":"cron","cron":"30 22 * * *"}                     // 5-field min hour dom mon dow
//
// All times are interpreted in the server's location (the same clock the rest of
// the scheduler uses), which keeps the model deterministic and testable.
type Schedule struct {
	Frequency  string `json:"frequency"`
	Hour       int    `json:"hour,omitempty"`
	Minute     int    `json:"minute,omitempty"`
	DayOfWeek  *int   `json:"day_of_week,omitempty"`  // weekly: 0=Sun..6=Sat
	DayOfMonth *int   `json:"day_of_month,omitempty"` // monthly: 1..31 (clamped)
	Cron       string `json:"cron,omitempty"`         // cron: "min hour dom mon dow"
}

// Validate checks the schedule shape is well-formed and representable. It returns
// a human-readable error suitable for a 400.
func (s Schedule) Validate() error {
	switch s.Frequency {
	case FrequencyDaily:
		return validHourMinute(s.Hour, s.Minute)
	case FrequencyWeekly:
		if err := validHourMinute(s.Hour, s.Minute); err != nil {
			return err
		}
		if s.DayOfWeek == nil {
			return fmt.Errorf("weekly schedule requires day_of_week (0=Sun..6=Sat)")
		}
		if *s.DayOfWeek < 0 || *s.DayOfWeek > 6 {
			return fmt.Errorf("day_of_week must be 0..6")
		}
		return nil
	case FrequencyMonthly:
		if err := validHourMinute(s.Hour, s.Minute); err != nil {
			return err
		}
		if s.DayOfMonth == nil {
			return fmt.Errorf("monthly schedule requires day_of_month (1..31)")
		}
		if *s.DayOfMonth < 1 || *s.DayOfMonth > 31 {
			return fmt.Errorf("day_of_month must be 1..31")
		}
		return nil
	case FrequencyCron:
		_, err := parseCron(s.Cron)
		return err
	default:
		return fmt.Errorf("frequency must be daily|weekly|monthly|cron")
	}
}

func validHourMinute(h, m int) error {
	if h < 0 || h > 23 {
		return fmt.Errorf("hour must be 0..23")
	}
	if m < 0 || m > 59 {
		return fmt.Errorf("minute must be 0..59")
	}
	return nil
}

// NextRunAfter returns the first scheduled instant STRICTLY after `after`, in
// `after`'s location. It is deterministic: the same schedule + the same `after`
// always yields the same instant, which is what makes idempotency (advance
// next_run_at past the just-handled instant) and the recurrence tests reliable.
func (s Schedule) NextRunAfter(after time.Time) (time.Time, error) {
	if err := s.Validate(); err != nil {
		return time.Time{}, err
	}
	loc := after.Location()
	switch s.Frequency {
	case FrequencyDaily:
		// Today at hh:mm; if that is not strictly after `after`, roll to tomorrow.
		cand := time.Date(after.Year(), after.Month(), after.Day(), s.Hour, s.Minute, 0, 0, loc)
		if !cand.After(after) {
			cand = cand.AddDate(0, 0, 1)
		}
		return cand, nil
	case FrequencyWeekly:
		cand := time.Date(after.Year(), after.Month(), after.Day(), s.Hour, s.Minute, 0, 0, loc)
		// Advance to the next occurrence of the target weekday that is strictly after.
		for i := 0; i < 8; i++ {
			if int(cand.Weekday()) == *s.DayOfWeek && cand.After(after) {
				return cand, nil
			}
			cand = cand.AddDate(0, 0, 1)
		}
		return cand, nil // unreachable; the loop always finds a day within a week
	case FrequencyMonthly:
		// Start at this month's clamped target day; roll forward month-by-month until
		// strictly after `after`. Clamp the day to the month's length so day_of_month=31
		// lands on Feb 28/29, Apr 30, etc.
		year, month := after.Year(), after.Month()
		for i := 0; i < 14; i++ {
			day := clampDay(year, month, *s.DayOfMonth)
			cand := time.Date(year, month, day, s.Hour, s.Minute, 0, 0, loc)
			if cand.After(after) {
				return cand, nil
			}
			month++
			if month > 12 {
				month = 1
				year++
			}
		}
		return time.Time{}, fmt.Errorf("monthly: could not resolve next run")
	case FrequencyCron:
		spec, err := parseCron(s.Cron)
		if err != nil {
			return time.Time{}, err
		}
		return spec.next(after), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported frequency %q", s.Frequency)
	}
}

// PeriodKey returns the stable identity of the logical period the instant `at`
// belongs to — the idempotency key recorded in scheduled_report_runs. Two ticks
// that resolve to the same logical period produce the SAME key, so the UNIQUE
// (scheduled_report_id, period_key) index collapses a duplicated/missed-tick run
// to exactly one delivery.
//
//   - daily   : the calendar date              (2026-06-15)
//   - weekly  : ISO year-week                   (2026-W24)
//   - monthly : the calendar month             (2026-06)
//   - cron    : the exact resolved minute       (2026-06-15T22:30) — cron has no
//     coarser natural period, so the minute is its identity.
func (s Schedule) PeriodKey(at time.Time) string {
	switch s.Frequency {
	case FrequencyDaily:
		return at.Format("2006-01-02")
	case FrequencyWeekly:
		y, w := at.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", y, w)
	case FrequencyMonthly:
		return at.Format("2006-01")
	case FrequencyCron:
		return at.Format("2006-01-02T15:04")
	default:
		return at.Format(time.RFC3339)
	}
}

// clampDay clamps `day` to the number of days in (year, month). e.g. day=31 in
// February clamps to 28 (or 29 in a leap year).
func clampDay(year int, month time.Month, day int) int {
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day > last {
		return last
	}
	if day < 1 {
		return 1
	}
	return day
}

// ---- Minimal 5-field cron (min hour dom mon dow) ----
//
// A deliberately small cron implementation: each field is "*", a single value, a
// comma list, or a range a-b. No step (/n) or names — the daily/weekly/monthly
// shapes cover the common cadences; cron is the power-user escape hatch. dow
// 0 and 7 both mean Sunday.

type cronSpec struct {
	min, hour, dom, mon, dow map[int]bool
}

func parseCron(expr string) (cronSpec, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return cronSpec{}, fmt.Errorf("cron must have 5 fields: min hour dom mon dow")
	}
	var (
		spec cronSpec
		err  error
	)
	if spec.min, err = cronField(fields[0], 0, 59); err != nil {
		return cronSpec{}, fmt.Errorf("cron minute: %w", err)
	}
	if spec.hour, err = cronField(fields[1], 0, 23); err != nil {
		return cronSpec{}, fmt.Errorf("cron hour: %w", err)
	}
	if spec.dom, err = cronField(fields[2], 1, 31); err != nil {
		return cronSpec{}, fmt.Errorf("cron day-of-month: %w", err)
	}
	if spec.mon, err = cronField(fields[3], 1, 12); err != nil {
		return cronSpec{}, fmt.Errorf("cron month: %w", err)
	}
	if spec.dow, err = cronField(fields[4], 0, 7); err != nil {
		return cronSpec{}, fmt.Errorf("cron day-of-week: %w", err)
	}
	// Normalise dow 7 -> 0 (both Sunday).
	if spec.dow[7] {
		spec.dow[0] = true
		delete(spec.dow, 7)
	}
	return spec, nil
}

func cronField(field string, lo, hi int) (map[int]bool, error) {
	out := map[int]bool{}
	if field == "*" {
		for v := lo; v <= hi; v++ {
			out[v] = true
		}
		return out, nil
	}
	for _, part := range strings.Split(field, ",") {
		if rng := strings.SplitN(part, "-", 2); len(rng) == 2 {
			a, err1 := strconv.Atoi(rng[0])
			b, err2 := strconv.Atoi(rng[1])
			if err1 != nil || err2 != nil || a > b || a < lo || b > hi {
				return nil, fmt.Errorf("invalid range %q", part)
			}
			for v := a; v <= b; v++ {
				out[v] = true
			}
			continue
		}
		v, err := strconv.Atoi(part)
		if err != nil || v < lo || v > hi {
			return nil, fmt.Errorf("invalid value %q", part)
		}
		out[v] = true
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty field")
	}
	return out, nil
}

// next returns the first minute strictly after `after` that matches the spec. It
// walks minute-by-minute, bounded to ~366 days so a never-matching expression
// (e.g. Feb 30) terminates instead of looping forever.
func (c cronSpec) next(after time.Time) time.Time {
	// Standard cron semantics: when BOTH dom and dow are restricted (not "*"), a day
	// matches if it satisfies EITHER. When only one is restricted, that one must match.
	domRestricted := len(c.dom) != 31
	dowRestricted := len(c.dow) != 7

	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := after.AddDate(1, 0, 1)
	for t.Before(limit) {
		if c.mon[int(t.Month())] && c.hour[t.Hour()] && c.min[t.Minute()] {
			domOK := c.dom[t.Day()]
			dowOK := c.dow[int(t.Weekday())]
			var dayOK bool
			switch {
			case domRestricted && dowRestricted:
				dayOK = domOK || dowOK
			case domRestricted:
				dayOK = domOK
			case dowRestricted:
				dayOK = dowOK
			default:
				dayOK = true
			}
			if dayOK {
				return t
			}
		}
		t = t.Add(time.Minute)
	}
	return t
}

// scheduleFromJSON decodes the stored schedule jsonb into a Schedule.
func scheduleFromJSON(raw json.RawMessage) (Schedule, error) {
	var s Schedule
	if err := json.Unmarshal(raw, &s); err != nil {
		return Schedule{}, err
	}
	return s, nil
}
