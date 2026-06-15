package server

import (
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/reporting"
	"github.com/japharyroman/fuelgrid-os/internal/reportrules"
)

// Structured report envelope (REPORTS-STRUCTURED).
//
// ReportEnvelope is the single, shared wire shape every structured report
// endpoint returns. It carries the report's metadata, the filters that produced
// it, the deterministic data-quality warnings and insights (REUSED verbatim from
// internal/reporting — no money is recomputed here), a headline summary, a
// report-specific chart payload (always decimal strings, never float money), a
// generic columnar table for drill-down, and the navigation/export affordances
// the frontend renders. Slices are always non-nil so the JSON shape is stable.

// ReportEnvelope is the canonical structured-report payload.
type ReportEnvelope struct {
	Metadata           reportMetadata      `json:"metadata"`
	FiltersUsed        map[string]string   `json:"filters_used"`
	DataQuality        []dataQualityItem   `json:"data_quality"`
	Summary            []summaryMetric     `json:"summary"`
	ChartData          any                 `json:"chart_data"`
	TenderMix          *tenderMix          `json:"tender_mix,omitempty"`
	Table              reportTable         `json:"table"`
	Insights           []reporting.Insight `json:"insights"`
	RecommendedActions []string            `json:"recommended_actions"`
	Drilldown          []drilldownLink     `json:"drilldown"`
	ExportOptions      []exportOption      `json:"export_options"`
	// InsightRules surfaces "which rules drove these insights": for every report
	// rule that FIRED (config-driven engine, Phase 15), a transparent attribution
	// row. It lists BOTH the augment rules whose insight was folded into Insights
	// above and the shadow rules that fired for preview only (folded=false), so the
	// management UI can show what each rule would say. Always non-nil.
	InsightRules []insightRuleHit `json:"insight_rules"`
}

