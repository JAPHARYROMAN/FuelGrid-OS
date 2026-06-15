package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
	"github.com/japharyroman/fuelgrid-os/internal/reportsnapshots"
)

// Lock-state surfacing (Reports Center Phase 14 — blueprint §15).
//
// These two read endpoints surface the snapshot LOCK STATE without re-running a
// report:
//
//   - GET /reports/{key}/lock-state — does a signed-off snapshot exist for this
//     report/scope? Drives the lock badge + link on a report view.
//   - GET /reports/snapshots/recent — the most recent signed-off snapshots across
//     all reports, PERMISSION-FILTERED so the hub "Locked" rail never lists a
//     snapshot of a report the actor cannot run live.
//
// Both honour the same permission model as the rest of the snapshot surface: a
// signed-off snapshot is only surfaced to an actor who could run the underlying
// report live (re-checked via reportSpecFor + policy.Can against the snapshot's
// captured filters).

// handleReportLockState reports whether a signed-off snapshot exists for the
// {key} report and the optional ?station_id scope. Gated by the report's OWN
// permission (so the lock badge itself never leaks across scope). Returns the
// locking snapshot's id + hash + signer when locked, or locked=false otherwise.
func (s *Server) handleReportLockState(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	reportKey := strings.TrimSpace(chi.URLParam(r, "key"))

	filters := map[string]string{}
	if sid := strings.TrimSpace(r.URL.Query().Get("station_id")); sid != "" {
		filters["station_id"] = sid
	}
	if _, ok := s.authorizeSnapshotReport(w, r, actor, reportKey, filters); !ok {
		return
	}

	snap, gerr := s.reportSnaps.LatestSignedOffForReport(r.Context(), actor.TenantID, reportKey, stationFilterPtr(filters))
	if errors.Is(gerr, reportsnapshots.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"report_key": reportKey, "locked": false})
		return
	}
	if gerr != nil {
		s.logger.Error("snapshot lock-state", "error", gerr, "report_key", reportKey)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := map[string]any{
		"report_key":   reportKey,
		"locked":       true,
		"snapshot_id":  snap.ID,
		"content_hash": snap.ContentHash,
		"revision":     snap.Revision,
		"signed_off_by": func() any {
			if snap.SignedOffBy != nil {
				return snap.SignedOffBy.String()
			}
			return nil
		}(),
		"signed_off_at": func() any {
			if snap.SignedOffAt != nil {
				return snap.SignedOffAt.Format(time.RFC3339)
			}
			return nil
		}(),
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRecentLockedSnapshots returns the most recent SIGNED-OFF snapshots across
// reports for the hub "Locked" rail. Each candidate is PERMISSION-FILTERED: a
// snapshot is only included when the actor could run its underlying report live
// (reportSpecFor + policy.Can against the snapshot's captured filters), so the
// rail never leaks a locked report the actor cannot otherwise see. Over-fetches a
// little so the filtered result still fills the requested limit.
func (s *Server) handleRecentLockedSnapshots(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit := 5
	// Over-fetch so the permission filter still yields up to `limit` rows.
	candidates, lerr := s.reportSnaps.ListSignedOff(r.Context(), actor.TenantID, limit*4)
	if lerr != nil {
		s.logger.Error("snapshot recent-locked", "error", lerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]map[string]any, 0, limit)
	for i := range candidates {
		snap := candidates[i]
		if !s.actorCanRunReport(r, actor, snap.ReportKey, snap.FiltersUsed) {
			continue
		}
		out = append(out, snapshotView(&snap))
		if len(out) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

// actorCanRunReport reports whether the actor could run the underlying report
// live for the snapshot's captured filters — the SAME permission gate the live
// report enforces. Used to permission-filter the locked rail without writing an
// HTTP error (unknown report keys / forbidden actors are simply excluded).
func (s *Server) actorCanRunReport(r *http.Request, actor identity.Actor, reportKey string, filters map[string]string) bool {
	spec, ok := reportSpecFor(reportKey)
	if !ok {
		return false
	}
	resource := policy.Resource{}
	if spec.stationScoped {
		sid := strings.TrimSpace(filters["station_id"])
		parsed, perr := uuid.Parse(sid)
		if perr != nil {
			return false
		}
		resource = policy.AtStation(parsed)
	}
	return s.policy.Can(r.Context(), actor, spec.perm, resource) == nil
}
