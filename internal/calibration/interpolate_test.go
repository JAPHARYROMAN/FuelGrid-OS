package calibration

import (
	"errors"
	"math"
	"testing"
)

func TestInterpolate(t *testing.T) {
	// A small linear chart: volume = dip * 10.
	chart := []Entry{
		{DipMM: 0, VolumeLitres: 0},
		{DipMM: 1200, VolumeLitres: 12000},
		{DipMM: 1260, VolumeLitres: 12600},
		{DipMM: 3000, VolumeLitres: 30000},
	}

	tests := []struct {
		name string
		dip  float64
		want float64
	}{
		{"exact low bound", 0, 0},
		{"exact high bound", 3000, 30000},
		{"exact interior point", 1200, 12000},
		{"interpolated midpoint", 1240, 12400}, // 2/3 between 12000 and 12600
		{"interpolated between wide gap", 2130, 21300},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Interpolate(chart, tc.dip)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(got-tc.want) > 1e-6 {
				t.Fatalf("dip %v: got %v, want %v", tc.dip, got, tc.want)
			}
		})
	}
}

func TestInterpolateRefusesExtrapolation(t *testing.T) {
	chart := []Entry{{DipMM: 100, VolumeLitres: 1000}, {DipMM: 200, VolumeLitres: 2000}}
	for _, dip := range []float64{99.9, 200.1, -5} {
		if _, err := Interpolate(chart, dip); !errors.Is(err, ErrOutOfRange) {
			t.Fatalf("dip %v: expected ErrOutOfRange, got %v", dip, err)
		}
	}
}

func TestInterpolateEmptyChart(t *testing.T) {
	if _, err := Interpolate(nil, 100); !errors.Is(err, ErrEmptyChart) {
		t.Fatalf("expected ErrEmptyChart, got %v", err)
	}
}

func TestInterpolateUnsortedInput(t *testing.T) {
	chart := []Entry{
		{DipMM: 200, VolumeLitres: 2000},
		{DipMM: 0, VolumeLitres: 0},
		{DipMM: 100, VolumeLitres: 1000},
	}
	got, err := Interpolate(chart, 150)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(got-1500) > 1e-6 {
		t.Fatalf("got %v, want 1500", got)
	}
}
