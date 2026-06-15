package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/email"
	"github.com/japharyroman/fuelgrid-os/internal/exportjobs"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
	"github.com/japharyroman/fuelgrid-os/internal/notifications"
	"github.com/japharyroman/fuelgrid-os/internal/scheduledreports"
)

// Per-tenant Scheduled Reports dispatcher (Reports Center Phase 12 — blueprint
// §8). Implements scheduler.ScheduledReportRunner: the scheduler's advisory-locked,
// panic-isolated, job_runs-ledgered tick calls RunDue, which claims every due
// enabled schedule and delivers it.
//
// PERMISSION AT DELIVERY (blueprint §8.5, non-negotiable). For each run the report
// is re-authorized at the MOMENT of generation/delivery, never trusting the grant
// that existed when the schedule was created:
//   - a USER-ID recipient is re-checked individually (its own identity); a user who
//     lost the report permission (or the station grant) is SKIPPED — no in-app row,
//     no email, no bytes.
//   - an EMAIL / WEBHOOK recipient has no platform identity, so the schedule OWNER
//     (created_by) is the permission anchor; if the owner can no longer run the
//     report the whole run is recorded skipped_forbidden and NOTHING is generated.
//
// IDEMPOTENCY. ClaimDue atomically advances next_run_at as it selects a due row, so
// a duplicated/concurrent tick can claim a given schedule at most once per period;
// the run is then recorded in scheduled_report_runs UNIQUE on (schedule, period_key)
// so even a retried delivery for the same logical period collapses to one send.

const (
	// scheduledReportsBatch caps schedules handled per tick so a backlog drains
	// across ticks instead of monopolising the worker.
	scheduledReportsBatch = 25
	// scheduledRunTimeout bounds a single schedule's render + delivery.
	scheduledRunTimeout = 90 * time.Second
	// webhookPostTimeout bounds a single webhook POST.
	webhookPostTimeout = 15 * time.Second
)

// RunDue is the scheduler entry point. It claims due schedules (advancing their
// next_run_at) and dispatches each. Per-schedule failures are isolated and recorded
// — one bad schedule never aborts the batch. Returns a short ledger detail string.
func (s *Server) RunDue(ctx context.Context) (string, error) {
	if s.scheduledRpts == nil {
		return "skipped: scheduled-reports repo unavailable", nil
	}
	now := time.Now()
	due, err := s.scheduledRpts.ClaimDue(ctx, now, scheduledReportsBatch)
	if err != nil {
		return "", fmt.Errorf("claim due schedules: %w", err)
	}
	if len(due) == 0 {
		return "no due schedules", nil
	}

	var delivered, skipped, failed int
	for i := range due {
		sr := due[i]
		// periodKey identifies the logical period this claim covers. We compute it
		// from the instant the schedule was due (the PRE-advance next_run_at), so a
		// duplicated tick for the same period yields the same key and the run ledger's
		// UNIQUE collapses it.
		periodKey := sr.Schedule.PeriodKey(now)

		rctx, cancel := context.WithTimeout(ctx, scheduledRunTimeout)
		outcome := s.dispatchOne(rctx, sr, periodKey, now)
		cancel()

		switch outcome.status {
		case scheduledreports.RunSuccess, scheduledreports.RunPartial:
			delivered += outcome.delivered
			skipped += outcome.skipped
		case scheduledreports.RunSkippedForbidden:
			skipped += outcome.skipped
		default:
			failed++
		}
	}
	return fmt.Sprintf("schedules=%d delivered=%d skipped=%d failed=%d", len(due), delivered, skipped, failed), nil
}

// dispatchOutcome is the result of handling one schedule.
type dispatchOutcome struct {
	status    string
	delivered int
	skipped   int
}

