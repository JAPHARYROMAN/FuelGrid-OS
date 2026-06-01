package main

import (
	"encoding/json"
	"testing"
)

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
