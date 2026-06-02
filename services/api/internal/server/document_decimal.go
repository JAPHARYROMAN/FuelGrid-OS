package server

import "math/big"

// Exact decimal-string arithmetic for record documents (DOC-PDF).
//
// Record documents (single purchase order, single invoice) need a couple of
// derived figures the repos don't pre-compute: a PO line total
// (ordered_litres × unit_price) and the per-document sum of those lines. The
// house rule is that money/litre figures are exact decimal STRINGS and never
// pass through float64. These helpers honour that by doing the arithmetic with
// math/big.Rat (arbitrary-precision rationals) and formatting the result to a
// fixed number of decimal places, so a figure on the page is exact — never a
// binary-float approximation.

// decMul multiplies two decimal strings exactly and formats the product to
// scale decimal places (rounded half-to-even by big.Rat.FloatString). It
// returns ("", false) when either operand is not a valid decimal. Used for a
// purchase-order line total: ordered_litres × unit_price.
func decMul(a, b string, scale int) (string, bool) {
	ra, ok := new(big.Rat).SetString(a)
	if !ok {
		return "", false
	}
	rb, ok := new(big.Rat).SetString(b)
	if !ok {
		return "", false
	}
	return new(big.Rat).Mul(ra, rb).FloatString(scale), true
}

// decAccumulator sums decimal strings exactly. Invalid addends are skipped so a
// single malformed cell can't poison a whole document total; callers that need
// strictness can validate first.
type decAccumulator struct{ sum *big.Rat }

func newDecAccumulator() *decAccumulator { return &decAccumulator{sum: new(big.Rat)} }

// add folds one decimal string into the running sum, ignoring invalid input.
func (d *decAccumulator) add(s string) {
	if r, ok := new(big.Rat).SetString(s); ok {
		d.sum.Add(d.sum, r)
	}
}

// string formats the running sum to scale decimal places.
func (d *decAccumulator) string(scale int) string { return d.sum.FloatString(scale) }
