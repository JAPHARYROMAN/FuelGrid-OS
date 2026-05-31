package server

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

func (s *Server) handleRecomputeRiskScores(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	scored := 0
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "risk_score.calculated", EventType: "RiskScoreCalculated", EntityType: "risk_score",
		EntityID: actor.TenantID.String(),
	}, func(tx pgx.Tx) (string, error) {
		n, err := s.risk.RecomputeStationScores(r.Context(), tx, actor.TenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		scored = n
		return actor.TenantID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scored_stations": scored})
}

func (s *Server) handleListRiskScores(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.risk.ListScoresPage(r.Context(), actor.TenantID, r.URL.Query().Get("dimension"), limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, map[string]any{
			"dimension": rows[i].Dimension, "entity_id": rows[i].EntityID,
			"score": rows[i].Score, "band": rows[i].Band, "open_alerts": rows[i].OpenAlerts,
		})
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

func (s *Server) handleRiskOverview(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	o, err := s.risk.Overview(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("risk overview", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	top := make([]map[string]any, 0, len(o.TopStations))
	for i := range o.TopStations {
		top = append(top, map[string]any{
			"entity_id": o.TopStations[i].EntityID, "score": o.TopStations[i].Score,
			"band": o.TopStations[i].Band, "open_alerts": o.TopStations[i].OpenAlerts,
		})
	}
	var computedAt *string
	if o.ComputedAt != nil {
		v := o.ComputedAt.Format(time.RFC3339)
		computedAt = &v
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"open_by_severity": o.OpenBySeverity, "open_total": o.OpenTotal,
		"top_stations": top, "scores_computed_at": computedAt,
	})
}
