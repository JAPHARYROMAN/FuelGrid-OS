package workforce

import (
	"testing"
	"time"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestRotation_ThreeDayCycle(t *testing.T) {
	anchor := date(2026, 6, 1)

	tests := []struct {
		day                    time.Time
		cycle, morn, eve, rest int
	}{
		// cycle day 0
		{date(2026, 6, 1), 0, 0, 1, 2},
		// cycle day 1
		{date(2026, 6, 2), 1, 2, 0, 1},
		// cycle day 2
		{date(2026, 6, 3), 2, 1, 2, 0},
		// wraps back to cycle day 0
		{date(2026, 6, 4), 0, 0, 1, 2},
		{date(2026, 6, 5), 1, 2, 0, 1},
	}

	for _, tt := range tests {
		got := Rotation(anchor, tt.day)
		if got.CycleDay != tt.cycle || got.MorningOrder != tt.morn ||
			got.EveningOrder != tt.eve || got.RestOrder != tt.rest {
			t.Errorf("Rotation(%s) = %+v; want cycle=%d morning=%d evening=%d rest=%d",
				tt.day.Format("2006-01-02"), got, tt.cycle, tt.morn, tt.eve, tt.rest)
		}
	}
}

func TestRotation_EachTeamGetsEveryRole(t *testing.T) {
	anchor := date(2026, 1, 1)
	// Over a full 3-day cycle each team order must appear exactly once in each
	// of the three roles (morning, evening, rest) — fair rotation.
	seenMorning := map[int]int{}
	seenEvening := map[int]int{}
	seenRest := map[int]int{}
	for i := 0; i < 3; i++ {
		d := Rotation(anchor, anchor.AddDate(0, 0, i))
		seenMorning[d.MorningOrder]++
		seenEvening[d.EveningOrder]++
		seenRest[d.RestOrder]++
		// The three roles on any given day must be three distinct teams.
		if d.MorningOrder == d.EveningOrder || d.MorningOrder == d.RestOrder || d.EveningOrder == d.RestOrder {
			t.Fatalf("day %d assigns a team to two roles: %+v", i, d)
		}
	}
	for order := 0; order < 3; order++ {
		if seenMorning[order] != 1 || seenEvening[order] != 1 || seenRest[order] != 1 {
			t.Errorf("team order %d not balanced across cycle: morning=%d evening=%d rest=%d",
				order, seenMorning[order], seenEvening[order], seenRest[order])
		}
	}
}

func TestRotation_BeforeAnchor(t *testing.T) {
	anchor := date(2026, 6, 1)
	// One day before the anchor is cycle day 2 (non-negative modulo).
	got := Rotation(anchor, date(2026, 5, 31))
	if got.CycleDay != 2 {
		t.Errorf("day before anchor: cycle = %d; want 2", got.CycleDay)
	}
}

func TestRotation_IgnoresTimeOfDay(t *testing.T) {
	anchor := time.Date(2026, 6, 1, 6, 0, 0, 0, time.UTC)
	morning := time.Date(2026, 6, 2, 5, 30, 0, 0, time.UTC)
	evening := time.Date(2026, 6, 2, 23, 0, 0, 0, time.UTC)
	if Rotation(anchor, morning) != Rotation(anchor, evening) {
		t.Error("rotation must depend only on the calendar date, not time of day")
	}
}

func TestSlotValid(t *testing.T) {
	if !SlotMorning.Valid() || !SlotEvening.Valid() {
		t.Error("morning/evening must be valid slots")
	}
	if Slot("night").Valid() {
		t.Error("unknown slot must be invalid")
	}
}
