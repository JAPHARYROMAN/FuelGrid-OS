package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/exportjobs"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
)

// Export-jobs surface (Feature 10.7 + Reports Center Phase 13 — the Export
// Center).
//
// This surface is now genuinely ASYNCHRONOUS. POST /exports ENQUEUES an
// export_jobs row in 'queued' status carrying the {report_key, format, filters}
// and the requesting actor; the advisory-locked background worker
// (export_worker.go) picks it up, RE-CHECKS the actor's permission at generation
// time, re-runs the report to its ReportEnvelope, renders the file, stores the
// bytes DURABLY in the export_jobs row (no external blob store), and marks the
// job completed/failed. The caller polls GET /exports/{id} for status and, once
// completed, GETs /exports/{id}/download to stream the stored bytes
// (permission-checked again at delivery).
//
// BACK-COMPAT: a report_key the async worker does not know how to render (but
// which the synchronous file endpoints DO map) still produces an immediate,
// already-completed receipt pointing at the existing file URL — exactly the old
// behaviour — so no existing caller breaks. The pre-existing synchronous
// /reports/* file endpoints remain mounted and authoritative for their own bytes.
//
// Gated by reports.export at the route. The worker re-checks the report's own
// read permission at generation, and the download re-checks it at delivery, so a
// user can never receive report data they are no longer permitted to view.

// createExportJobRequest is the POST /exports body.
type createExportJobRequest struct {
	ReportKey string            `json:"report_key"`
	Format    string            `json:"format"`
	Filters   map[string]string `json:"filters"`
}

// handleCreateExportJob enqueues an async export job (or, for a report the worker
// cannot render but the legacy file endpoints can, records an immediate
// back-compat receipt). It validates the {report_key, format, filters}, persists
// the job row and audits the request as 'report.exported' carrying the job id.
// Gated by reports.export at the route. A 202 Accepted signals the async path; a
// 201 Created the legacy receipt path.
func (s *Server) handleCreateExportJob(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req createExportJobRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.ReportKey = strings.TrimSpace(req.ReportKey)
	req.Format = strings.ToLower(strings.TrimSpace(req.Format))
	if req.Filters == nil {
		req.Filters = map[string]string{}
	}

	// Prefer the async worker when it knows how to render this report; otherwise
	// fall back to the legacy synchronous-receipt path so no caller breaks. Both
	// paths reject an unsupported combination before writing a row.
	if _, ok := reportSpecFor(req.ReportKey); ok {
		s.enqueueAsyncExport(w, r, actor, req)
		return
	}

	// Legacy receipt: map onto the existing export endpoint's same-origin URL.
	url, ok := buildExportURL(exportReportRequest(req))
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported report_key/format combination")
		return
	}
	fileName := exportJobFileName(req.ReportKey, req.Format)
	job, jerr := s.exportJobs.Create(r.Context(), actor.TenantID, exportjobs.CreateInput{
		ReportKey:   req.ReportKey,
		Format:      req.Format,
		Filters:     req.Filters,
		Status:      exportjobs.StatusCompleted,
		FileURL:     &url,
		FileName:    &fileName,
		RequestedBy: actor.UserID,
	})
	if jerr != nil {
		s.logger.Error("export job: create", "error", jerr, "report_key", req.ReportKey)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.auditExportJob(w, r, actor, job) {
		return
	}
	writeJSON(w, http.StatusCreated, exportJobView(job))
}

