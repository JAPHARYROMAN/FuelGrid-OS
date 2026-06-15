// Package reportrules is the config-driven, deterministic insight rule engine for
// the Reports Center (blueprint §9 "Automated Report Intelligence Without AI",
// §9.3 Insight Rule Engine, §21.3, §23). It is the report-insight sibling of
// internal/risk's evaluator registry: a per-tenant report_rules row whose
// `condition` column names a REGISTERED, code-backed Go evaluator (NOT a
// free-form expression and NOT AI) decides whether an insight fires, and a SAFE
// {token} message template renders the fired line deterministically.
//
// ADDITIVE / NO-REGRESSION CONTRACT. This engine never replaces the hardcoded
// composers in internal/reporting — those remain the byte-identical source of
// truth for the insights they emit today. The engine AUGMENTS that output: it
// reads the report's already-computed figures (a Facts bag of exact decimal
// STRINGS + ints the handler already has) and folds the fired insights/data-
// quality/actions of the applicable rules into the same envelope. System rules
// seed in mode "shadow" (evaluated for preview/audit but NOT folded), so a fresh
// tenant's default output is unchanged; a tenant flips a rule to "augment" or
// adds a custom rule to have the engine contribute a line.
//
// DETERMINISM. Same Facts + same rules -> identical output. Evaluators do only
// transparent threshold arithmetic on the supplied figures (parsed to float64
// for the same display heuristics internal/reporting already documents); no I/O,
// no clock, no randomness. The rule set is sorted by code before evaluation so
// the emitted order is stable.
package reportrules

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// Severity grades a fired insight (info < warning < critical), matching the
// reporting.Severity vocabulary so the envelope can carry both interchangeably.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Placement is where a fired insight surfaces on the report.
type Placement string

const (
	PlacementInsight     Placement = "insight"
	PlacementDataQuality Placement = "data_quality"
	PlacementSummary     Placement = "summary"
)

// Mode controls whether a fired insight is folded into the envelope (augment) or
// only evaluated for preview/audit (shadow — the no-regression default for seeded
// system rules so the composer stays authoritative).
type Mode string

const (
	ModeShadow  Mode = "shadow"
	ModeAugment Mode = "augment"
)

// Rule is one evaluated report_rules row. Threshold is a decimal STRING ("" when
// NULL) so it stays off the float path; ThresholdConfig is the structured
// multi-field surface (already decoded from the jsonb column) an evaluator reads
// when one scalar is not enough.
type Rule struct {
	ID                   string
	Code                 string
	Name                 string
	ReportKey            string // "" => applies broadly to every report
	Category             string
	Condition            string // registered evaluator key
	Threshold            string // decimal string; "" when NULL
	ThresholdConfig      map[string]any
	ComparisonPeriodDays int
	Severity             Severity
	MessageTemplate      string
	RecommendedAction    string
	Placement            Placement
	Mode                 Mode
	NotifyOnFire         bool
	IsSystem             bool
	Enabled              bool
	Status               string // draft | active | paused | retired
}

// Facts is the already-computed report figure bag the engine evaluates against.
// Every money/litre figure is an exact decimal STRING (the same value the report
// displays); counts are ints; flags are bools. Evaluators read named keys; an
// absent key simply means the evaluator cannot fire (an honest no-op), never an
// error. The handler fills the keys relevant to its report — no figure is
// recomputed here.
type Facts struct {
	Nums  map[string]string // metric -> exact decimal string (money/litre/percent)
	Ints  map[string]int    // metric -> integer count
	Flags map[string]bool   // metric -> boolean state
	Strs  map[string]string // metric -> display string (e.g. a label for {token})
}

// NewFacts returns an empty, non-nil Facts bag.
func NewFacts() Facts {
	return Facts{
		Nums:  map[string]string{},
		Ints:  map[string]int{},
		Flags: map[string]bool{},
		Strs:  map[string]string{},
	}
}

// num returns a fact's decimal value parsed to float64 and whether it was present
// and parseable. Used only for display-threshold heuristics (the figure itself is
// never recomputed), exactly as internal/reporting documents.
func (f Facts) num(key string) (float64, bool) {
	s, ok := f.Nums[key]
	if !ok {
		return 0, false
	}
	return parseDec(s)
}

// rawNum returns a fact's raw decimal string (for {token} substitution) and
// whether it was present.
func (f Facts) rawNum(key string) (string, bool) {
	s, ok := f.Nums[key]
	return s, ok
}

func (f Facts) intVal(key string) (int, bool) { v, ok := f.Ints[key]; return v, ok }

// Fired is one insight an evaluator produced. Severity/Action override the rule's
// configured defaults when the evaluator computes a sharper grade (e.g. a cash
// variance beyond 2x tolerance escalates to critical); Vars feed the template.
//
// Template overrides the rule's configured MessageTemplate for THIS fired insight
// only. It is needed when a single evaluator has two distinct branches whose prose
// differs (e.g. margin_health: a NEGATIVE-margin sentence vs a CONTRACTION
// sentence) and the seeded rule carries one template. The override is rendered
// through the same single-pass, injection-safe RenderTemplate with the fired Vars,
// so determinism and the "no value re-expansion" guarantee are preserved. Empty =>
// use the rule's MessageTemplate.
type Fired struct {
	Severity Severity
	Action   string
	Template string
	Vars     map[string]string
}

// Evaluator runs one named condition against the report Facts using the rule's
// configured thresholds and returns the fired insight(s) — usually zero or one.
// It is pure: same (rule, facts) -> same result.
type Evaluator func(rule Rule, facts Facts) []Fired

