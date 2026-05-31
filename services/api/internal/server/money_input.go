package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// decimalInput is a money/litre/rate/capacity value accepted on a request body
// as EITHER a JSON number (e.g. 1234.56) or a JSON string (e.g. "1234.56").
// It is stored as the exact decimal STRING and bound into a numeric column with
// $N::numeric — no float64 ever touches the persisted figure. The DB column's
// own scale (numeric(p,s)) is what canonicalises the value on write, and the
// ::text projection on read returns it at that scale.
//
// Accepting both forms keeps existing JSON-number clients working while letting
// callers send exact decimal strings to avoid float round-tripping.
type decimalInput struct {
	// s is the trimmed textual form of the value, or "" when the field was
	// absent / JSON null.
	s string
	// set reports whether the field was present (and non-null) in the body.
	set bool
}

var errBadDecimal = errors.New("value must be a number or a decimal string")

// UnmarshalJSON accepts a JSON number or a JSON string and keeps its exact
// textual form. A JSON null leaves the value unset.
func (d *decimalInput) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		d.s, d.set = "", false
		return nil
	}
	if b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		d.s, d.set = strings.TrimSpace(str), true
		return nil
	}
	// A bare JSON number: keep its literal text so we never round-trip through
	// float64. json.Number validates it is a well-formed number.
	var num json.Number
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&num); err != nil {
		return errBadDecimal
	}
	d.s, d.set = strings.TrimSpace(num.String()), true
	return nil
}

// String returns the trimmed decimal text (possibly empty when unset).
func (d decimalInput) String() string { return d.s }

// Set reports whether the field was present and non-null.
func (d decimalInput) Set() bool { return d.set }

// Valid reports whether a present value parses as a non-negative decimal
// matching decimalPattern (the same shape money/price inputs already require).
func (d decimalInput) Valid() bool { return validDecimal(d.s) }

// ValueOr returns the decimal text when set+valid, else the supplied default.
func (d decimalInput) ValueOr(def string) string {
	if d.set && validDecimal(d.s) {
		return d.s
	}
	return def
}

// Ptr returns a *string of the decimal text when set+valid, else nil. Used for
// nullable numeric columns and partial (PATCH) updates.
func (d decimalInput) Ptr() *string {
	if d.set && validDecimal(d.s) {
		s := d.s
		return &s
	}
	return nil
}