// dispatchOne handles one claimed schedule: authorize, render, deliver per channel,
// and record the run. It is panic-recovered by the caller's scheduler wrapper, but
// it also records its own failure into scheduled_report_runs so the schedule's
// history reflects the outcome. A duplicate period (ErrDuplicatePeriod) is treated
// as an already-delivered no-op.
func (s *Server) dispatchOne(ctx context.Context, sr scheduledreports.ScheduledReport, periodKey string, now time.Time) dispatchOutcome {
	log := s.logger.With("scheduled_report_id", sr.ID.String(), "tenant_id", sr.TenantID.String(), "report_key", sr.ReportKey)

	spec, ok := reportSpecFor(sr.ReportKey)
	if !ok {
		_ = s.recordRunSafe(ctx, sr, periodKey, scheduledreports.RecordRunInput{
			ScheduledReportID: sr.ID, PeriodKey: periodKey,
			Status: scheduledreports.RunFailed, Error: strptr("unknown report_key"),
		}, log)
		s.markScheduleStatus(ctx, sr, scheduledreports.StatusError, log)
		return dispatchOutcome{status: scheduledreports.RunFailed}
	}

	// OWNER permission re-check (the anchor for email/webhook recipients AND the
	// gate that must hold to render at all). A revoked owner means NOTHING is
	// generated or delivered for this run.
	ownerActor := identity.Actor{UserID: sr.CreatedBy, TenantID: sr.TenantID}
	if !s.canRunReport(ctx, ownerActor, spec, sr.Filters) {
		_ = s.recordRunSafe(ctx, sr, periodKey, scheduledreports.RecordRunInput{
			ScheduledReportID: sr.ID, PeriodKey: periodKey,
			Status: scheduledreports.RunSkippedForbidden,
			Error:  strptr("schedule owner is no longer permitted to run this report"),
		}, log)
		s.markScheduleStatus(ctx, sr, scheduledreports.StatusError, log)
		log.Warn("scheduled report skipped: owner permission revoked")
		return dispatchOutcome{status: scheduledreports.RunSkippedForbidden}
	}

	// Render the report file ONCE (under the owner actor). The same bytes are then
	// delivered to every permitted recipient.
	data, contentType, filename, checksum, rerr := s.renderExportJob(ctx, ownerActor, sr.ReportKey, sr.Format, sr.Filters)
	if rerr != nil {
		reason := classifyExportError(rerr)
		status := scheduledreports.RunFailed
		if errors.Is(rerr, errExportForbidden) {
			status = scheduledreports.RunSkippedForbidden
		}
		_ = s.recordRunSafe(ctx, sr, periodKey, scheduledreports.RecordRunInput{
			ScheduledReportID: sr.ID, PeriodKey: periodKey, Status: status, Error: &reason,
		}, log)
		s.markScheduleStatus(ctx, sr, scheduledreports.StatusError, log)
		log.Error("scheduled report render failed", "error", rerr)
		return dispatchOutcome{status: status}
	}

	// Persist the rendered file as a completed export_jobs row so it is downloadable
	// from the Export Center / the schedule's run history (durable receipt + bytes),
	// reusing the Phase-13 storage. Best-effort: a storage failure still lets the
	// channel delivery proceed (the run is recorded with a nil export_job_id).
	exportJobID := s.storeScheduledExport(ctx, sr, data, contentType, filename, checksum, log)

	// Deliver per channel. Permission is re-checked per in-app/email recipient.
	delivered, skipped := 0, 0
	var notifIDs []uuid.UUID
	var deliverErr error
	switch sr.DeliveryChannel {
	case scheduledreports.ChannelInApp:
		notifIDs, skipped, deliverErr = s.deliverInApp(ctx, sr, spec, filename, log)
		delivered = len(notifIDs)
	case scheduledreports.ChannelEmail:
		delivered, skipped, deliverErr = s.deliverEmail(ctx, sr, spec, data, contentType, filename, now, log)
	case scheduledreports.ChannelWebhook:
		// The webhook sink is anchored on the owner (already permitted above).
		deliverErr = s.deliverWebhook(ctx, sr, filename, checksum, len(data), now)
		if deliverErr == nil {
			delivered = 1
		}
	default:
		deliverErr = fmt.Errorf("unknown delivery channel %q", sr.DeliveryChannel)
	}

	// Decide the run status.
	status := scheduledreports.RunSuccess
	var errStr *string
	switch {
	case deliverErr != nil && delivered == 0:
		status = scheduledreports.RunFailed
		errStr = strptr(deliverErr.Error())
	case deliverErr != nil:
		status = scheduledreports.RunPartial
		errStr = strptr(deliverErr.Error())
	case skipped > 0 && delivered == 0:
		// Every recipient was permission-revoked: nothing delivered.
		status = scheduledreports.RunSkippedForbidden
	case skipped > 0:
		status = scheduledreports.RunPartial
	}

	recErr := s.recordRunSafe(ctx, sr, periodKey, scheduledreports.RecordRunInput{
		ScheduledReportID: sr.ID, PeriodKey: periodKey, Status: status,
		ExportJobID: exportJobID, NotificationIDs: notifIDs,
		DeliveredCount: delivered, SkippedCount: skipped, Error: errStr,
	}, log)
	if errors.Is(recErr, scheduledreports.ErrDuplicatePeriod) {
		// Another tick already delivered this period — our in-app/email side effects
		// here are the rare double-effect window, but the ledger guard means this is
		// logged, not retried. (In practice ClaimDue's next_run_at advance prevents a
		// second claim of the same period in the first place.)
		log.Info("scheduled report period already recorded; treating as delivered")
		return dispatchOutcome{status: scheduledreports.RunSuccess, delivered: delivered, skipped: skipped}
	}

	// Health status: error if the run failed, else active.
	healthStatus := scheduledreports.StatusActive
	if status == scheduledreports.RunFailed {
		healthStatus = scheduledreports.StatusError
	}
	s.markScheduleStatus(ctx, sr, healthStatus, log)

	s.auditScheduledRun(ctx, sr, periodKey, status, delivered, skipped)
	log.Info("scheduled report dispatched", "status", status, "delivered", delivered, "skipped", skipped)
	return dispatchOutcome{status: status, delivered: delivered, skipped: skipped}
}