// registry maps a condition key to its evaluator. Insight selects rules by their
// `condition`; an unknown condition is skipped (the engine degrades safely,
// mirroring the risk registry).
var registry = map[string]Evaluator{
	"period_over_period":           evalPeriodOverPeriod,
	"variance_vs_average":          evalVarianceVsAverage,
	"cash_variance_over_tolerance": evalCashVariance,
	"tank_over_tolerance":          evalTankOverTolerance,
	"margin_health":                evalMarginHealth,
	"overdue_share":                evalOverdueShare,
	"delivery_shortfall":           evalDeliveryShortfall,
	"period_unlocked":              evalPeriodUnlocked,
}

// EvaluatorFor returns the evaluator for a condition key, or (nil, false).
func EvaluatorFor(condition string) (Evaluator, bool) {
	e, ok := registry[condition]
	return e, ok
}

// RegisteredConditions returns the registered evaluator keys (sorted) — used by
// the CRUD layer to validate a rule's condition and by tests to lock the set.
func RegisteredConditions() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Insight is one rendered, fired insight ready to fold into the report envelope.
// It carries the source rule code (so the UI can show "which rule drove this")
// and the placement/mode that decide where and whether it surfaces.
type Insight struct {
	RuleID            string
	RuleCode          string
	RuleName          string
	Severity          Severity
	Message           string
	RecommendedAction string
	Placement         Placement
	Mode              Mode
	NotifyOnFire      bool
}

// Evaluate runs every APPLICABLE rule against the facts and returns the rendered
// fired insights, sorted deterministically by rule code. A rule is applicable
// when it is enabled, status 'active', and its report_key is empty (broad) or
// equals reportKey. The result includes BOTH shadow and augment insights so a
// preview/audit surface can show what every rule would say; callers fold only
// the augment ones into the live envelope (see FoldInto).
func Evaluate(reportKey string, rules []Rule, facts Facts) []Insight {
	applicable := make([]Rule, 0, len(rules))
	for i := range rules {
		r := rules[i]
		if !r.Enabled || r.Status != "active" {
			continue
		}
		if r.ReportKey != "" && r.ReportKey != reportKey {
			continue
		}
		applicable = append(applicable, r)
	}
	sort.SliceStable(applicable, func(i, j int) bool { return applicable[i].Code < applicable[j].Code })

	var out []Insight
	for i := range applicable {
		r := applicable[i]
		eval, ok := registry[r.Condition]
		if !ok {
			continue // unknown condition: skip safely
		}
		for _, fired := range eval(r, facts) {
			sev := r.Severity
			if fired.Severity != "" {
				sev = fired.Severity
			}
			action := r.RecommendedAction
			if fired.Action != "" {
				action = fired.Action
			}
			tmpl := r.MessageTemplate
			if fired.Template != "" {
				tmpl = fired.Template
			}
			out = append(out, Insight{
				RuleID:            r.ID,
				RuleCode:          r.Code,
				RuleName:          r.Name,
				Severity:          sev,
				Message:           RenderTemplate(tmpl, fired.Vars),
				RecommendedAction: action,
				Placement:         r.Placement,
				Mode:              r.Mode,
				NotifyOnFire:      r.NotifyOnFire,
			})
		}
	}
	return out
}

// RenderTemplate substitutes {token} placeholders in a message template with
// values from vars using deterministic, SAFE string replacement — there is no
// expression evaluation and no template engine, so a value can never inject a new
// placeholder or execute anything. Unknown tokens are left intact (a misconfigured
// template degrades visibly rather than silently dropping context). To prevent a
// value that itself contains a "{token}" from being re-expanded on a later pass,
// the substitution runs in a SINGLE left-to-right scan over the template.
func RenderTemplate(tmpl string, vars map[string]string) string {
	if tmpl == "" || !strings.ContainsRune(tmpl, '{') {
		return tmpl
	}
	var b strings.Builder
	b.Grow(len(tmpl))
	for i := 0; i < len(tmpl); {
		if tmpl[i] == '{' {
			if end := strings.IndexByte(tmpl[i:], '}'); end > 0 {
				token := tmpl[i+1 : i+end]
				if v, ok := vars[token]; ok {
					b.WriteString(v) // value written verbatim; never re-scanned for tokens
					i += end + 1
					continue
				}
			}
		}
		b.WriteByte(tmpl[i])
		i++
	}
	return b.String()
}

// ---- small deterministic numeric helpers (display-only math) ----

// parseDec parses a decimal string into a float64 for heuristic math only. Blank
// / unparseable -> (0, false) so an evaluator skips rather than emit a misleading
// figure. Mirrors internal/reporting.parseDec.
func parseDec(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// configFloat reads a float threshold from a rule's ThresholdConfig by key,
// falling back to def when absent or non-numeric. JSON numbers decode to float64;
// a string number is also accepted.
func configFloat(cfg map[string]any, key string, def float64) float64 {
	if cfg == nil {
		return def
	}
	switch v := cfg[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		if f, ok := parseDec(v); ok {
			return f
		}
	}
	return def
}

// configStr reads a string from a rule's ThresholdConfig by key, falling back to
// def when absent.
func configStr(cfg map[string]any, key, def string) string {
	if cfg == nil {
		return def
	}
	if v, ok := cfg[key].(string); ok && v != "" {
		return v
	}
	return def
}

// thresholdOr returns the rule's scalar threshold as a float, or def when unset.
func thresholdOr(rule Rule, def float64) float64 {
	if v, ok := parseDec(rule.Threshold); ok {
		return v
	}
	return def
}

// fmtPct1 renders a percentage with one decimal (e.g. "14.0").
func fmtPct1(p float64) string {
	return strconv.FormatFloat(p, 'f', 1, 64)
}

// fmtPct0 renders a percentage with no decimals (e.g. "50").
func fmtPct0(p float64) string {
	return strconv.FormatFloat(math.Round(p), 'f', 0, 64)
}
