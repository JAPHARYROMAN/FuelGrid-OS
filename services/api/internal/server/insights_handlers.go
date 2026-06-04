package server

import (
	"net/http"
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/risk"
)

// Feature 11.3 — deterministic insights persistence.
//
// Insights are NOT a new store: the risk engine (Phase 10) already persists
// deterministic, rule-based observations as risk_alerts, each linked to the
// immutable source fact that produced it (subject_type + subject_id). This
// surface re-presents those persisted alerts as "insights" with an explicit
// SOURCE-RECORD LINK so the /risk page (and any consumer) can deep-link from an
// insight to the record it was derived from. The list is read-only and gated by
// risk.read — the same permission as the risk dashboard the insights mirror.
//
// Determinism: the underlying alerts are produced by named SQL evaluators over
// immutable source facts (no float64, no AI), so the same data yields the same
// insight. This handler only reads and projects them.

// insightSource is the resolved deep-link target for the record that produced an
// insight. Kind is the source aggregate (tank, tank_reconciliation, attendant,
// po_line, station); Href is the in-app route the UI can navigate to, or empty
// when the subject has no dedicated page.
type insightSource struct {
	Kind string  `json:"kind"`
	ID   string  `json:"id"`
	Href *string `json:"href,omitempty"`
}

// insightDTO is one persisted, deterministic insight projected from a risk
// alert. RuleCode names the deterministic evaluator that produced it; Source is
// the link back to the originating record.
type insightDTO struct {
	ID                string         `json:"id"`
	RuleCode          *string        `json:"rule_code,omitempty"`
	Type              string         `json:"type"`
	Severity          string         `json:"severity"`
	Status            string         `json:"status"`
	Detail            *string        `json:"detail,omitempty"`
	Amount            *string        `json:"amount,omitempty"`
	RecommendedAction *string        `json:"recommended_action,omitempty"`
	StationID         *string        `json:"station_id,omitempty"`
	Source            *insightSource `json:"source,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
}

// sourceHref maps a source aggregate kind to the in-app route that shows it, so
// the UI can render a "View source" link. Subjects without a dedicated detail
// page (e.g. attendant, po_line) get no href — the source kind + id is still
// returned so the caller can label the link. Mirrors the deep-link logic in the
// notifications page.
func sourceHref(kind string) *string {
	var href string
	switch kind {
	case "tank", "tank_reconciliation":
		href = "/inventory"
	case "station":
		href = "/operations"
	default:
		return nil
	}
	return &href
}

func toInsightDTO(a *risk.Alert) insightDTO {
	d := insightDTO{
		ID:                a.ID.String(),
		RuleCode:          a.RuleCode,
		Type:              a.AlertType,
		Severity:          a.Severity,
		Status:            a.Status,
		Detail:            a.Detail,
		Amount:            a.Amount,
		RecommendedAction: a.RecommendedAction,
		CreatedAt:         a.CreatedAt,
	}
	if a.StationID != nil {
		s := a.StationID.String()
		d.StationID = &s
	}
	// The source-record link is the heart of 11.3: every insight points back to
	// the immutable record it was derived from.
	if a.SubjectType != nil && a.SubjectID != nil {
		d.Source = &insightSource{
			Kind: *a.SubjectType,
			ID:   a.SubjectID.String(),
			Href: sourceHref(*a.SubjectType),
		}
	}
	return d
}

// handleListInsights returns the tenant's persisted, deterministic insights
// (projected from risk alerts) newest-and-highest-score first, each with a
// link to its source record. ?status and ?type filter exactly as the alert list
// does; ?status defaults to all so resolved insights remain visible in history.
// Gated by risk.read.
func (s *Server) handleListInsights(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.risk.ListAlertsPage(r.Context(), actor.TenantID,
		r.URL.Query().Get("status"), r.URL.Query().Get("type"), limit+1, offset)
	if err != nil {
		s.logger.Error("list insights", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]insightDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toInsightDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}