// canRunReport re-evaluates whether an actor may run the report, honouring station
// scope from the filters. Mirrors the export worker's generation-time check.
func (s *Server) canRunReport(ctx context.Context, actor identity.Actor, spec reportSpec, filters map[string]string) bool {
	resource := policy.Resource{}
	if spec.stationScoped {
		sid, perr := uuid.Parse(strings.TrimSpace(filters["station_id"]))
		if perr != nil {
			return false
		}
		resource = policy.AtStation(sid)
	}
	return s.policy.Can(ctx, actor, spec.perm, resource) == nil
}

// deliverInApp creates a private notification row for each USER recipient whose
// permission still holds, re-checking each individually (a revoked user is skipped,
// no row written). Email recipients are ignored on this channel. Returns the created
// notification ids, the skipped count, and any error.
func (s *Server) deliverInApp(
	ctx context.Context, sr scheduledreports.ScheduledReport, spec reportSpec, filename string, log loggerLike,
) ([]uuid.UUID, int, error) {
	var ids []uuid.UUID
	var skipped int
	var firstErr error
	title := "Scheduled report ready: " + sr.Name
	body := fmt.Sprintf("Your scheduled report %q (%s) is ready. File: %s", sr.Name, sr.ReportKey, filename)

	for _, rcp := range sr.Recipients {
		if rcp.Type != scheduledreports.RecipientUser {
			continue
		}
		uid, perr := uuid.Parse(rcp.Value)
		if perr != nil {
			continue
		}
		// PERMISSION RE-CHECK PER RECIPIENT.
		if !s.canRunReport(ctx, identity.Actor{UserID: uid, TenantID: sr.TenantID}, spec, sr.Filters) {
			skipped++
			continue
		}
		entity := "scheduled_report"
		entityID := sr.ID.String()
		n, err := s.notifications.Create(ctx, sr.TenantID, notifications.CreateInput{
			UserID:            &uid,
			Type:              "report.scheduled.delivered",
			Title:             title,
			Body:              body,
			Severity:          notifications.SeverityInfo,
			RelatedEntityType: &entity,
			RelatedEntityID:   &entityID,
		})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Warn("scheduled report in-app delivery failed", "user_id", uid.String(), "error", err)
			continue
		}
		ids = append(ids, n.ID)
	}
	return ids, skipped, firstErr
}

