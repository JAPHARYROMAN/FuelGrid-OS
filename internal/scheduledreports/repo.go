package scheduledreports

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// ErrNotFound is returned when a schedule (or run) is absent for the tenant.
var ErrNotFound = errors.New("scheduledreports: not found")

// Delivery channels + run statuses (mirror the DB CHECK constraints).
const (
	ChannelInApp   = "in_app"
	ChannelEmail   = "email"
	ChannelWebhook = "webhook"

	StatusActive = "active"
	StatusPaused = "paused"
	StatusError  = "error"

	RunSuccess          = "success"
	RunPartial          = "partial"
	RunFailed           = "failed"
	RunSkippedForbidden = "skipped_forbidden"

	RecipientUser  = "user"
	RecipientEmail = "email"
)

// Recipient is one delivery target. A "user" recipient's Value is a user UUID
// (its own permission identity, re-checked per run); an "email" recipient's Value
// is an address (permission-anchored on the schedule owner).
type Recipient struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Schedule definition row (scheduled_reports). Filters + Recipients + the
// recurrence Schedule are stored as jsonb. WebhookURL is set only for the webhook
// channel. NextRunAt is the authoritative due-time the worker selects on.
type ScheduledReport struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	ReportKey       string
	Name            string
	Filters         map[string]string
	Schedule        Schedule
	Recipients      []Recipient
	DeliveryChannel string
	Format          string
	WebhookURL      *string
	CreatedBy       uuid.UUID
	Enabled         bool
	LastRunAt       *time.Time
	NextRunAt       time.Time
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time

	// DueAt is the instant this row was DUE when ClaimDue claimed it (its
	// next_run_at BEFORE the claim advanced it). It is NOT a stored column — it is
	// populated only on rows returned by ClaimDue so the dispatcher can derive the
	// period key from the period the run is FOR, not from the wall-clock of the tick
	// (which drifts when a tick fires late and would mis-key/defeat the idempotency
	// UNIQUE across a period boundary). Zero on rows loaded by Get/List.
	DueAt time.Time
}

// Run is one recorded generation (scheduled_report_runs). PeriodKey is the
// idempotency identity of the logical period; ExportJobID points at the rendered
// file when one was produced.
type Run struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	ScheduledReportID uuid.UUID
	PeriodKey         string
	RunAt             time.Time
	Status            string
	ExportJobID       *uuid.UUID
	NotificationIDs   []uuid.UUID
	DeliveredCount    int
	SkippedCount      int
	Error             *string
	CreatedAt         time.Time
}

// CreateInput is a new schedule definition.
type CreateInput struct {
	ReportKey       string
	Name            string
	Filters         map[string]string
	Schedule        Schedule
	Recipients      []Recipient
	DeliveryChannel string
	Format          string
	WebhookURL      *string
	CreatedBy       uuid.UUID
	NextRunAt       time.Time
}

// UpdateInput replaces a schedule's mutable definition fields.
type UpdateInput struct {
	Name            string
	Filters         map[string]string
	Schedule        Schedule
	Recipients      []Recipient
	DeliveryChannel string
	Format          string
	WebhookURL      *string
	NextRunAt       time.Time
}

// Repo is the scheduled-reports data layer.
type Repo struct{ pool *database.Pool }

// New constructs the repo.
func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const scheduleColumns = `
    id, tenant_id, report_key, name, filters, schedule, recipients,
    delivery_channel, format, webhook_url, created_by, enabled,
    last_run_at, next_run_at, status, created_at, updated_at`

