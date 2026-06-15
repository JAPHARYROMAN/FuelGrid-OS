// Package exportjobs is the data layer for the report export-jobs surface
// (Feature 10.7 + Reports Center Phase 13 — the Export Center).
//
// An export job is a durable receipt AND a unit of asynchronous work: a row
// records what was requested (report_key, format, filters, the requesting
// actor) and progresses through a queued -> running -> completed/failed
// lifecycle. The rendered file BYTES are stored inline in this table
// (result_bytes), so the download is served straight from Postgres with NO
// external blob store. An advisory-locked worker (internal/server) claims
// queued jobs, re-checks the actor's permission at generation time, re-runs the
// report, renders the file, and stamps the result here.
//
// The pre-existing synchronous file endpoints stay authoritative for their own
// bytes; this package adds the durable async queue beside them.
package exportjobs

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// ErrNotFound is returned when a job is absent for the tenant.
var ErrNotFound = errors.New("exportjobs: job not found")

// Status values for an export job's lifecycle.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

// Job is one recorded export request, its lifecycle status, and (once the worker
// finishes) the rendered file's metadata. ResultBytes is loaded only by the
// download path (GetResult); the list/get views never drag the bytea through.
type Job struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	ReportKey   string
	Format      string
	Filters     map[string]string
	Status      string
	FileURL     *string
	FileName    *string
	FileSize    *int64
	Error       *string
	RequestedBy uuid.UUID
	CreatedAt   time.Time

	// Async worker fields (migration 0112).
	ResultContentType *string
	ResultFilename    *string
	ResultSize        *int64
	ResultChecksum    *string
	StartedAt         *time.Time
	CompletedAt       *time.Time
	Attempts          int
}

// CreateInput is the immutable record of a synchronous (already-completed)
// export job — the back-compat receipt path the existing POST surface still
// uses when it maps onto an existing file URL.
type CreateInput struct {
	ReportKey   string
	Format      string
	Filters     map[string]string
	Status      string
	FileURL     *string
	FileName    *string
	FileSize    *int64
	Error       *string
	RequestedBy uuid.UUID
}

// EnqueueInput is a new ASYNC job: it carries everything the worker needs to
// re-run the report later — the report key, the filters, the format, and the
// requesting actor (so permission can be re-checked at generation time).
type EnqueueInput struct {
	ReportKey   string
	Format      string
	Filters     map[string]string
	RequestedBy uuid.UUID
}

// CompleteInput stamps a finished render onto a running job.
type CompleteInput struct {
	Bytes       []byte
	ContentType string
	Filename    string
	Checksum    string
}

type Repo struct{ pool *database.Pool }

// New constructs the repo over the connection pool.
func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

// jobColumns is the view-shape projection (everything EXCEPT the result_bytes
// bytea, which is fetched separately by GetResult so a status read never loads
// the file payload).
const jobColumns = `
    id, tenant_id, report_key, format, filters, status,
    file_url, file_name, file_size, error, requested_by, created_at,
    result_content_type, result_filename, result_size, result_checksum,
    started_at, completed_at, attempts
`

func scanJob(row pgx.Row, j *Job) error {
	return row.Scan(
		&j.ID, &j.TenantID, &j.ReportKey, &j.Format, &j.Filters, &j.Status,
		&j.FileURL, &j.FileName, &j.FileSize, &j.Error, &j.RequestedBy, &j.CreatedAt,
		&j.ResultContentType, &j.ResultFilename, &j.ResultSize, &j.ResultChecksum,
		&j.StartedAt, &j.CompletedAt, &j.Attempts,
	)
}

// Create persists an already-resolved (synchronous) export job and returns the
// stored row. Used by the back-compat receipt path.
func (r *Repo) Create(ctx context.Context, tenantID uuid.UUID, in CreateInput) (*Job, error) {
	filters := in.Filters
	if filters == nil {
		filters = map[string]string{}
	}
	var j Job
	if err := scanJob(r.pool.QueryRow(ctx, `
		INSERT INTO export_jobs
		    (tenant_id, report_key, format, filters, status, file_url, file_name, file_size, error, requested_by, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		        CASE WHEN $5 IN ('completed','failed') THEN now() ELSE NULL END)
		RETURNING `+jobColumns,
		tenantID, in.ReportKey, in.Format, filters, in.Status,
		in.FileURL, in.FileName, in.FileSize, in.Error, in.RequestedBy,
	), &j); err != nil {
		return nil, err
	}
	return &j, nil
}

// Enqueue inserts a new ASYNC job in 'queued' status for the worker to pick up.
func (r *Repo) Enqueue(ctx context.Context, tenantID uuid.UUID, in EnqueueInput) (*Job, error) {
	filters := in.Filters
	if filters == nil {
		filters = map[string]string{}
	}
	var j Job
	if err := scanJob(r.pool.QueryRow(ctx, `
		INSERT INTO export_jobs
		    (tenant_id, report_key, format, filters, status, requested_by)
		VALUES ($1, $2, $3, $4, 'queued', $5)
		RETURNING `+jobColumns,
		tenantID, in.ReportKey, in.Format, filters, in.RequestedBy,
	), &j); err != nil {
		return nil, err
	}
	return &j, nil
}

