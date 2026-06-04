// Package exportjobs is the data layer for the report export-jobs surface
// (Feature 10.7). An export job is a durable receipt of a report export: what
// was requested (report_key, format, filters) and the resulting file's metadata.
// The file itself is still produced synchronously through the existing export
// path; the job row records it and powers the reporting hub's export history.
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

// Job is one recorded export request and its resulting file metadata.
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
}

// CreateInput is the immutable record of a completed (or failed) export job.
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

type Repo struct{ pool *database.Pool }

// New constructs the repo over the connection pool.
func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const jobColumns = `
    id, tenant_id, report_key, format, filters, status,
    file_url, file_name, file_size, error, requested_by, created_at
`

func scanJob(row pgx.Row, j *Job) error {
	return row.Scan(
		&j.ID, &j.TenantID, &j.ReportKey, &j.Format, &j.Filters, &j.Status,
		&j.FileURL, &j.FileName, &j.FileSize, &j.Error, &j.RequestedBy, &j.CreatedAt,
	)
}

// Create persists an export job and returns the stored row.
func (r *Repo) Create(ctx context.Context, tenantID uuid.UUID, in CreateInput) (*Job, error) {
	filters := in.Filters
	if filters == nil {
		filters = map[string]string{}
	}
	var j Job
	if err := scanJob(r.pool.QueryRow(ctx, `
		INSERT INTO export_jobs
		    (tenant_id, report_key, format, filters, status, file_url, file_name, file_size, error, requested_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+jobColumns,
		tenantID, in.ReportKey, in.Format, filters, in.Status,
		in.FileURL, in.FileName, in.FileSize, in.Error, in.RequestedBy,
	), &j); err != nil {
		return nil, err
	}
	return &j, nil
}

// Get returns one export job for the tenant, or ErrNotFound.
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