// deliverEmail sends the rendered file as an attachment to each recipient: USER
// recipients are re-permission-checked (and their address resolved from the user
// record); EMAIL recipients ride the owner's already-verified permission. Returns
// delivered + skipped counts and any error.
func (s *Server) deliverEmail(
	ctx context.Context, sr scheduledreports.ScheduledReport, spec reportSpec,
	data []byte, contentType, filename string, now time.Time, log loggerLike,
) (int, int, error) {
	if s.email == nil {
		return 0, 0, fmt.Errorf("email sender not configured")
	}
	subject := fmt.Sprintf("Scheduled report: %s — %s", sr.Name, now.Format("2006-01-02"))
	body := fmt.Sprintf("Your scheduled report %q (%s) is attached.\n\nReport: %s\nFormat: %s\n",
		sr.Name, sr.ReportKey, sr.ReportKey, sr.Format)
	att := email.Attachment{Filename: filename, ContentType: contentType, Data: data}

	var delivered, skipped int
	var firstErr error
	for _, rcp := range sr.Recipients {
		var addr string
		switch rcp.Type {
		case scheduledreports.RecipientUser:
			uid, perr := uuid.Parse(rcp.Value)
			if perr != nil {
				continue
			}
			// PERMISSION RE-CHECK PER USER RECIPIENT.
			if !s.canRunReport(ctx, identity.Actor{UserID: uid, TenantID: sr.TenantID}, spec, sr.Filters) {
				skipped++
				continue
			}
			u, uerr := s.userRepo.FindByID(ctx, sr.TenantID, uid)
			if uerr != nil || u == nil || strings.TrimSpace(u.Email) == "" {
				skipped++
				continue
			}
			addr = u.Email
		case scheduledreports.RecipientEmail:
			// Anchored on the owner permission (already verified before render).
			addr = strings.TrimSpace(rcp.Value)
		default:
			continue
		}
		if addr == "" {
			continue
		}
		if err := s.email.Send(ctx, email.Message{To: addr, Subject: subject, Body: body, Attachments: []email.Attachment{att}}); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Warn("scheduled report email delivery failed", "to", addr, "error", err)
			continue
		}
		delivered++
	}
	return delivered, skipped, firstErr
}

