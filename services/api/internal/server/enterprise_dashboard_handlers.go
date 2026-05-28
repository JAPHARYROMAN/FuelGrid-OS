package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

func (s *Server) handleRebuildProjections(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rebuilt := 0
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "enterprise_projection.rebuilt", EventType: "EnterpriseProjectionRebuilt", EntityType: "enterprise_projection",
		EntityID: actor.TenantID.String(),
	}, func(tx pgx.Tx) (string, error) {
		n, err := s.enterprise.RebuildStationKPIs(r.Context(), tx, actor.TenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		rebuilt = n
		return actor.TenantID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projection": "station_daily_kpis", "rows": rebuilt})
}

func (s *Server) handleEnterpriseOverview(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	from := parseDateParam(r, "from", time.Now().AddDate(0, -1, 0))
	to := parseDateParam(r, "to", time.Now())
	o, err := s.enterprise.EnterpriseOverview(r.Context(), actor.TenantID, from, to)
	if err != nil {
		s.logger.Error("enterprise overview", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var projAt *string
	if o.ProjectionAt != nil {
		v := o.ProjectionAt.Format(time.RFC3339)
		projAt = &v
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from.Format(dateLayout), "to": to.Format(dateLayout),
		"gross_revenue": o.GrossRevenue, "net_revenue": o.NetRevenue, "margin_total": o.MarginTotal,
		"ap_outstanding": o.APOutstanding, "ar_outstanding": o.AROutstanding,
		"open_incidents": o.OpenIncidents, "approvals_waiting": o.ApprovalsWaiting,
		"projection_rebuilt_at": projAt,
	})
}

func (s *Server) handleStationRanking(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var regionID *uuid.UUID
	if id := queryUUID(r, "region_id"); id != uuid.Nil {
		regionID = &id
	}
	if rid := chi.URLParam(r, "id"); rid != "" {
		if id, perr := uuid.Parse(rid); perr == nil {
			regionID = &id
		}
	}
	from := parseDateParam(r, "from", time.Now().AddDate(0, -1, 0))
	to := parseDateParam(r, "to", time.Now())
	rows, err := s.enterprise.StationRanking(r.Context(), actor.TenantID, regionID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, map[string]any{
			"station_id": rows[i].StationID, "name": rows[i].Name,
			"gross_revenue": rows[i].GrossRevenue, "margin_total": rows[i].MarginTotal,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}
