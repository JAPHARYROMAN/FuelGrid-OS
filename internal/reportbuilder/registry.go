// Package reportbuilder is the curated, whitelisted dataset REGISTRY and the
// SAFE query composer behind the Custom Report Builder (Reports Center Phase 11 —
// blueprint §6 "Custom Report Builder", §22 report_templates).
//
// QUERY-SAFETY IS THE DEFINING REQUIREMENT. This is a live multi-tenant money
// system, so there is NEVER any free / raw SQL from the client. A builder spec
// only ever references identifiers that exist in this registry: a dataset key, a
// subset of its WHITELISTED dimensions (groupable columns), WHITELISTED measures
// (aggregatable numeric columns + a fixed agg-fn allowlist), WHITELISTED filters
// (column + allowed operators) and a sort + limit. Every identifier the user
// sends is validated against the dataset allowlist and REJECTED if absent — no
// identifier is ever interpolated from raw user input. Filter VALUES are always
// bound as $N parameters, never string-concatenated. Aggregate functions come
// from a fixed allowlist (sum/avg/count/min/max). The composer (composer.go)
// always injects tenant_id + station-scope predicates and omits SENSITIVE columns
// (margin / supplier cost / credit exposure) for an actor without the gating
// permission. Money/litre measures render as decimal STRINGS (numeric::text).
//
// The registry is the ONLY source of allowed identifiers. Adding a column to a
// report means adding it here, reviewed — never accepting it from the wire.
package reportbuilder

// AggFunc is one of the fixed, allowlisted aggregate functions a measure may use.
// This is the COMPLETE set the composer will ever emit — there is no path to any
// other SQL function from a builder spec.
type AggFunc string

const (
	AggSum   AggFunc = "sum"
	AggAvg   AggFunc = "avg"
	AggCount AggFunc = "count"
	AggMin   AggFunc = "min"
	AggMax   AggFunc = "max"
)

// allAggFuncs is the canonical agg allowlist. sqlFunc maps each to its Postgres
// function name; anything not in this map is rejected before composition.
var sqlFunc = map[AggFunc]string{
	AggSum:   "SUM",
	AggAvg:   "AVG",
	AggCount: "COUNT",
	AggMin:   "MIN",
	AggMax:   "MAX",
}

// Operator is one of the fixed, allowlisted filter operators. Each maps to a
// parameterized SQL fragment in the composer — the VALUE is always a bound $N,
// never concatenated.
type Operator string

const (
	OpEq      Operator = "eq"      // column = $n
	OpNe      Operator = "ne"      // column <> $n
	OpGt      Operator = "gt"      // column > $n
	OpGte     Operator = "gte"     // column >= $n
	OpLt      Operator = "lt"      // column < $n
	OpLte     Operator = "lte"     // column <= $n
	OpIn      Operator = "in"      // column = ANY($n)   (value is a list)
	OpLike    Operator = "like"    // column ILIKE $n     (value wrapped %...% by the composer)
	OpBetween Operator = "between" // column BETWEEN $n AND $n+1 (value is a [lo,hi] pair)
)

// ColumnType constrains how a filter value is parsed + bound and how a dimension
// renders. It also picks the SQL cast used when binding a parameter.
type ColumnType string

const (
	TypeText    ColumnType = "text"
	TypeUUID    ColumnType = "uuid"
	TypeDate    ColumnType = "date"
	TypeNumeric ColumnType = "numeric"
	TypeInt     ColumnType = "int"
	TypeBool    ColumnType = "bool"
)

// Dimension is a groupable column. SQLExpr is the EXACT, registry-controlled SQL
// expression that produces the group key (always a literal from this file — never
// from the wire). Label is the human name; ID is the stable key the wire uses.
type Dimension struct {
	ID      string     `json:"id"`
	Label   string     `json:"label"`
	SQLExpr string     `json:"-"` // controlled SQL; never serialized to the client
	Type    ColumnType `json:"type"`
}

