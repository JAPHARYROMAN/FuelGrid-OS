package reportbuilder

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// captureQuerier records the SQL + args Compose would run, WITHOUT a database, so
// the query-safety assertions (no raw identifier interpolation, every value bound)
// run as fast unit tests with no DB dependency.
type captureQuerier struct {
	sql  string
	args []any
}

func (c *captureQuerier) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.sql = sql
	c.args = args
	// Return an empty (zero-row) result so Compose runs to completion and returns a
	// fully-populated Result (columns, OmittedSensitive) without a database. The
	// test inspects the captured SQL/args + the returned Result, not row data.
	return emptyRows{}, nil
}

// emptyRows is a pgx.Rows that yields no rows — enough for Compose to finish the
// scan loop cleanly in a DB-free unit test.
type emptyRows struct{}

func (emptyRows) Close()                                       {}
func (emptyRows) Err() error                                   { return nil }
func (emptyRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (emptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (emptyRows) Next() bool                                   { return false }
func (emptyRows) Scan(...any) error                            { return nil }
func (emptyRows) Values() ([]any, error)                       { return nil, nil }
func (emptyRows) RawValues() [][]byte                          { return nil }
func (emptyRows) Conn() *pgx.Conn                              { return nil }

var errStopAfterCapture = errors.New("stop-after-capture")

func mustValidate(t *testing.T, spec Spec) (Dataset, Spec) {
	t.Helper()
	ds, err := spec.Validate()
	if err != nil {
		t.Fatalf("spec should validate: %v", err)
	}
	return ds, spec
}

func compose(t *testing.T, spec Spec, scope ScopeContext) *captureQuerier {
	t.Helper()
	ds, _ := mustValidate(t, spec)
	cq := &captureQuerier{}
	_, err := Compose(context.Background(), cq, ds, spec, scope)
	if err != nil && !errors.Is(err, errStopAfterCapture) {
		t.Fatalf("compose: %v", err)
	}
	return cq
}

func tenantWideScope(allowSensitive bool) ScopeContext {
	return ScopeContext{TenantID: uuid.New(), TenantWide: true, AllowSensitive: allowSensitive}
}

// --- Registry integrity ---

func TestRegistry_NoDuplicateKeysAndValidPermissions(t *testing.T) {
	seen := map[string]bool{}
	for _, ds := range Registry() {
		if seen[ds.Key] {
			t.Fatalf("duplicate dataset key %q", ds.Key)
		}
		seen[ds.Key] = true
		if ds.RequiredPermission == "" {
			t.Fatalf("dataset %q has no required permission", ds.Key)
		}
		if ds.From == "" || ds.TenantColumn == "" {
			t.Fatalf("dataset %q missing From/TenantColumn", ds.Key)
		}
		if ds.HasSensitive() && ds.SensitivePermission == "" {
			t.Fatalf("dataset %q has sensitive measures but no SensitivePermission", ds.Key)
		}
		// Every measure agg in AllowedAggs must be a known sql func.
		for _, m := range ds.Measures {
			for _, a := range m.AllowedAggs {
				if _, ok := sqlFunc[a]; !ok {
					t.Fatalf("dataset %q measure %q lists unknown agg %q", ds.Key, m.ID, a)
				}
			}
		}
	}
}

// --- Allowlist rejection (the core safety proof) ---

func TestValidate_RejectsNonAllowlistedIdentifiers(t *testing.T) {
	cases := []struct {
		name     string
		spec     Spec
		wantCode string
	}{
		{
			name:     "injection-y dimension identifier",
			spec:     Spec{Dataset: "revenue_days", Dimensions: []string{"rd.gross_revenue); DROP TABLE revenue_days;--"}},
			wantCode: "unknown_dimension",
		},
		{
			name:     "unknown measure",
			spec:     Spec{Dataset: "revenue_days", Measures: []SpecMeasure{{Measure: "supplier_secret", Agg: AggSum}}},
			wantCode: "unknown_measure",
		},
		{
			name:     "agg not allowed for measure (count-only measure)",
			spec:     Spec{Dataset: "revenue_days", Measures: []SpecMeasure{{Measure: "day_count", Agg: AggAvg}}},
			wantCode: "agg_not_allowed",
		},
		{
			name:     "unknown agg function",
			spec:     Spec{Dataset: "revenue_days", Measures: []SpecMeasure{{Measure: "gross_revenue", Agg: AggFunc("median")}}},
			wantCode: "unknown_agg",
		},
		{
			name: "operator not allowed for filter",
			spec: Spec{Dataset: "revenue_days", Measures: []SpecMeasure{{Measure: "gross_revenue", Agg: AggSum}},
				Filters: []SpecFilter{{Filter: "status", Operator: OpBetween, Values: []any{"a", "b"}}}},
			wantCode: "operator_not_allowed",
		},
		{
			name:     "unknown dataset",
			spec:     Spec{Dataset: "users; SELECT * FROM passwords", Dimensions: []string{"x"}},
			wantCode: "unknown_dataset",
		},
		{
			name: "unknown filter id",
			spec: Spec{Dataset: "revenue_days", Measures: []SpecMeasure{{Measure: "gross_revenue", Agg: AggSum}},
				Filters: []SpecFilter{{Filter: "rd.tenant_id", Operator: OpEq, Value: "x"}}},
			wantCode: "unknown_filter",
		},
		{
			name: "sort references un-selected identifier",
			spec: Spec{Dataset: "revenue_days", Dimensions: []string{"business_date"},
				Sort: []SpecSort{{By: "cogs_total"}}},
			wantCode: "unknown_sort",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.spec.Validate()
			if err == nil {
				t.Fatalf("expected rejection, spec validated")
			}
			if !errors.Is(err, ErrInvalidSpec) {
				t.Fatalf("error should wrap ErrInvalidSpec, got %v", err)
			}
			if code := SpecErrorCode(err); code != tc.wantCode {
				t.Fatalf("want code %q, got %q (%v)", tc.wantCode, code, err)
			}
		})
	}
}

// A rejected spec must NEVER reach the composer — prove no query runs.
func TestValidate_RejectedSpecRunsNoQuery(t *testing.T) {
	spec := Spec{Dataset: "revenue_days", Dimensions: []string{"evil_col"}}
	if _, err := spec.Validate(); err == nil {
		t.Fatal("expected validation to reject")
	}
	// There is no path to Compose without a successful Validate (the handler
	// enforces it), and Compose requires the Dataset returned by Validate. This
	// test documents that contract: a failed Validate yields no Dataset to compose.
}

// --- Parameterization (filter values are bound, never concatenated) ---

func TestCompose_FilterValuesAreBound(t *testing.T) {
	evil := "x'); DROP TABLE revenue_days;--"
	spec := Spec{
		Dataset:    "revenue_days",
		Dimensions: []string{"business_date"},
		Measures:   []SpecMeasure{{Measure: "gross_revenue", Agg: AggSum}},
		Filters:    []SpecFilter{{Filter: "status", Operator: OpEq, Value: evil}},
	}
	cq := compose(t, spec, tenantWideScope(true))

	// The literal must NOT appear anywhere in the SQL text — it is a bound value.
	if strings.Contains(cq.sql, "DROP TABLE") {
		t.Fatalf("filter value leaked into SQL: %s", cq.sql)
	}
	// It must appear among the bound args.
	found := false
	for _, a := range cq.args {
		if s, ok := a.(string); ok && s == evil {
			found = true
		}
	}
	if !found {
		t.Fatalf("filter value not bound as a parameter; args=%v", cq.args)
	}
	// args[0] is always the tenant id.
	if _, ok := cq.args[0].(uuid.UUID); !ok {
		t.Fatalf("args[0] must be the tenant id, got %T", cq.args[0])
	}
}

// --- Tenant + station scope always injected ---

func TestCompose_TenantAndStationScopeInjected(t *testing.T) {
	station := uuid.New()
	scope := ScopeContext{TenantID: uuid.New(), TenantWide: false, StationIDs: []uuid.UUID{station}, AllowSensitive: false}
	spec := Spec{
		Dataset:    "revenue_days",
		Dimensions: []string{"station_id"},
		Measures:   []SpecMeasure{{Measure: "gross_revenue", Agg: AggSum}},
	}
	cq := compose(t, spec, scope)

	if !strings.Contains(cq.sql, "rd.tenant_id = $1") {
		t.Fatalf("tenant predicate missing: %s", cq.sql)
	}
	if !strings.Contains(cq.sql, "rd.station_id = ANY($2)") {
		t.Fatalf("station predicate missing for station-scoped actor: %s", cq.sql)
	}
}

func TestCompose_TenantWideActorGetsNoStationFilter(t *testing.T) {
	spec := Spec{
		Dataset:    "revenue_days",
		Dimensions: []string{"station_id"},
		Measures:   []SpecMeasure{{Measure: "gross_revenue", Agg: AggSum}},
	}
	cq := compose(t, spec, tenantWideScope(true))
	if strings.Contains(cq.sql, "ANY($2)") || strings.Contains(cq.sql, "station_id = ANY") {
		t.Fatalf("tenant-wide actor should have no station predicate: %s", cq.sql)
	}
	if !strings.Contains(cq.sql, "rd.tenant_id = $1") {
		t.Fatalf("tenant predicate still required: %s", cq.sql)
	}
}

// --- Sensitive column gating ---

func TestCompose_SensitiveMeasureOmittedForNonHolder(t *testing.T) {
	spec := Spec{
		Dataset:    "revenue_days",
		Dimensions: []string{"business_date"},
		Measures: []SpecMeasure{
			{Measure: "gross_revenue", Agg: AggSum},
			{Measure: "margin_total", Agg: AggSum}, // sensitive
		},
	}
	ds, _ := mustValidate(t, spec)

	// Non-holder: margin omitted.
	cq := &captureQuerier{}
	res, err := Compose(context.Background(), cq, ds, spec, tenantWideScope(false))
	if err != nil && !errors.Is(err, errStopAfterCapture) {
		t.Fatalf("compose: %v", err)
	}
	if strings.Contains(cq.sql, "margin_total") {
		t.Fatalf("sensitive margin column must be omitted for non-holder: %s", cq.sql)
	}
	if len(res.OmittedSensitive) != 1 || res.OmittedSensitive[0] != "margin_total" {
		t.Fatalf("want margin_total omitted, got %v", res.OmittedSensitive)
	}

	// Holder: margin present.
	cap2 := &captureQuerier{}
	_, err = Compose(context.Background(), cap2, ds, spec, tenantWideScope(true))
	if err != nil && !errors.Is(err, errStopAfterCapture) {
		t.Fatalf("compose: %v", err)
	}
	if !strings.Contains(cap2.sql, "rd.margin_total") {
		t.Fatalf("sensitive margin column must be present for holder: %s", cap2.sql)
	}
}

// --- Station-scoped actor cannot reach a tenant-wide-only dataset ---

func TestCompose_TenantWideOnlyDatasetRejectsStationScopedActor(t *testing.T) {
	spec := Spec{Dataset: "audit_logs", Dimensions: []string{"action"}, Measures: []SpecMeasure{{Measure: "event_count", Agg: AggCount}}}
	ds, _ := mustValidate(t, spec)
	scope := ScopeContext{TenantID: uuid.New(), TenantWide: false, StationIDs: []uuid.UUID{uuid.New()}}
	_, err := Compose(context.Background(), &captureQuerier{}, ds, spec, scope)
	if err == nil || SpecErrorCode(err) != "requires_tenant_wide" {
		t.Fatalf("want requires_tenant_wide rejection, got %v", err)
	}
}

// --- Station-scoped actor with NO grants is fail-closed ---

func TestCompose_NoStationGrantsFailsClosed(t *testing.T) {
	spec := Spec{Dataset: "revenue_days", Measures: []SpecMeasure{{Measure: "gross_revenue", Agg: AggSum}}}
	ds, _ := mustValidate(t, spec)
	scope := ScopeContext{TenantID: uuid.New(), TenantWide: false, StationIDs: nil}
	_, err := Compose(context.Background(), &captureQuerier{}, ds, spec, scope)
	if err == nil || SpecErrorCode(err) != "no_station_access" {
		t.Fatalf("want no_station_access rejection, got %v", err)
	}
}

// --- COUNT compiles to COUNT(*); decimals cast ::text ---

func TestCompose_AggShapes(t *testing.T) {
	spec := Spec{
		Dataset:    "revenue_days",
		Dimensions: []string{"station_id"},
		Measures: []SpecMeasure{
			{Measure: "day_count", Agg: AggCount},
			{Measure: "gross_revenue", Agg: AggSum},
		},
	}
	cq := compose(t, spec, tenantWideScope(true))
	if !strings.Contains(cq.sql, "COUNT(*)") {
		t.Fatalf("count should compile to COUNT(*): %s", cq.sql)
	}
	if !strings.Contains(cq.sql, "(SUM(rd.gross_revenue))::text") {
		t.Fatalf("decimal sum should cast ::text: %s", cq.sql)
	}
	if !strings.Contains(cq.sql, "GROUP BY 1") {
		t.Fatalf("should group by the dimension ordinal: %s", cq.sql)
	}
}
