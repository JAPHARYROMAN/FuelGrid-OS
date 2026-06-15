package reportbuilder

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Composer turns a VALIDATED spec into a parameterized, tenant- and station-scoped
// SQL query and executes it read-only. It is the second half of the query-safety
// guarantee: registry.go owns the identifiers, spec.go proves the spec only uses
// allowlisted ones, and this file proves that (a) no identifier is ever taken from
// the wire (every SQL fragment comes from the resolved Dataset), (b) every filter
// VALUE is a bound $N parameter, and (c) tenant + station predicates are ALWAYS
// injected.

// ScopeContext is the actor's resolved scope the composer injects into every
// query. TenantID is mandatory. TenantWide=true means the actor reads every
// station (no station predicate). Otherwise StationIDs is the exact set of
// stations the actor may read; an empty set with TenantWide=false means "no
// station access" and the composer refuses to run (fail-closed). AllowSensitive
// gates the dataset's sensitive measures — when false they are omitted from the
// composed SELECT (not zeroed).
type ScopeContext struct {
	TenantID       uuid.UUID
	TenantWide     bool
	StationIDs     []uuid.UUID
	AllowSensitive bool
}

// Querier is the read-only query surface the composer needs (satisfied by
// *database.Pool and pgx.Tx). The composer never writes.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Column describes one output column of the composed result: a stable Key (the
// dim/measure id, suffixed with the agg for measures), a human Label, whether it
// is a Dimension (vs a measure), whether it is Decimal (money/litre → string), and
// its Unit. The result table/chart use these to render.
type Column struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Dimension bool   `json:"dimension"`
	Decimal   bool   `json:"decimal"`
	Unit      string `json:"unit,omitempty"`
}

// Result is the composed query's output: the resolved columns (in SELECT order)
// and the rows (every cell already a string — decimal money/litre via ::text,
// counts via ::text, dims as text). Decimal strings pass through verbatim; no
// money is ever parsed to float in the composer. OmittedSensitive lists the
// sensitive measure ids that were dropped because the actor lacked the gating
// permission, so the handler can surface an honest data-quality note.
type Result struct {
	Columns          []Column
	Rows             [][]string
	OmittedSensitive []string
}

// ErrNoStationAccess is returned when a station-scoped actor has no station grants
// and the dataset has a station axis — the composer refuses to run rather than
// fail open to every station.
var errNoStationAccess = invalid("no_station_access", "no station access for this dataset")