func scanSchedule(row pgx.Row) (ScheduledReport, error) {
	var (
		sr           ScheduledReport
		scheduleRaw  json.RawMessage
		recipientRaw json.RawMessage
	)
	if err := row.Scan(
		&sr.ID, &sr.TenantID, &sr.ReportKey, &sr.Name, &sr.Filters, &scheduleRaw, &recipientRaw,
		&sr.DeliveryChannel, &sr.Format, &sr.WebhookURL, &sr.CreatedBy, &sr.Enabled,
		&sr.LastRunAt, &sr.NextRunAt, &sr.Status, &sr.CreatedAt, &sr.UpdatedAt,
	); err != nil {
		return ScheduledReport{}, err
	}
	if sr.Filters == nil {
		sr.Filters = map[string]string{}
	}
	sched, err := scheduleFromJSON(scheduleRaw)
	if err != nil {
		return ScheduledReport{}, err
	}
	sr.Schedule = sched
	if len(recipientRaw) > 0 {
		if err := json.Unmarshal(recipientRaw, &sr.Recipients); err != nil {
			return ScheduledReport{}, err
		}
	}
	if sr.Recipients == nil {
		sr.Recipients = []Recipient{}
	}
	return sr, nil
}

// Create inserts a new schedule and returns the stored row. Like every method on
// this repo it runs on the OWNER pool and enforces tenant isolation by the explicit
// tenant_id it is given (taken from the authenticated actor); the table's RLS policy
// is defense-in-depth, not the primary boundary.
func (r *Repo) Create(ctx context.Context, tenantID uuid.UUID, in CreateInput) (*ScheduledReport, error) {
	filters := in.Filters
	if filters == nil {
		filters = map[string]string{}
	}
	recipients := in.Recipients
	if recipients == nil {
		recipients = []Recipient{}
	}
	scheduleJSON, err := json.Marshal(in.Schedule)
	if err != nil {
		return nil, err
	}
	recipientsJSON, err := json.Marshal(recipients)
	if err != nil {
		return nil, err
	}
	sr, err := scanSchedule(r.pool.QueryRow(ctx, `
		INSERT INTO scheduled_reports
		    (tenant_id, report_key, name, filters, schedule, recipients,
		     delivery_channel, format, webhook_url, created_by, next_run_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+scheduleColumns,
		tenantID, in.ReportKey, in.Name, filters, scheduleJSON, recipientsJSON,
		in.DeliveryChannel, in.Format, in.WebhookURL, in.CreatedBy, in.NextRunAt,
	))
	if err != nil {
		return nil, err
	}
	return &sr, nil
}

// Get returns one schedule for the tenant, or ErrNotFound.
func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*ScheduledReport, error) {
	sr, err := scanSchedule(r.pool.QueryRow(ctx,
		`SELECT `+scheduleColumns+` FROM scheduled_reports WHERE tenant_id = $1 AND id = $2`,
		tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sr, nil
}

// ListPage returns a page of the tenant's schedules, newest first.
func (r *Repo) ListPage(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]ScheduledReport, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+scheduleColumns+` FROM scheduled_reports
		 WHERE tenant_id = $1 ORDER BY created_at DESC, id LIMIT $2 OFFSET $3`,
		tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScheduledReport{}
	for rows.Next() {
		sr, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sr)
	}
	return out, rows.Err()
}

// Update replaces a schedule's mutable definition. Returns ErrNotFound when the
// row is absent for the tenant.
func (r *Repo) Update(ctx context.Context, tenantID, id uuid.UUID, in UpdateInput) (*ScheduledReport, error) {
	filters := in.Filters
	if filters == nil {
		filters = map[string]string{}
	}
	recipients := in.Recipients
	if recipients == nil {
		recipients = []Recipient{}
	}
	scheduleJSON, err := json.Marshal(in.Schedule)
	if err != nil {
		return nil, err
	}
	recipientsJSON, err := json.Marshal(recipients)
	if err != nil {
		return nil, err
	}
	sr, err := scanSchedule(r.pool.QueryRow(ctx, `
		UPDATE scheduled_reports SET
		    name = $3, filters = $4, schedule = $5, recipients = $6,
		    delivery_channel = $7, format = $8, webhook_url = $9, next_run_at = $10
		WHERE tenant_id = $1 AND id = $2
		RETURNING `+scheduleColumns,
		tenantID, id, in.Name, filters, scheduleJSON, recipientsJSON,
		in.DeliveryChannel, in.Format, in.WebhookURL, in.NextRunAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sr, nil
}

// SetEnabled toggles a schedule on/off and aligns its status (active when enabled,
// paused when disabled). When re-enabling, nextRunAt is refreshed so a long-paused
// schedule does not immediately fire for every missed period. Returns ErrNotFound.
func (r *Repo) SetEnabled(ctx context.Context, tenantID, id uuid.UUID, enabled bool, nextRunAt time.Time) (*ScheduledReport, error) {
	status := StatusActive
	if !enabled {
		status = StatusPaused
	}
	sr, err := scanSchedule(r.pool.QueryRow(ctx, `
		UPDATE scheduled_reports SET enabled = $3, status = $4, next_run_at = $5
		WHERE tenant_id = $1 AND id = $2
		RETURNING `+scheduleColumns,
		tenantID, id, enabled, status, nextRunAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sr, nil
}

// Delete removes a schedule (its runs cascade). Returns ErrNotFound when absent.
func (r *Repo) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM scheduled_reports WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ClaimDue atomically claims up to `limit` enabled schedules whose next_run_at is
// at or before `now`, advancing each claimed row's next_run_at to its computed
// next instant IN THE SAME UPDATE. This is the idempotency anchor: a duplicated or
// concurrent tick can claim a given due row at most once (the advanced next_run_at
// no longer satisfies the predicate), exactly like export_jobs' FOR UPDATE SKIP
// LOCKED claim. It runs CROSS-TENANT on the owner pool (background work), so it
// returns the tenant_id on each row and the worker scopes every downstream action
// by it.
//
// Because next_run_at must be recomputed per row (from each row's own schedule),
// the claim is done in a short transaction: SELECT ... FOR UPDATE SKIP LOCKED the
// due rows, compute the next instant in Go, then UPDATE each. The SKIP LOCKED +
// per-row UPDATE within one tx is what serialises concurrent workers safely.
func (r *Repo) ClaimDue(ctx context.Context, now time.Time, limit int) ([]ScheduledReport, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT `+scheduleColumns+`
		FROM scheduled_reports
		WHERE enabled = true AND next_run_at <= $1
		ORDER BY next_run_at
		FOR UPDATE SKIP LOCKED
		LIMIT $2`,
		now, limit)
	if err != nil {
		return nil, err
	}
	var claimed []ScheduledReport
	for rows.Next() {
		sr, serr := scanSchedule(rows)
		if serr != nil {
			rows.Close()
			return nil, serr
		}
		claimed = append(claimed, sr)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Advance each claimed row's next_run_at past `now` (so the next tick won't
	// re-claim it) and record last_run_at. A row whose schedule no longer resolves
	// (shouldn't happen — Validate ran at write) is parked far in the future and
	// flagged 'error' so it stops re-firing rather than tight-looping.
	for i := range claimed {
		// Capture the DUE instant (the pre-advance next_run_at) BEFORE we overwrite
		// it. The dispatcher keys the run's period off this, not off the tick's
		// wall-clock, so a late tick still records the run under the period it was
		// due for and the (schedule, period_key) UNIQUE collapses duplicates correctly.
		claimed[i].DueAt = claimed[i].NextRunAt

		next, nerr := claimed[i].Schedule.NextRunAfter(now)
		status := claimed[i].Status
		if nerr != nil {
			next = now.AddDate(100, 0, 0)
			status = StatusError
		}
		if _, uerr := tx.Exec(ctx, `
			UPDATE scheduled_reports
			SET next_run_at = $2, last_run_at = $3, status = $4
			WHERE id = $1`,
			claimed[i].ID, next, now, status); uerr != nil {
			return nil, uerr
		}
		claimed[i].NextRunAt = next
		claimed[i].LastRunAt = &now
		claimed[i].Status = status
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return claimed, nil
}

// MarkStatus sets a schedule's health status (active/error) after a run. Owner
// pool; the worker calls it cross-tenant scoped by tenant_id.
func (r *Repo) MarkStatus(ctx context.Context, tenantID, id uuid.UUID, status string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE scheduled_reports SET status = $3 WHERE tenant_id = $1 AND id = $2`,
		tenantID, id, status)
	return err
}

// RecordRunInput captures one generation's outcome.
type RecordRunInput struct {
	ScheduledReportID uuid.UUID
	PeriodKey         string
	Status            string
	ExportJobID       *uuid.UUID
	NotificationIDs   []uuid.UUID
	DeliveredCount    int
	SkippedCount      int
	Error             *string
}

// ErrDuplicatePeriod is returned by RecordRun when a run for the same
// (schedule, period_key) already exists — the idempotency guard fired.
var ErrDuplicatePeriod = errors.New("scheduledreports: run for this period already recorded")

// RecordRun inserts a run-history row. The UNIQUE (scheduled_report_id,
// period_key) index makes a duplicate period a clean ErrDuplicatePeriod rather
// than a second delivery. Owner pool (the worker is cross-tenant); tenant_id is
// passed explicitly. The notification ids are stored as a jsonb array of strings.
func (r *Repo) RecordRun(ctx context.Context, tenantID uuid.UUID, in RecordRunInput) (*Run, error) {
	notifJSON, err := json.Marshal(uuidStrings(in.NotificationIDs))
	if err != nil {
		return nil, err
	}
	status := in.Status
	if status == "" {
		status = RunSuccess
	}
	var (
		run     Run
		notifs  json.RawMessage
		exportP *uuid.UUID
	)
	err = r.pool.QueryRow(ctx, `
		INSERT INTO scheduled_report_runs
		    (tenant_id, scheduled_report_id, period_key, status, export_job_id,
		     notification_ids, delivered_count, skipped_count, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, tenant_id, scheduled_report_id, period_key, run_at, status,
		          export_job_id, notification_ids, delivered_count, skipped_count, error, created_at`,
		tenantID, in.ScheduledReportID, in.PeriodKey, status, in.ExportJobID,
		notifJSON, in.DeliveredCount, in.SkippedCount, in.Error,
	).Scan(
		&run.ID, &run.TenantID, &run.ScheduledReportID, &run.PeriodKey, &run.RunAt, &run.Status,
		&exportP, &notifs, &run.DeliveredCount, &run.SkippedCount, &run.Error, &run.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicatePeriod
		}
		return nil, err
	}
	run.ExportJobID = exportP
	run.NotificationIDs = parseUUIDArray(notifs)
	return &run, nil
}

// ListRuns returns a schedule's recent runs, newest first. Tenant + schedule
// scoped on the owner pool (tenant isolation via the explicit tenant_id filter;
// RLS is defense-in-depth).
func (r *Repo) ListRuns(ctx context.Context, tenantID, scheduleID uuid.UUID, limit, offset int) ([]Run, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, scheduled_report_id, period_key, run_at, status,
		       export_job_id, notification_ids, delivered_count, skipped_count, error, created_at
		FROM scheduled_report_runs
		WHERE tenant_id = $1 AND scheduled_report_id = $2
		ORDER BY run_at DESC, id LIMIT $3 OFFSET $4`,
		tenantID, scheduleID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var (
			run     Run
			notifs  json.RawMessage
			exportP *uuid.UUID
		)
		if err := rows.Scan(
			&run.ID, &run.TenantID, &run.ScheduledReportID, &run.PeriodKey, &run.RunAt, &run.Status,
			&exportP, &notifs, &run.DeliveredCount, &run.SkippedCount, &run.Error, &run.CreatedAt,
		); err != nil {
			return nil, err
		}
		run.ExportJobID = exportP
		run.NotificationIDs = parseUUIDArray(notifs)
		out = append(out, run)
	}
	return out, rows.Err()
}

// uuidStrings renders a slice of uuids as strings for jsonb storage.
func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

// parseUUIDArray decodes a stored jsonb string array back into uuids, dropping
// any malformed entry (defensive — the writer only ever stores valid uuids).
func parseUUIDArray(raw json.RawMessage) []uuid.UUID {
	var strs []string
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &strs)
	}
	out := make([]uuid.UUID, 0, len(strs))
	for _, s := range strs {
		if id, err := uuid.Parse(s); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// isUniqueViolation reports whether err is a Postgres unique_violation (23505).
func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
