package server

import (
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/reporting"
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
	Table              reportTable         `json:"table"`
	Insights           []reporting.Insight `json:"insights"`
	RecommendedActions []string            `json:"recommended_actions"`
	Drilldown          []drilldownLink     `json:"drilldown"`
	ExportOptions      []exportOption      `json:"export_options"`
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