// Compose builds and runs the query for ds + spec under scope, returning the
// Result. ds MUST be the Dataset returned by spec.Validate (so identifiers are
// already proven allowlisted). It:
//  1. resolves the SELECT list from the spec's dims + non-omitted measures,
//  2. always prepends the tenant predicate (TenantColumn = $1) and, for a
//     station-scoped actor on a station-axis dataset, the station predicate
//     (StationColumn = ANY($n)) — a tenant-wide actor gets no station filter,
//  3. appends each filter as a parameterized predicate (value(s) bound as $N),
//  4. GROUPs BY the selected dimensions, applies the validated sort + a bounded
//     limit,
//  5. executes read-only and scans every cell to a string.
func Compose(ctx context.Context, q Querier, ds Dataset, spec Spec, scope ScopeContext) (Result, error) {
	if scope.TenantID == uuid.Nil {
		return Result{}, invalid("no_tenant", "a tenant is required")
	}
	// A station-axis dataset run by a station-scoped actor with no grants must not
	// fall open to the whole tenant.
	if ds.StationColumn != "" && !scope.TenantWide && len(scope.StationIDs) == 0 {
		return Result{}, errNoStationAccess
	}
	// A tenant-wide-only dataset (no station axis, e.g. audit) cannot be scoped to
	// a station-restricted actor — refuse it for them.
	if ds.StationColumn == "" && !scope.TenantWide {
		return Result{}, invalid("requires_tenant_wide", "dataset %q is tenant-wide and requires a tenant-wide role", ds.Key)
	}

	// args[0] is always the tenant id — $1.
	args := []any{scope.TenantID}

	// ---- SELECT list + columns (dims then measures) ----
	var selectParts []string
	var groupByOrdinals []string
	columns := make([]Column, 0, len(spec.Dimensions)+len(spec.Measures))
	aliasOf := map[string]string{} // dim/measure key -> SELECT alias

	ordinal := 0
	for _, dimID := range spec.Dimensions {
		dim, _ := ds.dimByID(dimID) // proven present by Validate
		ordinal++
		alias := fmt.Sprintf("d%d", ordinal)
		// dim.SQLExpr is a registry-owned literal; the alias is composer-owned.
		selectParts = append(selectParts, fmt.Sprintf("%s AS %s", dim.SQLExpr, alias))
		groupByOrdinals = append(groupByOrdinals, fmt.Sprintf("%d", ordinal))
		columns = append(columns, Column{Key: dim.ID, Label: dim.Label, Dimension: true, Unit: "", Decimal: false})
		aliasOf[dim.ID] = alias
	}

	var omitted []string
	for _, sm := range spec.Measures {
		mdef, _ := ds.measureByID(sm.Measure) // proven present by Validate
		// Sensitive gating: omit (not zero) when the actor lacks the permission.
		if mdef.Sensitive && !scope.AllowSensitive {
			omitted = append(omitted, mdef.ID)
			continue
		}
		ordinal++
		alias := fmt.Sprintf("m%d", ordinal)
		key := mdef.ID + "_" + string(sm.Agg)
		// Decimal money/litre → ::text so the wire carries an exact decimal string.
		// COUNT/SUM of "1" etc. also rendered ::text for a uniform string table.
		aggSQL := fmt.Sprintf("%s(%s)", sqlFunc[sm.Agg], mdef.SQLExpr)
		if sm.Agg == AggCount {
			// COUNT ignores its arg's value; count rows in the group.
			aggSQL = "COUNT(*)"
		}
		selectParts = append(selectParts, fmt.Sprintf("(%s)::text AS %s", aggSQL, alias))
		columns = append(columns, Column{
			Key: key, Label: mdef.Label + " (" + string(sm.Agg) + ")",
			Dimension: false, Decimal: mdef.Decimal, Unit: mdef.Unit,
		})
		aliasOf[key] = alias
		// A sort that targets the bare measure id resolves to its (single) agg alias.
		if _, exists := aliasOf[mdef.ID]; !exists {
			aliasOf[mdef.ID] = alias
		}
	}

	if len(selectParts) == 0 {
		// Every selected measure was sensitive-and-omitted and there were no dims:
		// nothing to return. Surface an honest, empty result rather than an invalid
		// SQL (SELECT with no columns).
		return Result{Columns: columns, Rows: [][]string{}, OmittedSensitive: omitted}, nil
	}

	// ---- WHERE: tenant predicate ALWAYS first, then station scope, then filters ----
	where := []string{fmt.Sprintf("%s = $1", ds.TenantColumn)}

	if ds.StationColumn != "" && !scope.TenantWide {
		args = append(args, uuidSlice(scope.StationIDs))
		where = append(where, fmt.Sprintf("%s = ANY($%d)", ds.StationColumn, len(args)))
	}

	for _, sf := range spec.Filters {
		fdef, _ := ds.filterByID(sf.Filter) // proven present by Validate
		pred, newArgs := bindFilter(fdef, sf, len(args))
		args = append(args, newArgs...)
		where = append(where, pred)
	}

	// ---- assemble ----
	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(selectParts, ", "))
	sb.WriteString(" FROM ")
	sb.WriteString(ds.From)
	sb.WriteString(" WHERE ")
	sb.WriteString(strings.Join(where, " AND "))
	if len(groupByOrdinals) > 0 {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(strings.Join(groupByOrdinals, ", "))
	}
	if orderBy := buildOrderBy(spec, aliasOf, groupByOrdinals); orderBy != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(orderBy)
	}
	limit := spec.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	fmt.Fprintf(&sb, " LIMIT %d", limit)

	rows, err := q.Query(ctx, sb.String(), args...)
	if err != nil {
		return Result{}, fmt.Errorf("reportbuilder: query: %w", err)
	}
	defer rows.Close()

	out := Result{Columns: columns, Rows: [][]string{}, OmittedSensitive: omitted}
	colN := len(columns)
	for rows.Next() {
		// Every column is selected ::text (measures) or already text (dims), so scan
		// into *string. A NULL aggregate (empty group) scans to "" → rendered "0"
		// for decimals so the table never shows a blank money cell.
		cells := make([]*string, colN)
		ptrs := make([]any, colN)
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return Result{}, fmt.Errorf("reportbuilder: scan: %w", err)
		}
		row := make([]string, colN)
		for i := range cells {
			if cells[i] == nil {
				if columns[i].Decimal {
					row[i] = "0"
				} else {
					row[i] = ""
				}
				continue
			}
			row[i] = *cells[i]
		}
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("reportbuilder: rows: %w", err)
	}
	return out, nil
}

