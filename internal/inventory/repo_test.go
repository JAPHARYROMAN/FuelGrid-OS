package inventory

import "testing"

// TestNegateDecimal locks in the contra-litres negation used by ReverseMovement:
// it must flip the sign of a decimal STRING without float arithmetic, and must
// never emit a spurious "-0" for any spelling of zero.
func TestNegateDecimal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"10000.000", "-10000.000"},
		{"-10000.000", "10000.000"},
		{"4200", "-4200"},
		{"-4200", "4200"},
		{"0", "0"},
		{"0.000", "0.000"},
		{"-0.000", "0.000"},
		{"0.500", "-0.500"},
		{"-0.500", "0.500"},
		{"", ""},
	}
	for _, c := range cases {
		if got := negateDecimal(c.in); got != c.want {
			t.Errorf("negateDecimal(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