// enqueueAsyncExport validates the actor may request this report (a fast 403 at
// enqueue, before a queued row is written — the worker re-checks at generation
// too), enqueues a 'queued' job, audits the enqueue, and returns 202 with the
// job. The actual file is produced later by the worker.
func (s *Server) enqueueAsyncExport(w http.ResponseWriter, r *http.Request, actor identity.Actor, req createExportJobRequest) {
	spec, _ := reportSpecFor(req.ReportKey)
	if req.Format != "csv" && req.Format != "pdf" && req.Format != "xlsx" {
		writeError(w, http.StatusBadRequest, "format must be csv|pdf|xlsx")
		return
	}

	// Fail fast on an obvious permission/scope problem at enqueue, so the caller
	// gets an immediate 403 rather than a queued job that the worker will fail.
	resource := policy.Resource{}
	if spec.stationScoped {
		sid, perr := uuid.Parse(strings.TrimSpace(req.Filters["station_id"]))
		if perr != nil {
			writeError(w, http.StatusBadRequest, "station_id is required for this report")
			return
		}
		resource = policy.AtStation(sid)
	}
	if cerr := s.policy.Can(r.Context(), actor, spec.perm, resource); cerr != nil {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	job, jerr := s.exportJobs.Enqueue(r.Context(), actor.TenantID, exportjobs.EnqueueInput{
		ReportKey:   req.ReportKey,
		Format:      req.Format,
		Filters:     req.Filters,
		RequestedBy: actor.UserID,
	})
	if jerr != nil {
		s.logger.Error("export job: enqueue", "error", jerr, "report_key", req.ReportKey)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.auditExportJob(w, r, actor, job) {
		return
	}
	writeJSON(w, http.StatusAccepted, exportJobView(job))
}

// handleListExportJobs returns the tenant's export-job history, newest first.
// Gated by reports.export.
func (s *Server) handleListExportJobs(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	jobs, lerr := s.exportJobs.ListPage(r.Context(), actor.TenantID, limit+1, offset)
	if lerr != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(jobs) > limit
	if hasMore {
		jobs = jobs[:limit]
	}
	out := make([]map[string]any, 0, len(jobs))
	for i := range jobs {
		out = append(out, exportJobView(&jobs[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

// handleGetExportJob returns one export job by id (tenant-scoped). Gated by
// reports.export.
func (s *Server) handleGetExportJob(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, perr := uuid.Parse(chi.URLParam(r, "id"))
	if perr != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	job, gerr := s.exportJobs.Get(r.Context(), actor.TenantID, id)
	if errors.Is(gerr, exportjobs.ErrNotFound) {
		writeError(w, http.StatusNotFound, "export job not found")
		return
	}
	if gerr != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, exportJobView(job))
}

// exportJobView renders a job as the JSON wire shape. download_url is set only
// once the async worker has stored the file bytes (status completed with a
// result) — the front-end shows the download action when it is non-nil. The
// legacy synchronous receipt path leaves download_url null and keeps file_url.
func exportJobView(j *exportjobs.Job) map[string]any {
	var downloadURL *string
	if j.Status == exportjobs.StatusCompleted && j.ResultSize != nil && *j.ResultSize > 0 {
		u := "/api/v1/exports/" + j.ID.String() + "/download"
		downloadURL = &u
	}
	var completedAt *string
	if j.CompletedAt != nil {
		c := j.CompletedAt.Format(time.RFC3339)
		completedAt = &c
	}
	var startedAt *string
	if j.StartedAt != nil {
		st := j.StartedAt.Format(time.RFC3339)
		startedAt = &st
	}
	return map[string]any{
		"id":           j.ID,
		"report_key":   j.ReportKey,
		"format":       j.Format,
		"filters":      j.Filters,
		"status":       j.Status,
		"file_url":     j.FileURL,
		"file_name":    j.FileName,
		"file_size":    j.FileSize,
		"error":        j.Error,
		"requested_by": j.RequestedBy,
		"created_at":   j.CreatedAt.Format(time.RFC3339),
		"started_at":   startedAt,
		"completed_at": completedAt,
		"checksum":     j.ResultChecksum,
		"download_url": downloadURL,
	}
}

// handleDownloadExportJob streams a completed async job's stored file bytes. It
// re-checks the requesting actor's permission AT DELIVERY (the same generation
// gate the worker applied) so a user who has lost access since the job ran cannot
// download the data. A not-yet-completed or legacy (no stored bytes) job is a
// 404/409 as appropriate. Gated by reports.export at the route.
func (s *Server) handleDownloadExportJob(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, perr := uuid.Parse(chi.URLParam(r, "id"))
	if perr != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	job, data, found, gerr := s.exportJobs.GetResult(r.Context(), actor.TenantID, id)
	if errors.Is(gerr, exportjobs.ErrNotFound) {
		writeError(w, http.StatusNotFound, "export job not found")
		return
	}
	if gerr != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !found {
		// Exists but no stored bytes yet: still queued/running, failed, or a legacy
		// receipt (download via its file_url instead).
		writeError(w, http.StatusConflict, "export is not ready for download")
		return
	}

	// PERMISSION RE-CHECK AT DELIVERY. Re-evaluate the report's own read
	// permission for this actor + filters; a revoked grant 403s rather than
	// serving stale data.
	if spec, ok := reportSpecFor(job.ReportKey); ok {
		resource := policy.Resource{}
		if spec.stationScoped {
			if sid, serr := uuid.Parse(strings.TrimSpace(job.Filters["station_id"])); serr == nil {
				resource = policy.AtStation(sid)
			}
		}
		if cerr := s.policy.Can(r.Context(), actor, spec.perm, resource); cerr != nil {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
	}

	contentType := "application/octet-stream"
	if job.ResultContentType != nil && *job.ResultContentType != "" {
		contentType = *job.ResultContentType
	}
	filename := exportJobFileName(job.ReportKey, job.Format)
	if job.ResultFilename != nil && *job.ResultFilename != "" {
		filename = *job.ResultFilename
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	// Force a download (never inline render) and disable content-type sniffing, so
	// the stored bytes can never be interpreted as active content by the browser.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if job.ResultChecksum != nil {
		w.Header().Set("X-Export-Checksum", *job.ResultChecksum)
	}
	w.Header().Set("X-Export-Id", job.ID.String())
	w.WriteHeader(http.StatusOK)
	// data is the report file the worker rendered and stored under this tenant; it
	// is served as an attachment with an explicit content type and nosniff, so it
	// is delivered as a file download, never executed in the browser.
	_, _ = w.Write(data) //nolint:gosec // G705: attachment download of stored, tenant-scoped report bytes; Content-Disposition+nosniff set
}

// exportJobFileName builds a friendly download name for the job's file.
func exportJobFileName(reportKey, format string) string {
	return reportKey + "-" + time.Now().UTC().Format("20060102") + "." + format
}

// auditExportJob records the export job as a 'report.exported' audit event
// (mirroring the file handlers' audit path) within a tx, carrying the job id.
// Returns false (after writing the error) on failure.
func (s *Server) auditExportJob(w http.ResponseWriter, r *http.Request, actor identity.Actor, job *exportjobs.Job) bool {
	newValue := map[string]any{
		"report_type": job.ReportKey, "format": job.Format, "export_job_id": job.ID.String(),
	}
	for k, v := range job.Filters {
		newValue["filter_"+k] = v
	}
	ctx := r.Context()
	tx, terr := s.deps.DB.Begin(ctx)
	if terr != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "report.exported", EventType: "ReportExported",
		EntityType: "export_job", EntityID: job.ID.String(),
		NewValue:  newValue,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("export job audit", "error", err, "report_key", job.ReportKey)
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	return true
}