// Get returns one export job for the tenant (view shape, no bytes), or ErrNotFound.
func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Job, error) {
	var j Job
	err := scanJob(r.pool.QueryRow(ctx, `
		SELECT `+jobColumns+` FROM export_jobs WHERE tenant_id = $1 AND id = $2
	`, tenantID, id), &j)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// ListPage returns a page of export jobs for the tenant, newest first (with id
// as a tiebreaker for stable paging), applying the supplied limit and offset.
func (r *Repo) ListPage(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]Job, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+jobColumns+` FROM export_jobs
		WHERE tenant_id = $1 ORDER BY created_at DESC, id LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		var j Job
		if err := scanJob(rows, &j); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ClaimNext atomically claims the oldest queued job across ALL tenants, moving
// it to 'running' and bumping attempts. It runs cross-tenant on the owner pool
// (the worker is background work, like the scheduler) and uses FOR UPDATE SKIP
// LOCKED so concurrent workers/replicas never claim the same row and never
// block on each other. Returns (nil, nil) when the queue is empty. The returned
// Job carries no result bytes (none exist yet).
func (r *Repo) ClaimNext(ctx context.Context) (*Job, error) {
	var j Job
	err := scanJob(r.pool.QueryRow(ctx, `
		UPDATE export_jobs SET
		    status     = 'running',
		    started_at = now(),
		    attempts   = attempts + 1
		WHERE id = (
		    SELECT id FROM export_jobs
		    WHERE status = 'queued'
		    ORDER BY created_at
		    FOR UPDATE SKIP LOCKED
		    LIMIT 1
		)
		RETURNING `+jobColumns), &j)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// ReclaimStale recovers jobs that were claimed ('running') but never reached a
// terminal status — the worker process was SIGKILLed / OOM-killed / evicted after
// the claim committed but before Complete/Fail. Such a row would otherwise be
// wedged in 'running' forever (ClaimNext only selects 'queued'). For every running
// row whose started_at is older than staleBefore, this:
//
//   - permanently FAILS it (status='failed') when attempts >= maxAttempts — a
//     poison job that keeps crashing the worker is capped and never retried again;
//   - otherwise RE-QUEUES it (status='queued', started_at=NULL) so the next claim
//     re-runs it (with a fresh permission re-check at generation).
//
// It runs cross-tenant on the owner pool (background work, like ClaimNext) under
// the worker's advisory lock, so only one replica reclaims per tick. Returns the
// number of rows reclaimed (failed + re-queued) for logging.
func (r *Repo) ReclaimStale(ctx context.Context, staleBefore time.Time, maxAttempts int) (int64, error) {
	ct, err := r.pool.Exec(ctx, `
		UPDATE export_jobs SET
		    status       = CASE WHEN attempts >= $2 THEN 'failed' ELSE 'queued' END,
		    started_at   = CASE WHEN attempts >= $2 THEN started_at ELSE NULL END,
		    completed_at = CASE WHEN attempts >= $2 THEN now() ELSE NULL END,
		    error        = CASE WHEN attempts >= $2
		                        THEN 'export worker stopped before this job finished (retry limit reached)'
		                        ELSE error END
		WHERE status = 'running' AND started_at IS NOT NULL AND started_at < $1
	`, staleBefore, maxAttempts)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

// Complete stamps a successful render onto a running job: it stores the file
// bytes + metadata inline and moves the job to 'completed'. Idempotent on
// redelivery — a job already in a terminal state is left untouched (the WHERE
// guards on status = 'running'), so a retried completion never double-writes.
func (r *Repo) Complete(ctx context.Context, tenantID, id uuid.UUID, in CompleteInput) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE export_jobs SET
		    status              = 'completed',
		    result_bytes        = $3,
		    result_content_type = $4,
		    result_filename     = $5,
		    result_size         = $6,
		    result_checksum     = $7,
		    file_name           = $5,
		    file_size           = $6,
		    error               = NULL,
		    completed_at        = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'running'
	`, tenantID, id, in.Bytes, in.ContentType, in.Filename, int64(len(in.Bytes)), in.Checksum)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Fail moves a running job to 'failed' with a human-readable reason. Idempotent:
// guards on status = 'running' so a terminal job is never clobbered.
func (r *Repo) Fail(ctx context.Context, tenantID, id uuid.UUID, reason string) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE export_jobs SET
		    status       = 'failed',
		    error        = $3,
		    completed_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'running'
	`, tenantID, id, reason)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetResult loads a completed job's stored file bytes for the download path. It
// returns ErrNotFound when the job is absent for the tenant; found is false when
// the job exists but has no stored bytes (e.g. a not-yet-completed job, or a
// legacy synchronous receipt that only carries a file_url). The job's view
// metadata is returned alongside so the caller can set headers / re-check
// permission without a second read.
func (r *Repo) GetResult(ctx context.Context, tenantID, id uuid.UUID) (job *Job, data []byte, found bool, err error) {
	var j Job
	var bytesPtr *[]byte
	row := r.pool.QueryRow(ctx, `
		SELECT `+jobColumns+`, result_bytes
		FROM export_jobs WHERE tenant_id = $1 AND id = $2
	`, tenantID, id)
	if scanErr := row.Scan(
		&j.ID, &j.TenantID, &j.ReportKey, &j.Format, &j.Filters, &j.Status,
		&j.FileURL, &j.FileName, &j.FileSize, &j.Error, &j.RequestedBy, &j.CreatedAt,
		&j.ResultContentType, &j.ResultFilename, &j.ResultSize, &j.ResultChecksum,
		&j.StartedAt, &j.CompletedAt, &j.Attempts, &bytesPtr,
	); scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return nil, nil, false, ErrNotFound
		}
		return nil, nil, false, scanErr
	}
	if bytesPtr == nil || len(*bytesPtr) == 0 {
		return &j, nil, false, nil
	}
	return &j, *bytesPtr, true, nil
}
