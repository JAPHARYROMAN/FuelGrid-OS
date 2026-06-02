package risk

import "testing"

// TestRenderTemplate locks the deterministic {token} substitution that turns a
// rule's message_template into an alert detail. No AI, no eval — plain string
// replacement, with unknown tokens left intact.
func TestRenderTemplate(t *testing.T) {
	cases := []struct {
		name string
		tmpl string
		vars map[string]string
		want string
	}{
		{
			name: "single token",
			tmpl: "{product} variance exceeded tolerance by {variance_litres} L.",
			vars: map[string]string{"product": "PMS", "variance_litres": "123.450"},
			want: "PMS variance exceeded tolerance by 123.450 L.",
		},
		{
			name: "repeated cash shortage",
			tmpl: "Attendant {attendant} has repeated shortages across {count} shifts in {days} days.",
			vars: map[string]string{"attendant": "Jane Doe", "count": "3", "days": "7"},
			want: "Attendant Jane Doe has repeated shortages across 3 shifts in 7 days.",
		},
		{
			name: "unknown token left intact",
			tmpl: "{product} at {station} reaches {hours}h",
			vars: map[string]string{"product": "AGO"},
			want: "AGO at {station} reaches {hours}h",
		},
		{
			name: "empty template",
			tmpl: "",
			vars: map[string]string{"x": "y"},
			want: "",
		},
		{
			name: "same token used twice",
			tmpl: "{product}/{product}",
			vars: map[string]string{"product": "PMS"},
			want: "PMS/PMS",
		},
		{
			name: "no vars",
			tmpl: "static message",
			vars: map[string]string{},
			want: "static message",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := renderTemplate(c.tmpl, c.vars); got != c.want {
				t.Fatalf("renderTemplate(%q) = %q, want %q", c.tmpl, got, c.want)
			}
		})
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 7: "7", 14: "14", 365: "365", -3: "-3"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Fatalf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestStrOr(t *testing.T) {
	if got := strOr("", "3"); got != "3" {
		t.Fatalf("strOr empty = %q, want 3", got)
	}
	if got := strOr("  ", "3"); got != "3" {
		t.Fatalf("strOr blank = %q, want 3", got)
	}
	if got := strOr("5", "3"); got != "5" {
		t.Fatalf("strOr value = %q, want 5", got)
	}
}

// TestEvaluatorRegistry ensures all four named conditions resolve to an
// evaluator and an unknown condition does not (RunDetection skips unknowns).
func TestEvaluatorRegistry(t *testing.T) {
	want := []string{
		"fuel_variance_over_tolerance",
		"repeated_cash_shortage",
		"stockout_coverage",
		"supplier_delivery_shortage",
	}
	for _, c := range want {
		if _, ok := EvaluatorFor(c); !ok {
			t.Fatalf("condition %q not registered", c)
		}
	}
	if _, ok := EvaluatorFor("does_not_exist"); ok {
		t.Fatalf("unknown condition resolved unexpectedly")
	}
}
