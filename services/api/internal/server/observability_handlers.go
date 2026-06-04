package server

import (
	"context"
	"net/http"
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// handleObservabilityHealth is the API-exposed, BFF-reachable read-only health
// snapshot (Feature 13.3). The kube probes /readyz + /metrics live OUTSIDE
// /api/v1 so the SDK can't reach them; this endpoint re-surfaces the same
// signals — postgres/redis reachability, the outbox backlog and dead-letter
// counts, and the scheduler's last run — under /api/v1/observability so the
// in-app observability page can show more than scheduler job health.
//
// It is read-only and gated on audit.read (the same admin-read permission the
// /admin/jobs scheduler-visibility endpoint uses), checked at the route. The
// outbox counts are tenant-scoped by RLS on the request pool; the scheduler
// last-run comes from the cross-tenant job_runs system ledger via the owner
// pool, exactly like /admin/jobs.
func (s *Server) handleObservabilityHealth(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	checks := map[string]string{}

	// Postgres: a short-timeout ping on the owner pool, mirroring /readyz.
	if s.deps.DB != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		err := s.deps.DB.Ping(ctx)
		cancel()
		if err != nil {
			checks["postgres"] = "unreachable"
		} else {
			checks["postgres"] = "ok"
		}
	} else {
		checks["postgres"] = "unconfigured"
	}

	// Redis: same shape as /readyz.
	if s.deps.Redis != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		if err := s.deps.Redis.Ping(ctx).Err(); err != nil {
			checks["redis"] = "unreachable"
		} else {
			checks["redis"] = "ok"
		}
		cancel()
	} else {
		checks["redis"] = "unconfigured"
	}

	// Outbox backlog + dead-letter. backlog = unpublished, not dead-lettered (the
	// publisher's eligible set; see migration 0071); dead_letter = parked rows
	// that exhausted the retry budget (failed_at set). Scoped to the caller's
	// tenant with an explicit filter on the owner pool (the same pattern the
	// enterprise read repos use), so an admin sees only their tenant's outbox.
	var backlog, deadLetter int64
	if s.deps.DB != nil {
		if err := s.deps.DB.QueryRow(r.Context(), `
			SELECT
			  count(*) FILTER (WHERE published_at IS NULL AND failed_at IS NULL),
			  count(*) FILTER (WHERE failed_at IS NOT NULL)
			FROM outbox_events
			WHERE tenant_id = $1
		`, actor.TenantID).Scan(&backlog, &deadLetter); err != nil {
			s.logger.Error("observability outbox counts", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	// Scheduler last run: the newest run across all jobs from the job_runs ledger
	// (owner pool, cross-tenant system telemetry). Degrades to null when no run
	// has been recorded or the ledger is absent.
	var scheduler map[string]any
	if s.jobRuns != nil {
		runs, err := s.jobRuns.RecentRuns(r.Context(), 1)
		if err != nil {
			s.logger.Error("observability scheduler last run", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if len(runs) > 0 {
			jr := runs[0]
			dto := toJobRunDTO(jr)
			scheduler = map[string]any{
				"job_name":    dto.JobName,
				"status":      dto.Status,
				"started_at":  dto.StartedAt,
				"finished_at": dto.FinishedAt,
				"duration_ms": dto.DurationMs,
			}
		}
	}

	healthy := checks["postgres"] == "ok" &&
		(checks["redis"] == "ok" || checks["redis"] == "unconfigured") &&
		deadLetter == 0

	writeJSON(w, http.StatusOK, map[string]any{
		"healthy": healthy,
		"checks":  checks,
		"outbox": map[string]any{
			"backlog":     backlog,
			"dead_letter": deadLetter,
		},
		"scheduler_last_run": scheduler,
	})
}
