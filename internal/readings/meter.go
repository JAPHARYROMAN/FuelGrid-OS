// Package readings is the data layer + math for shift readings — pump meter
// readings now, tank dip readings in the next stage.
package readings

import (
	"errors"
	"math"
)

var (
	// ErrMeterRollback is returned when a closing meter reads below its
	// opening. Phase 3 treats this as an error rather than silently
	// assuming a meter wrap; a correction is the operator's recourse.
	ErrMeterRollback = errors.New("readings: closing meter is below opening")
	// ErrPrecision is returned when a reading carries more decimal places
	// than the nozzle's configured meter precision.
	ErrPrecision = errors.New("readings: reading has more decimals than the nozzle's meter precision")
)

// LitresDispensed returns closing - opening, rejecting a closing below the
// opening (no implicit rollover handling in Phase 3).
func LitresDispensed(opening, closing float64) (float64, error) {
	if closing < opening {
		return 0, ErrMeterRollback
	}
	return closing - opening, nil
}

// ValidateScale checks that reading carries no more decimal places than
// decimalPlaces. It works on the float value: scaling by 10^dp must land on
// (within a tiny epsilon of) a whole number.
func ValidateScale(reading float64, decimalPlaces int) error {
	if decimalPlaces < 0 {
		decimalPlaces = 0
	}
	scaled := reading * math.Pow10(decimalPlaces)
	if math.Abs(scaled-math.Round(scaled)) > 1e-6 {
		return ErrPrecision
	}
	return nil
}