// insightRuleHit is one fired report-rule attribution: the source rule, the
// rendered message, the resolved severity/placement, and whether it was actually
// folded into the live envelope (augment) or evaluated for preview only (shadow).
type insightRuleHit struct {
	RuleID    string `json:"rule_id"`
	RuleCode  string `json:"rule_code"`
	RuleName  string `json:"rule_name"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Placement string `json:"placement"`
	Mode      string `json:"mode"`
	Folded    bool   `json:"folded"`
}

// tenderMix is an additive, report-specific breakdown of a station-day's
// recorded tenders by type — cash / mobile-money / card / credit / voucher and
// their total. Every figure is an exact decimal STRING (numeric -> text), read
// from the same revenue_days rollup the close figures come from; no money is
// recomputed here. Optional (a pointer with omitempty) so reports that have no
// tender split simply omit it. The Daily Station Close report sets it; later
// phases (Sales) reuse the same shape for the payment-method donut.
type tenderMix struct {
	Cash        string `json:"cash"`
	MobileMoney string `json:"mobile_money"`
	Card        string `json:"card"`
	Credit      string `json:"credit"`
	Voucher     string `json:"voucher"`
	Total       string `json:"total"`
}

// reportMetadata identifies the report instance.
type reportMetadata struct {
	ReportKey   string  `json:"report_key"`
	Title       string  `json:"title"`
	GeneratedAt string  `json:"generated_at"` // RFC3339
	StationID   *string `json:"station_id,omitempty"`
	Period      string  `json:"period"`
}

// dataQualityItem is one data-quality banner: a level (info|warning) and message.
// Derived from the reusable reporting.DataQualityWarning (which carries only a
// message); the level here is always "warning" since a DQ note always tempers a
// figure's reliability.
type dataQualityItem struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// summaryMetric is one headline figure. Value is always a string (decimal money/
// litre or a plain count); unit/delta/direction are optional display hints.
type summaryMetric struct {
	Label     string  `json:"label"`
	Value     string  `json:"value"`
	Unit      string  `json:"unit,omitempty"`
	Delta     *string `json:"delta,omitempty"`
	Direction string  `json:"direction,omitempty"` // up | down | flat
}

// reportTable is a generic columnar table for the drillable grid. Every cell is
// a string so decimal money/litre figures pass through verbatim.
type reportTable struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// drilldownLink points the UI at a deeper view (an overview/console page).
type drilldownLink struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}

// exportOption is one downloadable rendering of the same report.
type exportOption struct {
	Format string `json:"format"` // csv | pdf | xlsx
	URL    string `json:"url"`
}

// newEnvelope seeds an envelope with non-nil slices/maps so the JSON shape is
// always complete even when a section is empty.
func newEnvelope(reportKey, title, period string, stationID *string) ReportEnvelope {
	return ReportEnvelope{
		Metadata: reportMetadata{
			ReportKey:   reportKey,
			Title:       title,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			StationID:   stationID,
			Period:      period,
		},
		FiltersUsed:        map[string]string{},
		DataQuality:        []dataQualityItem{},
		Summary:            []summaryMetric{},
		ChartData:          struct{}{},
		Table:              reportTable{Columns: []string{}, Rows: [][]string{}},
		Insights:           []reporting.Insight{},
		RecommendedActions: []string{},
		Drilldown:          []drilldownLink{},
		ExportOptions:      []exportOption{},
		InsightRules:       []insightRuleHit{},
	}
}

// applyReport folds a reusable reporting.Report (insights + DQ) into the
// envelope, mapping every DQ warning to a "warning"-level item and surfacing
// each insight's recommended_action into the envelope-level recommended_actions
// (deduplicated) so the UI can render a single action list.
func (e *ReportEnvelope) applyReport(rep reporting.Report) {
	e.Insights = append(e.Insights, rep.Insights...)
	for i := range rep.DataQuality {
		e.DataQuality = append(e.DataQuality, dataQualityItem{
			Level:   "warning",
			Message: rep.DataQuality[i].Message,
		})
	}
	seen := map[string]bool{}
	for i := range e.RecommendedActions {
		seen[e.RecommendedActions[i]] = true
	}
	for i := range rep.Insights {
		a := rep.Insights[i].RecommendedAction
		if a != "" && !seen[a] {
			e.RecommendedActions = append(e.RecommendedActions, a)
			seen[a] = true
		}
	}
}

// applyReportRules runs the config-driven report insight engine (Phase 15) for
// this report's key against the supplied figures and folds the result in. It is
// ADDITIVE and NO-REGRESSION: the composer output applied via applyReport above
// stays the byte-identical source of truth; this only folds in rules whose mode
// is "augment" (tenant-tuned thresholds and custom rules), placed per the rule's
// report_placement. Every fired rule — augment AND shadow — is recorded in
// InsightRules for the "which rules drove these insights" surface. With no rules
// (or only shadow rules) the visible envelope is unchanged. It returns the fired
// insights so the handler can dispatch opt-in notifications. The engine never
// errors the report: an empty rules slice is a clean no-op.
func (e *ReportEnvelope) applyReportRules(reportKey string, rules []reportrules.Rule, facts reportrules.Facts) []reportrules.Insight {
	if len(rules) == 0 {
		return nil
	}
	fired := reportrules.Evaluate(reportKey, rules, facts)
	if len(fired) == 0 {
		return nil
	}

	seenAction := map[string]bool{}
	for i := range e.RecommendedActions {
		seenAction[e.RecommendedActions[i]] = true
	}

	for i := range fired {
		ins := fired[i]
		folded := ins.Mode == reportrules.ModeAugment
		e.InsightRules = append(e.InsightRules, insightRuleHit{
			RuleID:    ins.RuleID,
			RuleCode:  ins.RuleCode,
			RuleName:  ins.RuleName,
			Severity:  string(ins.Severity),
			Message:   ins.Message,
			Placement: string(ins.Placement),
			Mode:      string(ins.Mode),
			Folded:    folded,
		})
		if !folded {
			continue // shadow: recorded for preview, not folded into the live view
		}
		switch ins.Placement {
		case reportrules.PlacementDataQuality:
			e.DataQuality = append(e.DataQuality, dataQualityItem{
				Level:   "warning",
				Message: ins.Message,
			})
		default: // insight | summary both surface as an insight line
			e.Insights = append(e.Insights, reporting.Insight{
				Severity:          reporting.Severity(ins.Severity),
				Message:           ins.Message,
				RecommendedAction: ins.RecommendedAction,
			})
		}
		if a := ins.RecommendedAction; a != "" && !seenAction[a] {
			e.RecommendedActions = append(e.RecommendedActions, a)
			seenAction[a] = true
		}
	}
	return fired
}
