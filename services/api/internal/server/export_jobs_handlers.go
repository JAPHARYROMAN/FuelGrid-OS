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
)

// Export-jobs surface (Feature 10.7).
//
// Today's report exports are synchronous: a request streams the file straight
// back. This surface adds a durable receipt — an export_jobs row recording the
// {report_key, format, filters} requested and the resulting file's same-origin
// URL — so the reporting hub can show an export history. The file is still
// produced by the existing export endpoints (buildExportURL maps the request
// onto one); creating a job records it and audits 'report.exported' (the same
// action the file handlers emit), wiring the job row to that audit event.
//
// Gated by reports.export at the route (verified present in 0004). The mapped
// file endpoint re-checks its own per-station / finance permission when fetched,
// so recording a job never bypasses a file's gate.

// createExportJobRequest is the POST /exports body.
type createExportJobRequest struct {
	ReportKey string            `json:"report_key"`
	Format    string            `json:"format"`
	Filters   map[string]string `json:"filters"`
}

// handleCreateExportJob records an export job: it validates the
// {report_key, format, filters}, maps it onto the existing export endpoint's
// same-origin URL (the synchronous produce path), persists the job row, and
// audits the request as 'report.exported' carrying the job id. Gated by
// reports.export.
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

	// Reuse the same mapping the unified export uses to resolve the file URL —
	// an unsupported report_key/format combination is rejected here, before a
	// job row is written.
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
		Status:      "completed",
		FileURL:     &url,
		FileName:    &fileName,
		RequestedBy: actor.UserID,
	})
	if jerr != nil {
		s.logger.Error("export job: create", "error", jerr, "report_key", req.ReportKey)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Audit the export request, mirroring the file handlers' 'report.exported'
	// path and wiring the job row's id to the audit event.
	if !s.auditExportJob(w, r, actor, job) {
		return
	}
	writeJSON(w, http.StatusCreated, exportJobView(job))
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

// exportJobView renders a job as the JSON wire shape.
func exportJobView(j *exportjobs.Job) map[string]any {
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
	}
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