// Measure is an aggregatable numeric column. SQLExpr is the controlled column
// expression the agg wraps. AllowedAggs is the per-measure agg allowlist (a money
// total typically allows sum/avg/min/max; a row count is count-only). Decimal is
// true for money/litre measures so the composer casts the aggregate to ::text and
// returns a decimal STRING. Sensitive marks margin / cost / exposure measures that
// require RequiredPermission (the dataset's SensitivePermission) to be returned —
// they are OMITTED (not zeroed) for a non-holder.
type Measure struct {
	ID          string    `json:"id"`
	Label       string    `json:"label"`
	SQLExpr     string    `json:"-"`
	AllowedAggs []AggFunc `json:"allowed_aggs"`
	Decimal     bool      `json:"decimal"`
	Unit        string    `json:"unit,omitempty"`
	Sensitive   bool      `json:"sensitive"`
}

// Filter is a filterable column with its allowed operators. SQLExpr is the
// controlled column expression the predicate compares; the VALUE is always bound.
type Filter struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	SQLExpr   string     `json:"-"`
	Type      ColumnType `json:"type"`
	Operators []Operator `json:"operators"`
}

// Dataset is one curated, queryable source. From is the controlled FROM clause
// (a real table/view + its alias — always a literal here). TenantColumn and
// StationColumn name the alias-qualified columns the composer injects the
// tenant + station-scope predicates on (StationColumn empty = the dataset has no
// station axis, so station scope cannot be enforced and the dataset is therefore
// tenant-wide-only and must require a tenant-wide permission). RequiredPermission
// gates the whole dataset; SensitivePermission gates its Sensitive measures.
type Dataset struct {
	Key                 string      `json:"key"`
	Name                string      `json:"name"`
	Description         string      `json:"description"`
	RequiredPermission  string      `json:"required_permission"`
	SensitivePermission string      `json:"sensitive_permission,omitempty"`
	From                string      `json:"-"` // controlled FROM clause; never from the wire
	TenantColumn        string      `json:"-"`
	StationColumn       string      `json:"-"` // "" => no station axis (tenant-wide only)
	Dimensions          []Dimension `json:"dimensions"`
	Measures            []Measure   `json:"measures"`
	Filters             []Filter    `json:"filters"`
}

// dimByID / measureByID / filterByID give O(1) allowlist lookups. A spec
// identifier absent from these maps is REJECTED — this is the allowlist gate.
func (d Dataset) dimByID(id string) (Dimension, bool) {
	for i := range d.Dimensions {
		if d.Dimensions[i].ID == id {
			return d.Dimensions[i], true
		}
	}
	return Dimension{}, false
}

func (d Dataset) measureByID(id string) (Measure, bool) {
	for i := range d.Measures {
		if d.Measures[i].ID == id {
			return d.Measures[i], true
		}
	}
	return Measure{}, false
}

func (d Dataset) filterByID(id string) (Filter, bool) {
	for i := range d.Filters {
		if d.Filters[i].ID == id {
			return d.Filters[i], true
		}
	}
	return Filter{}, false
}

// HasSensitive reports whether any of the dataset's measures is sensitive (so the
// dataset's SensitivePermission is meaningful).
func (d Dataset) HasSensitive() bool {
	for i := range d.Measures {
		if d.Measures[i].Sensitive {
			return true
		}
	}
	return false
}

// moneyAggs / litreAggs / countAggs are the common per-measure agg allowlists.
var (
	moneyAggs = []AggFunc{AggSum, AggAvg, AggMin, AggMax}
	countAggs = []AggFunc{AggCount, AggSum, AggAvg, AggMin, AggMax}
)

// Registry is the fixed set of curated datasets — the SINGLE source of allowed
// identifiers. Every From/SQLExpr/TenantColumn/StationColumn below is a literal
// owned by this file; none is ever taken from a request.
//
// Conventions:
//   - Money/litre measures set Decimal:true so the composer casts ::text.
//   - Sensitive measures (margin / cost / exposure / profit) set Sensitive:true and
//     are gated by the dataset's SensitivePermission ("margin.view").
//   - A dataset with a StationColumn is station-scoped (the composer injects the
//     actor's station-grant predicate); a dataset WITHOUT one (audit) is tenant-
//     wide only and requires a tenant-wide permission.
func Registry() []Dataset {
	return []Dataset{
		revenueDaysDataset(),
		deliveriesDataset(),
		stockMovementsDataset(),
		riskAlertsDataset(),
		receivablesDataset(),
		shiftsDataset(),
		cashDataset(),
		auditDataset(),
	}
}

