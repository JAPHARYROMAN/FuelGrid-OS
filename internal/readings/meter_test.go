package readings

import (
	"errors"
	"math"
	"testing"
)

func TestLitresDispensed(t *testing.T) {
	got, err := LitresDispensed(10000, 10500.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(got-500.5) > 1e-6 {
		t.Fatalf("got %v, want 500.5", got)
	}
	if _, err := LitresDispensed(10500, 10000); !errors.Is(err, ErrMeterRollback) {
		t.Fatalf("expected ErrMeterRollback, got %v", err)
	}
	if got, _ := LitresDispensed(100, 100); got != 0 {
		t.Fatalf("equal readings should dispense 0, got %v", got)
	}
}

func TestValidateScale(t *testing.T) {
	ok := []struct {
		v  float64
		dp int
	}{
		{12.34, 2}, {12.3, 2}, {12, 2}, {12.345, 3}, {100, 0}, {12.30, 2},
	}
	for _, c := range ok {
		if err := ValidateScale(c.v, c.dp); err != nil {
			t.Errorf("ValidateScale(%v, %d) = %v, want nil", c.v, c.dp, err)
		}
	}
	bad := []struct {
		v  float64
		dp int
	}{
		{12.345, 2}, {12.1, 0}, {12.3456, 3},
	}
	for _, c := range bad {
		if err := ValidateScale(c.v, c.dp); !errors.Is(err, ErrPrecision) {
			t.Errorf("ValidateScale(%v, %d) = %v, want ErrPrecision", c.v, c.dp, err)
		}
	}
}
