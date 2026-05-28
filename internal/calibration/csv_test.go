package calibration

import (
	"strings"
	"testing"
)

func TestParseCSVHappyPath(t *testing.T) {
	in := "dip_mm,volume_litres\n0,0\n60,600\n120,1200\n"
	entries, err := ParseCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[2].DipMM != 120 || entries[2].VolumeLitres != 1200 {
		t.Fatalf("unexpected last entry: %+v", entries[2])
	}
}

func TestParseCSVRejects(t *testing.T) {
	cases := map[string]string{
		"non-integer dip":    "dip_mm,volume_litres\n0,0\n60.5,600\n",
		"wrong header":       "depth,volume\n0,0\n60,600\n",
		"non-monotonic dip":  "dip_mm,volume_litres\n0,0\n60,600\n50,500\n",
		"decreasing volume":  "dip_mm,volume_litres\n0,0\n60,600\n120,400\n",
		"negative value":     "dip_mm,volume_litres\n0,0\n60,-10\n",
		"too few data rows":  "dip_mm,volume_litres\n0,0\n",
		"non-numeric volume": "dip_mm,volume_litres\n0,0\n60,abc\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseCSV(strings.NewReader(in)); err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}
