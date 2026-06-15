package reportbuilder

import (
	"errors"
	"fmt"
)

// Spec is the validated builder spec: a dataset key + chosen dimensions, measures
// (each with an agg), filters, sort and limit, plus a visualization hint. This is
// the ONLY thing the composer turns into SQL, and EVERY field is validated against
// the dataset's registry allowlist before composition (Validate below). It is
// exactly the jsonb persisted in report_templates.spec.
type Spec struct {
	Dataset    string        `json:"dataset"`
	Dimensions []string      `json:"dimensions"`
	Measures   []SpecMeasure `json:"measures"`
	Filters    []SpecFilter  `json:"filters"`
	Sort       []SpecSort    `json:"sort,omitempty"`
	Limit      int           `json:"limit,omitempty"`
	Viz        string        `json:"viz,omitempty"` // table | bar | line | pie (display hint only)
}

// SpecMeasure is one chosen measure + the agg to apply. Agg is validated against
// the measure's per-measure AllowedAggs.
type SpecMeasure struct {
	Measure string  `json:"measure"`
	Agg     AggFunc `json:"agg"`
}

// SpecFilter is one chosen filter: a filter column id, an operator (validated
// against the filter's allowed operators) and a VALUE that is always bound as a
// parameter — never concatenated into SQL. For "in" the value is a list; for
// "between" it is a [lo, hi] pair; otherwise a scalar.
type SpecFilter struct {
	Filter   string   `json:"filter"`
	Operator Operator `json:"operator"`
	Value    any      `json:"value,omitempty"`
	Values   []any    `json:"values,omitempty"` // for in / between
}

// SpecSort orders the result by a chosen dimension id or a measure alias. By is a
// dimension id OR a measure id (the composer resolves it to the SELECT alias);
// Desc toggles direction. The composer rejects a By that is not part of the spec's
// selected dims/measures, so sort cannot smuggle an un-selected identifier.
type SpecSort struct {
	By   string `json:"by"`
	Desc bool   `json:"desc,omitempty"`
}

// validVizzes is the display-hint allowlist (cosmetic; never affects SQL).
var validVizzes = map[string]bool{"": true, "table": true, "bar": true, "line": true, "pie": true}

// maxDimensions / maxMeasures / maxFilters / maxLimit bound a spec so a composed
// query stays cheap and the GROUP BY cannot explode. These are conservative.
const (
	maxDimensions = 4
	maxMeasures   = 8
	maxFilters    = 12
	maxLimit      = 1000
	defaultLimit  = 200
)

// ErrInvalidSpec is the sentinel for a spec that references an identifier / agg /
// operator outside the dataset allowlist, or is otherwise malformed. The handler
// maps it to a 400 with code "invalid_spec" and the message text.
var ErrInvalidSpec = errors.New("reportbuilder: invalid spec")

// specError wraps ErrInvalidSpec with a human message AND a stable machine code so
// the API can return a precise 400 (e.g. "unknown_dimension"). It always Is()
// ErrInvalidSpec.
type specError struct {
	Code string
	Msg  string
}

func (e specError) Error() string { return e.Msg }
func (e specError) Unwrap() error { return ErrInvalidSpec }

