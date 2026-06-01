// Package workforce manages a station's employees, their three shift teams,
// and the daily rotation that assigns teams to the Morning/Evening slots (with
// the third team resting). The rotation is deterministic: given a station's
// anchor date (cycle day 0), the team on any slot on any date is computed with
// no stored roster.
package workforce

import "time"

// Slot is one of the two daily working windows. The third team rests.
type Slot string

const (
	SlotMorning Slot = "morning"
	SlotEvening Slot = "evening"
)

// Valid reports whether s is a recognised slot.
func (s Slot) Valid() bool { return s == SlotMorning || s == SlotEvening }

// RotationDay is the team-order assignment for a single date: which
// rotation_order (0..2) works Morning, which works Evening, and which rests.
//
// The pattern advances by one position each day on a 3-day cycle:
//
//	cycle day 0:  order0 → Morning   order1 → Evening   order2 → Rest
//	cycle day 1:  order2 → Morning   order0 → Evening   order1 → Rest
//	cycle day 2:  order1 → Morning   order2 → Evening   order0 → Rest
type RotationDay struct {
	CycleDay     int // 0..2
	MorningOrder int // team rotation_order working the Morning slot
	EveningOrder int // team rotation_order working the Evening slot
	RestOrder    int // team rotation_order resting
}

// OrderForSlot returns the team rotation_order working the given slot.
func (d RotationDay) OrderForSlot(s Slot) int {
	if s == SlotEvening {
		return d.EveningOrder
	}
	return d.MorningOrder
}

// Rotation computes the team-order assignment for date, where anchor is the
// station's cycle-day-0 date. Only the calendar date of each argument is used
// (time-of-day and zone are ignored) so the rotation never drifts.
func Rotation(anchor, date time.Time) RotationDay {
	cycle := mod3(daysBetween(anchor, date))
	morning := mod3(3 - cycle)
	evening := mod3(morning + 1)
	rest := mod3(morning + 2)
	return RotationDay{CycleDay: cycle, MorningOrder: morning, EveningOrder: evening, RestOrder: rest}
}

// daysBetween returns the whole-day difference (date b minus date a), using each
// argument's calendar date at UTC midnight to avoid DST/time-of-day artefacts.
func daysBetween(a, b time.Time) int {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	au := time.Date(ay, am, ad, 0, 0, 0, 0, time.UTC)
	bu := time.Date(by, bm, bd, 0, 0, 0, 0, time.UTC)
	return int(bu.Sub(au).Hours()) / 24
}

// mod3 is a Euclidean modulo-3 that always returns a non-negative result
// (0..2), so the rotation is correct for dates before the anchor too.
func mod3(a int) int { return ((a % 3) + 3) % 3 }
