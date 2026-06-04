package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/scheduler"
)

// registerAdminJobRoutes mounts the read-only scheduler-visibility surface. It
// runs inside the admin-console group (requireAuth + rateLimitPerTenant) and is
// gated on audit.read — the canonical admin read-only permission (held by
// system_admin, executive, and auditor), the same gate the audit-log surface
// uses. The job_runs ledger is cross-tenant SYSTEM telemetry, so a single
// admin-read permission (not a station-scoped one) is the right fit.
func (s *Server) registerAdminJobRoutes(r chi.Router) {
	r.With(s.requirePermission("audit.read", nil)).
		Get("/admin/jobs", s.handleListJobRuns)
	// API-exposed, BFF-reachable observability snapshot (Feature 13.3). The kube
	// probes /readyz + /metrics live outside /api/v1 and so are unreachable from
	// the SDK; this re-surfaces the same signals (postgres/redis health, outbox
	// backlog + dead-letter, scheduler last run) under /api/v1. Same audit.read
	// gate as /admin/jobs — read-only operational telemetry.
	r.With(s.requirePermission("audit.read", nil)).
		Get("/observability/health", s.handleObservabilityHealth)
}

// jobRunDTO is the wire shape for one scheduler job run. duration_ms is the
// derived wall-clock run time in milliseconds, omitted while a run is still in
// progress (finished_at null).
type jobRunDTO struct {
	ID         uuid.UUID `json:"id"`
	JobName    string    `json:"job_name"`
	StartedAt  string    `json:"started_at"`
	FinishedAt *string   `json:"finished_at,omitempty"`
	Status     string    `json:"status"`
	Detail     *string   `json:"detail,omitempty"`
	DurationMs *int64    `json:"duration_ms,omitempty"`
}

func toJobRunDTO(jr scheduler.JobRun) jobRunDTO {
	dto := jobRunDTO{
		ID:        jr.ID,
		JobName:   jr.JobName,
		StartedAt: jr.StartedAt.UTC().Format(time.RFC3339),
		Status:    jr.Status,
		Detail:    jr.Detail,
	}
	if jr.FinishedAt != nil {
		f := jr.FinishedAt.UTC().Format(time.RFC3339)
		dto.FinishedAt = &f
	}
	if jr.Duration != nil {
		ms := jr.Duration.Milliseconds()
		dto.DurationMs = &ms
	}
	return dto
}

// handleListJobRuns returns the latest run of every background scheduler job —
// name, last run time, terminal status, and duration — for the admin System
// health page. It is read-only and cross-tenant (job_runs is a system ledger),
// so it never accepts paging or tenant filters: the result is one row per job.
//
// A deployment without migration 0079 (no job_runs table) returns an empty list
// rather than an error, so the page degrades to "no runs recorded yet".
func (s *Server) handleListJobRuns(w http.ResponseWriter, r *http.Request) {
	if _, err := identity.Require(r.Context()); err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if s.jobRuns == nil {
		writeError(w, http.StatusServiceUnavailable, "scheduler visibility unavailable")
		return
	}
	runs, err := s.jobRuns.LatestPerJob(r.Context())
	if err != nil {
		s.logger.Error("list job runs", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]jobRunDTO, 0, len(runs))
	for _, jr := range runs {
		out = append(out, toJobRunDTO(jr))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}