// SpecErrorCode extracts the machine code from a validation error (or "" if the
// error is not a specError).
func SpecErrorCode(err error) string {
	var se specError
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

func invalid(code, format string, a ...any) error {
	return specError{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// Validate checks the spec ENTIRELY against the dataset's registry allowlist. It
// is the allowlist gate: a spec that references any dimension / measure / filter /
// agg / operator NOT registered for the dataset is rejected with a precise code,
// and NO query is composed. On success it returns the resolved Dataset for the
// composer. This is where injection-y identifiers die — there is no path from a
// rejected identifier into SQL.
//
// It does NOT check permissions (the handler does that with the actor's policy);
// it only validates SHAPE + allowlist membership. Sensitive measures are allowed
// through here and FILTERED by the composer using the actor's allowSensitive flag.
func (s Spec) Validate() (Dataset, error) {
	ds, ok := ByKey(s.Dataset)
	if !ok {
		return Dataset{}, invalid("unknown_dataset", "unknown dataset %q", s.Dataset)
	}

	if len(s.Dimensions) == 0 && len(s.Measures) == 0 {
		return Dataset{}, invalid("empty_spec", "a spec needs at least one dimension or measure")
	}
	if len(s.Dimensions) > maxDimensions {
		return Dataset{}, invalid("too_many_dimensions", "at most %d dimensions are allowed", maxDimensions)
	}
	if len(s.Measures) > maxMeasures {
		return Dataset{}, invalid("too_many_measures", "at most %d measures are allowed", maxMeasures)
	}
	if len(s.Filters) > maxFilters {
		return Dataset{}, invalid("too_many_filters", "at most %d filters are allowed", maxFilters)
	}

	// Dimensions: each must be a registered dimension id; no duplicates.
	seenDim := map[string]bool{}
	for _, dimID := range s.Dimensions {
		if seenDim[dimID] {
			return Dataset{}, invalid("duplicate_dimension", "duplicate dimension %q", dimID)
		}
		seenDim[dimID] = true
		if _, ok := ds.dimByID(dimID); !ok {
			return Dataset{}, invalid("unknown_dimension", "dimension %q is not allowed for dataset %q", dimID, ds.Key)
		}
	}

	// Measures: each must be a registered measure id, and the agg must be in the
	// measure's per-measure allowlist.
	for _, m := range s.Measures {
		mdef, ok := ds.measureByID(m.Measure)
		if !ok {
			return Dataset{}, invalid("unknown_measure", "measure %q is not allowed for dataset %q", m.Measure, ds.Key)
		}
		if _, ok := sqlFunc[m.Agg]; !ok {
			return Dataset{}, invalid("unknown_agg", "aggregate %q is not allowed", m.Agg)
		}
		if !aggAllowed(mdef.AllowedAggs, m.Agg) {
			return Dataset{}, invalid("agg_not_allowed", "aggregate %q is not allowed for measure %q", m.Agg, m.Measure)
		}
	}

	// Filters: each must be a registered filter id with an allowed operator, and a
	// well-shaped value for that operator.
	for _, f := range s.Filters {
		fdef, ok := ds.filterByID(f.Filter)
		if !ok {
			return Dataset{}, invalid("unknown_filter", "filter %q is not allowed for dataset %q", f.Filter, ds.Key)
		}
		if !operatorAllowed(fdef.Operators, f.Operator) {
			return Dataset{}, invalid("operator_not_allowed", "operator %q is not allowed for filter %q", f.Operator, f.Filter)
		}
		if err := validateFilterValue(f); err != nil {
			return Dataset{}, err
		}
	}

	// Sort: each By must be a SELECTED dimension or measure (so a sort cannot
	// reference an un-selected — therefore un-validated — identifier).
	for _, srt := range s.Sort {
		if !seenDim[srt.By] && !measureSelected(s.Measures, srt.By) {
			return Dataset{}, invalid("unknown_sort", "sort %q must be one of the selected dimensions or measures", srt.By)
		}
	}

	if s.Limit < 0 || s.Limit > maxLimit {
		return Dataset{}, invalid("invalid_limit", "limit must be between 0 and %d", maxLimit)
	}
	if !validVizzes[s.Viz] {
		return Dataset{}, invalid("invalid_viz", "viz must be one of table|bar|line|pie")
	}

	return ds, nil
}

// validateFilterValue checks the value SHAPE for the operator (it does not bind —
// binding happens in the composer). "in" needs a non-empty Values list; "between"
// needs exactly two; everything else needs a scalar Value. The VALUES themselves
// are arbitrary and always parameter-bound, so no content check is needed for
// safety — only shape.
func validateFilterValue(f SpecFilter) error {
	switch f.Operator {
	case OpIn:
		if len(f.Values) == 0 {
			return invalid("filter_value", "operator \"in\" requires a non-empty values list for filter %q", f.Filter)
		}
		if len(f.Values) > 200 {
			return invalid("filter_value", "operator \"in\" allows at most 200 values for filter %q", f.Filter)
		}
	case OpBetween:
		if len(f.Values) != 2 {
			return invalid("filter_value", "operator \"between\" requires exactly two values for filter %q", f.Filter)
		}
	default:
		if f.Value == nil {
			return invalid("filter_value", "filter %q requires a value", f.Filter)
		}
	}
	return nil
}

func aggAllowed(allowed []AggFunc, a AggFunc) bool {
	for _, x := range allowed {
		if x == a {
			return true
		}
	}
	return false
}

func operatorAllowed(allowed []Operator, o Operator) bool {
	for _, x := range allowed {
		if x == o {
			return true
		}
	}
	return false
}

func measureSelected(ms []SpecMeasure, id string) bool {
	for _, m := range ms {
		if m.Measure == id {
			return true
		}
	}
	return false
}
