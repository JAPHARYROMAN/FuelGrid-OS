package server

import (
	"encoding/json"
	"testing"
)

func TestDecimalInputUnmarshal(t *testing.T) {
	type body struct {
		V decimalInput `json:"v"`
	}

	cases := []struct {
		name    string
		json    string
		wantSet bool
		wantStr string
		wantVal bool // Valid()
	}{
		{"json number int", `{"v":2950}`, true, "2950", true},
		{"json number decimal", `{"v":2950.00}`, true, "2950.00", true},
		{"json number frac", `{"v":12.345}`, true, "12.345", true},
		{"json string", `{"v":"2820.50"}`, true, "2820.50", true},
		{"json string trims", `{"v":" 100 "}`, true, "100", true},
		{"absent", `{}`, false, "", false},
		{"null", `{"v":null}`, false, "", false},
		{"negative number invalid", `{"v":-5}`, true, "-5", false},
		{"too many decimals invalid", `{"v":"1.23456"}`, true, "1.23456", false},
		{"non-numeric string invalid", `{"v":"abc"}`, true, "abc", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b body
			if err := json.Unmarshal([]byte(c.json), &b); err != nil {
				t.Fatalf("unmarshal %q: %v", c.json, err)
			}
			if b.V.Set() != c.wantSet {
				t.Errorf("Set() = %v, want %v", b.V.Set(), c.wantSet)
			}
			if b.V.String() != c.wantStr {
				t.Errorf("String() = %q, want %q", b.V.String(), c.wantStr)
			}
			if b.V.Valid() != c.wantVal {
				t.Errorf("Valid() = %v, want %v", b.V.Valid(), c.wantVal)
			}
		})
	}
}

func TestDecimalInputValueOrAndPtr(t *testing.T) {
	type body struct {
		V decimalInput `json:"v"`
	}

	// Absent -> default for ValueOr, nil for Ptr.
	var absent body
	if err := json.Unmarshal([]byte(`{}`), &absent); err != nil {
		t.Fatal(err)
	}
	if got := absent.V.ValueOr("0"); got != "0" {
		t.Errorf("absent ValueOr = %q, want \"0\"", got)
	}
	if absent.V.Ptr() != nil {
		t.Errorf("absent Ptr = %v, want nil", absent.V.Ptr())
	}

	// Present valid -> own value for ValueOr, non-nil Ptr.
	var present body
	if err := json.Unmarshal([]byte(`{"v":"1234.56"}`), &present); err != nil {
		t.Fatal(err)
	}
	if got := present.V.ValueOr("0"); got != "1234.56" {
		t.Errorf("present ValueOr = %q, want \"1234.56\"", got)
	}
	if p := present.V.Ptr(); p == nil || *p != "1234.56" {
		t.Errorf("present Ptr = %v, want \"1234.56\"", p)
	}

	// Present invalid -> default for ValueOr, nil Ptr (rejected before bind).
	var invalid body
	if err := json.Unmarshal([]byte(`{"v":"-1"}`), &invalid); err != nil {
		t.Fatal(err)
	}
	if got := invalid.V.ValueOr("0"); got != "0" {
		t.Errorf("invalid ValueOr = %q, want \"0\"", got)
	}
	if invalid.V.Ptr() != nil {
		t.Errorf("invalid Ptr = %v, want nil", invalid.V.Ptr())
	}
}