// ByKey returns the dataset with the given key, or (Dataset{}, false).
func ByKey(key string) (Dataset, bool) {
	for _, d := range Registry() {
		if d.Key == key {
			return d, true
		}
	}
	return Dataset{}, false
}

// revenueDaysDataset: the per-station, per-business-day revenue rollup
// (revenue_days). The signature sales/finance dataset. Margin / COGS are
// supplier-cost-derived and SENSITIVE (margin.view).
func revenueDaysDataset() Dataset {
	return Dataset{
		Key:                 "revenue_days",
		Name:                "Daily Revenue",
		Description:         "Per-station, per-day revenue: gross/net revenue, tender mix, cash variance, and (gated) margin & COGS.",
		RequiredPermission:  "revenue.read",
		SensitivePermission: "margin.view",
		From:                "revenue_days rd",
		TenantColumn:        "rd.tenant_id",
		StationColumn:       "rd.station_id",
		Dimensions: []Dimension{
			{ID: "business_date", Label: "Business date", SQLExpr: "rd.business_date::text", Type: TypeDate},
			{ID: "station_id", Label: "Station", SQLExpr: "rd.station_id::text", Type: TypeUUID},
			{ID: "status", Label: "Lock status", SQLExpr: "rd.status", Type: TypeText},
		},
		Measures: []Measure{
			{ID: "gross_revenue", Label: "Gross revenue", SQLExpr: "rd.gross_revenue", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS"},
			{ID: "net_revenue", Label: "Net revenue", SQLExpr: "rd.net_revenue", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS"},
			{ID: "tax_total", Label: "Tax", SQLExpr: "rd.tax_total", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS"},
			{ID: "cash_total", Label: "Cash tender", SQLExpr: "rd.cash_total", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS"},
			{ID: "mobile_money_total", Label: "Mobile money", SQLExpr: "rd.mobile_money_total", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS"},
			{ID: "card_total", Label: "Card", SQLExpr: "rd.card_total", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS"},
			{ID: "credit_total", Label: "Credit", SQLExpr: "rd.credit_total", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS"},
			{ID: "cash_variance", Label: "Cash variance", SQLExpr: "rd.cash_variance", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS"},
			{ID: "day_count", Label: "Days", SQLExpr: "1", AllowedAggs: []AggFunc{AggCount, AggSum}, Decimal: false, Unit: "count"},
			// SENSITIVE: supplier-cost-derived. Gated by margin.view; omitted for a
			// non-holder (never zeroed).
			{ID: "margin_total", Label: "Gross margin", SQLExpr: "rd.margin_total", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS", Sensitive: true},
			{ID: "cogs_total", Label: "Cost of goods sold", SQLExpr: "rd.cogs_total", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS", Sensitive: true},
		},
		Filters: []Filter{
			{ID: "business_date", Label: "Business date", SQLExpr: "rd.business_date", Type: TypeDate, Operators: []Operator{OpEq, OpGte, OpLte, OpBetween}},
			{ID: "station_id", Label: "Station", SQLExpr: "rd.station_id", Type: TypeUUID, Operators: []Operator{OpEq, OpIn}},
			{ID: "status", Label: "Lock status", SQLExpr: "rd.status", Type: TypeText, Operators: []Operator{OpEq, OpNe}},
		},
	}
}

// deliveriesDataset: fuel deliveries joined to their tank's station, so the
// dataset is station-scoped. Litres only (no money) — no sensitive columns.
func deliveriesDataset() Dataset {
	return Dataset{
		Key:                "deliveries",
		Name:               "Fuel Deliveries",
		Description:        "Fuel deliveries received per station/tank: volume litres and dip-before/after variance.",
		RequiredPermission: "station.read",
		From:               "deliveries dl JOIN tanks tk ON tk.id = dl.tank_id AND tk.tenant_id = dl.tenant_id",
		TenantColumn:       "dl.tenant_id",
		StationColumn:      "tk.station_id",
		Dimensions: []Dimension{
			{ID: "received_date", Label: "Received date", SQLExpr: "(dl.received_at AT TIME ZONE 'UTC')::date::text", Type: TypeDate},
			{ID: "station_id", Label: "Station", SQLExpr: "tk.station_id::text", Type: TypeUUID},
			{ID: "tank_id", Label: "Tank", SQLExpr: "dl.tank_id::text", Type: TypeUUID},
			{ID: "product_id", Label: "Product", SQLExpr: "tk.product_id::text", Type: TypeUUID},
			{ID: "supplier_ref", Label: "Supplier ref", SQLExpr: "COALESCE(dl.supplier_ref,'')", Type: TypeText},
		},
		Measures: []Measure{
			{ID: "volume_litres", Label: "Volume delivered", SQLExpr: "dl.volume_litres", AllowedAggs: moneyAggs, Decimal: true, Unit: "L"},
			{ID: "dip_variance_litres", Label: "Dip variance", SQLExpr: "COALESCE(dl.dip_variance_litres,0)", AllowedAggs: moneyAggs, Decimal: true, Unit: "L"},
			{ID: "delivery_count", Label: "Deliveries", SQLExpr: "1", AllowedAggs: []AggFunc{AggCount, AggSum}, Decimal: false, Unit: "count"},
		},
		Filters: []Filter{
			{ID: "received_date", Label: "Received date", SQLExpr: "(dl.received_at AT TIME ZONE 'UTC')::date", Type: TypeDate, Operators: []Operator{OpEq, OpGte, OpLte, OpBetween}},
			{ID: "station_id", Label: "Station", SQLExpr: "tk.station_id", Type: TypeUUID, Operators: []Operator{OpEq, OpIn}},
			{ID: "tank_id", Label: "Tank", SQLExpr: "dl.tank_id", Type: TypeUUID, Operators: []Operator{OpEq, OpIn}},
			{ID: "product_id", Label: "Product", SQLExpr: "tk.product_id", Type: TypeUUID, Operators: []Operator{OpEq, OpIn}},
			{ID: "supplier_ref", Label: "Supplier ref", SQLExpr: "COALESCE(dl.supplier_ref,'')", Type: TypeText, Operators: []Operator{OpEq, OpLike}},
		},
	}
}

// stockMovementsDataset: the tank stock ledger joined to its tank's station.
// Posted movements only by default is the operator's choice via a status filter.
func stockMovementsDataset() Dataset {
	return Dataset{
		Key:                "stock_movements",
		Name:               "Stock Movements",
		Description:        "Tank stock ledger: litres in/out by movement type, per station/tank.",
		RequiredPermission: "reconciliation.read",
		From:               "stock_movements sm JOIN tanks tk ON tk.id = sm.tank_id AND tk.tenant_id = sm.tenant_id",
		TenantColumn:       "sm.tenant_id",
		StationColumn:      "tk.station_id",
		Dimensions: []Dimension{
			{ID: "recorded_date", Label: "Recorded date", SQLExpr: "(sm.recorded_at AT TIME ZONE 'UTC')::date::text", Type: TypeDate},
			{ID: "station_id", Label: "Station", SQLExpr: "tk.station_id::text", Type: TypeUUID},
			{ID: "tank_id", Label: "Tank", SQLExpr: "sm.tank_id::text", Type: TypeUUID},
			{ID: "product_id", Label: "Product", SQLExpr: "tk.product_id::text", Type: TypeUUID},
			{ID: "movement_type", Label: "Movement type", SQLExpr: "sm.movement_type", Type: TypeText},
			{ID: "status", Label: "Status", SQLExpr: "sm.status", Type: TypeText},
		},
		Measures: []Measure{
			{ID: "litres", Label: "Litres moved", SQLExpr: "sm.litres", AllowedAggs: moneyAggs, Decimal: true, Unit: "L"},
			{ID: "movement_count", Label: "Movements", SQLExpr: "1", AllowedAggs: []AggFunc{AggCount, AggSum}, Decimal: false, Unit: "count"},
		},
		Filters: []Filter{
			{ID: "recorded_date", Label: "Recorded date", SQLExpr: "(sm.recorded_at AT TIME ZONE 'UTC')::date", Type: TypeDate, Operators: []Operator{OpEq, OpGte, OpLte, OpBetween}},
			{ID: "station_id", Label: "Station", SQLExpr: "tk.station_id", Type: TypeUUID, Operators: []Operator{OpEq, OpIn}},
			{ID: "tank_id", Label: "Tank", SQLExpr: "sm.tank_id", Type: TypeUUID, Operators: []Operator{OpEq, OpIn}},
			{ID: "movement_type", Label: "Movement type", SQLExpr: "sm.movement_type", Type: TypeText, Operators: []Operator{OpEq, OpNe, OpIn}},
			{ID: "status", Label: "Status", SQLExpr: "sm.status", Type: TypeText, Operators: []Operator{OpEq, OpNe}},
		},
	}
}

// riskAlertsDataset: risk alerts, station-scoped via station_id (nullable — a
// tenant-wide alert has NULL station_id; the composer's station predicate keeps
// those visible only to tenant-wide actors). The amount is loss/exposure value —
// SENSITIVE (margin.view).
func riskAlertsDataset() Dataset {
	return Dataset{
		Key:                 "risk_alerts",
		Name:                "Risk Alerts",
		Description:         "Risk & loss alerts by type, severity and status, per station, with a (gated) value amount.",
		RequiredPermission:  "risk.read",
		SensitivePermission: "margin.view",
		From:                "risk_alerts ra",
		TenantColumn:        "ra.tenant_id",
		StationColumn:       "ra.station_id",
		Dimensions: []Dimension{
			{ID: "created_date", Label: "Raised date", SQLExpr: "(ra.created_at AT TIME ZONE 'UTC')::date::text", Type: TypeDate},
			{ID: "station_id", Label: "Station", SQLExpr: "COALESCE(ra.station_id::text,'')", Type: TypeUUID},
			{ID: "alert_type", Label: "Alert type", SQLExpr: "ra.alert_type", Type: TypeText},
			{ID: "severity", Label: "Severity", SQLExpr: "ra.severity", Type: TypeText},
			{ID: "status", Label: "Status", SQLExpr: "ra.status", Type: TypeText},
			{ID: "rule_code", Label: "Rule", SQLExpr: "COALESCE(ra.rule_code,'')", Type: TypeText},
		},
		Measures: []Measure{
			{ID: "alert_count", Label: "Alerts", SQLExpr: "1", AllowedAggs: []AggFunc{AggCount, AggSum}, Decimal: false, Unit: "count"},
			{ID: "score", Label: "Risk score", SQLExpr: "ra.score", AllowedAggs: countAggs, Decimal: false, Unit: "score"},
			// SENSITIVE: the loss/exposure value.
			{ID: "amount", Label: "Loss value", SQLExpr: "COALESCE(ra.amount,0)", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS", Sensitive: true},
		},
		Filters: []Filter{
			{ID: "created_date", Label: "Raised date", SQLExpr: "(ra.created_at AT TIME ZONE 'UTC')::date", Type: TypeDate, Operators: []Operator{OpEq, OpGte, OpLte, OpBetween}},
			{ID: "station_id", Label: "Station", SQLExpr: "ra.station_id", Type: TypeUUID, Operators: []Operator{OpEq, OpIn}},
			{ID: "alert_type", Label: "Alert type", SQLExpr: "ra.alert_type", Type: TypeText, Operators: []Operator{OpEq, OpNe, OpIn}},
			{ID: "severity", Label: "Severity", SQLExpr: "ra.severity", Type: TypeText, Operators: []Operator{OpEq, OpNe, OpIn}},
			{ID: "status", Label: "Status", SQLExpr: "ra.status", Type: TypeText, Operators: []Operator{OpEq, OpNe, OpIn}},
		},
	}
}

// receivablesDataset: customer credit invoices, station-scoped via the invoice's
// station_id (nullable). The outstanding amount is CREDIT EXPOSURE — SENSITIVE
// (margin.view per blueprint §14 credit exposure gating).
func receivablesDataset() Dataset {
	return Dataset{
		Key:                 "receivables",
		Name:                "Customer Receivables",
		Description:         "Customer credit invoices: amount and (gated) outstanding credit exposure by customer/station/status.",
		RequiredPermission:  "customer.read",
		SensitivePermission: "margin.view",
		From:                "customer_invoices ci",
		TenantColumn:        "ci.tenant_id",
		StationColumn:       "ci.station_id",
		Dimensions: []Dimension{
			{ID: "invoice_date", Label: "Invoice date", SQLExpr: "ci.invoice_date::text", Type: TypeDate},
			{ID: "customer_id", Label: "Customer", SQLExpr: "ci.customer_id::text", Type: TypeUUID},
			{ID: "station_id", Label: "Station", SQLExpr: "COALESCE(ci.station_id::text,'')", Type: TypeUUID},
			{ID: "status", Label: "Status", SQLExpr: "ci.status", Type: TypeText},
			{ID: "source_type", Label: "Source", SQLExpr: "ci.source_type", Type: TypeText},
		},
		Measures: []Measure{
			{ID: "amount", Label: "Invoiced amount", SQLExpr: "ci.amount", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS"},
			{ID: "invoice_count", Label: "Invoices", SQLExpr: "1", AllowedAggs: []AggFunc{AggCount, AggSum}, Decimal: false, Unit: "count"},
			// SENSITIVE: outstanding credit exposure.
			{ID: "outstanding_amount", Label: "Outstanding exposure", SQLExpr: "ci.outstanding_amount", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS", Sensitive: true},
		},
		Filters: []Filter{
			{ID: "invoice_date", Label: "Invoice date", SQLExpr: "ci.invoice_date", Type: TypeDate, Operators: []Operator{OpEq, OpGte, OpLte, OpBetween}},
			{ID: "customer_id", Label: "Customer", SQLExpr: "ci.customer_id", Type: TypeUUID, Operators: []Operator{OpEq, OpIn}},
			{ID: "station_id", Label: "Station", SQLExpr: "ci.station_id", Type: TypeUUID, Operators: []Operator{OpEq, OpIn}},
			{ID: "status", Label: "Status", SQLExpr: "ci.status", Type: TypeText, Operators: []Operator{OpEq, OpNe, OpIn}},
		},
	}
}

// shiftsDataset: shift lifecycle per station/day. Counts only.
func shiftsDataset() Dataset {
	return Dataset{
		Key:                "shifts",
		Name:               "Shifts",
		Description:        "Shift lifecycle by station and status (open / closed / approved).",
		RequiredPermission: "station.read",
		From:               "shifts sh",
		TenantColumn:       "sh.tenant_id",
		StationColumn:      "sh.station_id",
		Dimensions: []Dimension{
			{ID: "opened_date", Label: "Opened date", SQLExpr: "(sh.opened_at AT TIME ZONE 'UTC')::date::text", Type: TypeDate},
			{ID: "station_id", Label: "Station", SQLExpr: "sh.station_id::text", Type: TypeUUID},
			{ID: "status", Label: "Status", SQLExpr: "sh.status", Type: TypeText},
			{ID: "name", Label: "Shift name", SQLExpr: "sh.name", Type: TypeText},
		},
		Measures: []Measure{
			{ID: "shift_count", Label: "Shifts", SQLExpr: "1", AllowedAggs: []AggFunc{AggCount, AggSum}, Decimal: false, Unit: "count"},
		},
		Filters: []Filter{
			{ID: "opened_date", Label: "Opened date", SQLExpr: "(sh.opened_at AT TIME ZONE 'UTC')::date", Type: TypeDate, Operators: []Operator{OpEq, OpGte, OpLte, OpBetween}},
			{ID: "station_id", Label: "Station", SQLExpr: "sh.station_id", Type: TypeUUID, Operators: []Operator{OpEq, OpIn}},
			{ID: "status", Label: "Status", SQLExpr: "sh.status", Type: TypeText, Operators: []Operator{OpEq, OpNe, OpIn}},
		},
	}
}

// cashDataset: the cash position drawn from revenue_days (cash tender vs
// variance), a finance-permission view of the same rollup focused on cash.
func cashDataset() Dataset {
	return Dataset{
		Key:                "cash_position",
		Name:               "Cash Position",
		Description:        "Per-station, per-day cash tender, expected vs recorded variance and shortfalls.",
		RequiredPermission: "finance.read",
		From:               "revenue_days rd",
		TenantColumn:       "rd.tenant_id",
		StationColumn:      "rd.station_id",
		Dimensions: []Dimension{
			{ID: "business_date", Label: "Business date", SQLExpr: "rd.business_date::text", Type: TypeDate},
			{ID: "station_id", Label: "Station", SQLExpr: "rd.station_id::text", Type: TypeUUID},
			{ID: "status", Label: "Lock status", SQLExpr: "rd.status", Type: TypeText},
		},
		Measures: []Measure{
			{ID: "cash_total", Label: "Cash tender", SQLExpr: "rd.cash_total", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS"},
			{ID: "cash_variance", Label: "Cash variance", SQLExpr: "rd.cash_variance", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS"},
			{ID: "tender_total", Label: "Total tender", SQLExpr: "rd.tender_total", AllowedAggs: moneyAggs, Decimal: true, Unit: "TZS"},
			{ID: "day_count", Label: "Days", SQLExpr: "1", AllowedAggs: []AggFunc{AggCount, AggSum}, Decimal: false, Unit: "count"},
		},
		Filters: []Filter{
			{ID: "business_date", Label: "Business date", SQLExpr: "rd.business_date", Type: TypeDate, Operators: []Operator{OpEq, OpGte, OpLte, OpBetween}},
			{ID: "station_id", Label: "Station", SQLExpr: "rd.station_id", Type: TypeUUID, Operators: []Operator{OpEq, OpIn}},
			{ID: "status", Label: "Lock status", SQLExpr: "rd.status", Type: TypeText, Operators: []Operator{OpEq, OpNe}},
		},
	}
}

// auditDataset: the immutable audit trail. This is a TENANT-WIDE dataset (no
// station axis) — it requires audit.read, a tenant-wide permission, and the
// composer rejects it for a station-scoped actor (no StationColumn to scope on).
func auditDataset() Dataset {
	return Dataset{
		Key:                "audit_logs",
		Name:               "Audit Trail",
		Description:        "Immutable audit events by action and entity type. Tenant-wide; requires audit.read.",
		RequiredPermission: "audit.read",
		From:               "audit_logs al",
		TenantColumn:       "al.tenant_id",
		StationColumn:      "", // no station axis: tenant-wide only
		Dimensions: []Dimension{
			{ID: "occurred_date", Label: "Date", SQLExpr: "(al.occurred_at AT TIME ZONE 'UTC')::date::text", Type: TypeDate},
			{ID: "action", Label: "Action", SQLExpr: "al.action", Type: TypeText},
			{ID: "entity_type", Label: "Entity type", SQLExpr: "al.entity_type", Type: TypeText},
		},
		Measures: []Measure{
			{ID: "event_count", Label: "Events", SQLExpr: "1", AllowedAggs: []AggFunc{AggCount, AggSum}, Decimal: false, Unit: "count"},
		},
		Filters: []Filter{
			{ID: "occurred_date", Label: "Date", SQLExpr: "(al.occurred_at AT TIME ZONE 'UTC')::date", Type: TypeDate, Operators: []Operator{OpEq, OpGte, OpLte, OpBetween}},
			{ID: "action", Label: "Action", SQLExpr: "al.action", Type: TypeText, Operators: []Operator{OpEq, OpNe, OpLike, OpIn}},
			{ID: "entity_type", Label: "Entity type", SQLExpr: "al.entity_type", Type: TypeText, Operators: []Operator{OpEq, OpNe, OpIn}},
		},
	}
}