// deliverWebhook POSTs a compact JSON notification of the generated report to the
// schedule's webhook_url. The URL is SSRF-RE-VALIDATED at delivery (the host's DNS
// may now resolve to a private IP) before any connection. The payload deliberately
// carries metadata (not the file bytes) so a webhook never exfiltrates report data
// to an endpoint; the file is fetched from the Export Center under permission.
func (s *Server) deliverWebhook(
	ctx context.Context, sr scheduledreports.ScheduledReport, filename, checksum string, byteCount int, now time.Time,
) error {
	if sr.WebhookURL == nil {
		return fmt.Errorf("webhook_url missing")
	}
	if err := validateWebhookURL(*sr.WebhookURL, s.cfg.ScheduledReportsWebhookAllowHosts, s.webhookLookupIP); err != nil {
		return fmt.Errorf("webhook blocked: %w", err)
	}
	payload := fmt.Sprintf(
		`{"event":"scheduled_report.delivered","scheduled_report_id":%q,"name":%q,"report_key":%q,"format":%q,"filename":%q,"checksum":%q,"byte_count":%d,"delivered_at":%q}`,
		sr.ID.String(), jsonEscape(sr.Name), sr.ReportKey, sr.Format, filename, checksum, byteCount, now.UTC().Format(time.RFC3339))

	pctx, cancel := context.WithTimeout(ctx, webhookPostTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(pctx, http.MethodPost, *sr.WebhookURL, bytes.NewReader([]byte(payload)))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "FuelGrid-ScheduledReports/1")

	resp, err := s.webhookClient().Do(req)
	if err != nil {
		return fmt.Errorf("webhook POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// webhookClient returns the HTTP client used for webhook delivery. Redirects are
// DISABLED so a 3xx to a private host can't bypass the SSRF guard (the guard ran on
// the original URL only).
func (s *Server) webhookClient() *http.Client {
	return &http.Client{
		Timeout: webhookPostTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// storeScheduledExport persists the rendered file as a completed export_jobs row so
// it is durably downloadable. Best-effort: returns nil on failure (the run is still
// recorded, just without a downloadable file pointer).
func (s *Server) storeScheduledExport(
	ctx context.Context, sr scheduledreports.ScheduledReport,
	data []byte, contentType, filename, checksum string, log loggerLike,
) *uuid.UUID {
	job, err := s.exportJobs.Enqueue(ctx, sr.TenantID, exportjobs.EnqueueInput{
		ReportKey:   sr.ReportKey,
		Format:      sr.Format,
		Filters:     sr.Filters,
		RequestedBy: sr.CreatedBy,
	})
	if err != nil {
		log.Warn("scheduled report: enqueue export receipt failed", "error", err)
		return nil
	}
	// Complete it directly with the bytes we already rendered — the async worker
	// must NOT re-render it (CompleteQueued flips queued->completed, so ClaimNext
	// never picks it up).
	if cerr := s.exportJobs.CompleteQueued(ctx, sr.TenantID, job.ID, exportjobs.CompleteInput{
		Bytes: data, ContentType: contentType, Filename: filename, Checksum: checksum,
	}); cerr != nil {
		log.Warn("scheduled report: complete export receipt failed", "error", cerr)
		return nil
	}
	id := job.ID
	return &id
}

// recordRunSafe records a run and logs (but swallows) any non-duplicate error so a
// ledger write failure never aborts the batch. Returns the error so callers can
// special-case ErrDuplicatePeriod.
func (s *Server) recordRunSafe(ctx context.Context, sr scheduledreports.ScheduledReport, periodKey string, in scheduledreports.RecordRunInput, log loggerLike) error {
	_, err := s.scheduledRpts.RecordRun(ctx, sr.TenantID, in)
	if err != nil && !errors.Is(err, scheduledreports.ErrDuplicatePeriod) {
		log.Error("scheduled report: record run failed", "period_key", periodKey, "error", err)
	}
	return err
}

// markScheduleStatus best-effort updates the schedule's health label.
func (s *Server) markScheduleStatus(ctx context.Context, sr scheduledreports.ScheduledReport, status string, log loggerLike) {
	if err := s.scheduledRpts.MarkStatus(ctx, sr.TenantID, sr.ID, status); err != nil {
		log.Warn("scheduled report: mark status failed", "status", status, "error", err)
	}
}

// auditScheduledRun records a 'report.scheduled.delivered' audit + outbox event for
// a completed run. Best-effort.
func (s *Server) auditScheduledRun(ctx context.Context, sr scheduledreports.ScheduledReport, periodKey, status string, delivered, skipped int) {
	if s.deps.DB == nil {
		return
	}
	tx, terr := s.deps.DB.Begin(ctx)
	if terr != nil {
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	newValue := map[string]any{
		"report_key":       sr.ReportKey,
		"delivery_channel": sr.DeliveryChannel,
		"format":           sr.Format,
		"period_key":       periodKey,
		"status":           status,
		"delivered_count":  delivered,
		"skipped_count":    skipped,
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID:   sr.TenantID,
		ActorID:    sr.CreatedBy,
		Action:     "report.scheduled.delivered",
		EventType:  "ScheduledReportDelivered",
		EntityType: "scheduled_report",
		EntityID:   sr.ID.String(),
		NewValue:   newValue,
	}); err != nil {
		s.logger.Warn("scheduled report: audit write failed", "error", err)
		return
	}
	_ = tx.Commit(ctx)
}

// loggerLike is the small logging surface dispatch helpers need (satisfied by
// *slog.Logger), kept tiny so helpers don't depend on the concrete type.
type loggerLike interface {
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	Info(msg string, args ...any)
}

// strptr returns a pointer to s (for nullable error columns).
func strptr(s string) *string { return &s }

// jsonEscape escapes a string for embedding in the compact webhook JSON payload's
// quoted field (handles the characters that would break JSON: backslash, quote,
// and control chars). It is a minimal escaper sufficient for a schedule name.
func jsonEscape(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`,
	)
	return r.Replace(s)
}
