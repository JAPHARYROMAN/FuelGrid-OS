// Package calibration turns dipstick millimetres into litres using a tank's
// strapping chart. Charts are sparse (dip_mm -> volume_litres) points; a
// lookup linearly interpolates between the two surrounding entries and
// refuses to extrapolate beyond the charted range.
package calibration

import (
	"errors"
	"sort"
)

// Entry is one charted point: a dip depth and the volume it corresponds to.
type Entry struct {
	DipMM        float64
	VolumeLitres float64
}

var (
	// ErrEmptyChart is returned when a chart has no entries to interpolate from.
	ErrEmptyChart = errors.New("calibration: chart has no entries")
	// ErrOutOfRange is returned when the dip falls outside the charted range.
	// Interpolation never extrapolates — an out-of-range dip is an error, not
	// a guess.
	ErrOutOfRange = errors.New("calibration: dip is outside the chart's range")
)

// Interpolate returns the litre volume for dipMM by linearly interpolating
// the supplied entries. Entries need not be pre-sorted; a copy is sorted by
// dip ascending. An exact dip match returns its exact volume. A dip below the
// smallest or above the largest charted dip returns ErrOutOfRange.
func Interpolate(entries []Entry, dipMM float64) (float64, error) {
	if len(entries) == 0 {
		return 0, ErrEmptyChart
	}

	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].DipMM < sorted[j].DipMM })

	if dipMM < sorted[0].DipMM || dipMM > sorted[len(sorted)-1].DipMM {
		return 0, ErrOutOfRange
	}

	// Find the first entry whose dip is >= the target. Because the range
	// check above passed, this index is always within bounds.
	i := sort.Search(len(sorted), func(k int) bool { return sorted[k].DipMM >= dipMM })

	hi := sorted[i]
	if hi.DipMM == dipMM {
		return hi.VolumeLitres, nil
	}

	// hi.DipMM > dipMM and i > 0 (i==0 would mean dipMM < sorted[0], excluded).
	lo := sorted[i-1]
	span := hi.DipMM - lo.DipMM
	if span == 0 {
		return lo.VolumeLitres, nil
	}
	ratio := (dipMM - lo.DipMM) / span
	return lo.VolumeLitres + ratio*(hi.VolumeLitres-lo.VolumeLitres), nil
}