// bindFilter renders ONE filter predicate using the registry-owned fdef.SQLExpr
// (never a wire identifier) and binds the filter VALUE(s) as $N parameters
// starting after baseN. The value is NEVER concatenated into the SQL — a value
// like `x'); DROP TABLE ...` is a harmless literal string compared by the bound
// parameter. Returns the predicate and the args to append.
func bindFilter(fdef Filter, sf SpecFilter, baseN int) (string, []any) {
	col := fdef.SQLExpr
	switch sf.Operator {
	case OpEq:
		return fmt.Sprintf("%s = $%d", col, baseN+1), []any{coerce(fdef.Type, sf.Value)}
	case OpNe:
		return fmt.Sprintf("%s <> $%d", col, baseN+1), []any{coerce(fdef.Type, sf.Value)}
	case OpGt:
		return fmt.Sprintf("%s > $%d", col, baseN+1), []any{coerce(fdef.Type, sf.Value)}
	case OpGte:
		return fmt.Sprintf("%s >= $%d", col, baseN+1), []any{coerce(fdef.Type, sf.Value)}
	case OpLt:
		return fmt.Sprintf("%s < $%d", col, baseN+1), []any{coerce(fdef.Type, sf.Value)}
	case OpLte:
		return fmt.Sprintf("%s <= $%d", col, baseN+1), []any{coerce(fdef.Type, sf.Value)}
	case OpLike:
		// ILIKE with the value wrapped %...%; the % wrapping is on the bound VALUE,
		// not the SQL, so still fully parameterized.
		return fmt.Sprintf("%s ILIKE $%d", col, baseN+1), []any{"%" + asString(sf.Value) + "%"}
	case OpIn:
		vals := make([]any, 0, len(sf.Values))
		for _, v := range sf.Values {
			vals = append(vals, coerce(fdef.Type, v))
		}
		return fmt.Sprintf("%s = ANY($%d)", col, baseN+1), []any{vals}
	case OpBetween:
		lo := coerce(fdef.Type, sf.Values[0])
		hi := coerce(fdef.Type, sf.Values[1])
		return fmt.Sprintf("%s BETWEEN $%d AND $%d", col, baseN+1, baseN+2), []any{lo, hi}
	}
	// Unreachable: Validate already rejected any operator outside the allowlist.
	return "FALSE", nil
}

// coerce converts a JSON-decoded value to a Go type pgx binds appropriately for
// the column type. UUID/date/text/numeric all bind safely as their natural type;
// when in doubt we pass a string and let Postgres cast on the typed column. The
// value is ALWAYS a bound parameter regardless, so coercion is about correctness,
// not safety.
func coerce(t ColumnType, v any) any {
	switch t {
	case TypeInt:
		if f, ok := v.(float64); ok {
			return int64(f)
		}
	case TypeBool:
		if b, ok := v.(bool); ok {
			return b
		}
	}
	// text / uuid / date / numeric: bind the string form. pgx + the typed column
	// cast it; an invalid cast is a clean query error, never an injection.
	return asString(v)
}

// asString renders a JSON scalar as its string form for binding.
func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		// Render integers without a trailing ".0"; keep fractional precision.
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", x)
	}
}

// uuidSlice converts station ids to a []uuid.UUID pgx binds as a uuid[] for the
// ANY() station predicate.
func uuidSlice(ids []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, len(ids))
	copy(out, ids)
	return out
}

// buildOrderBy resolves the validated sort onto SELECT aliases. Each By was proven
// by Validate to be a selected dim/measure, so it resolves to a known alias; an
// unresolved By (e.g. a measure that was sensitive-omitted) is skipped. With no
// sort, results order by the first dimension for a stable display.
func buildOrderBy(spec Spec, aliasOf map[string]string, groupByOrdinals []string) string {
	var parts []string
	for _, srt := range spec.Sort {
		alias, ok := aliasOf[srt.By]
		if !ok {
			continue // omitted (e.g. sensitive measure dropped) — skip, never inject
		}
		dir := "ASC"
		if srt.Desc {
			dir = "DESC"
		}
		parts = append(parts, alias+" "+dir)
	}
	if len(parts) == 0 && len(groupByOrdinals) > 0 {
		// Stable default: order by the grouped dimensions.
		return strings.Join(groupByOrdinals, ", ")
	}
	return strings.Join(parts, ", ")
}
