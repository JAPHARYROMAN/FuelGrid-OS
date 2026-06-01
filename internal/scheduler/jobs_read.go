package scheduler

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// JobRun is one row of the job_runs visibility ledger (migration 0079), shaped
// for the read-only admin endpoint. Duration is derived (finished_at -
// started_at) and is nil for a run still in progress.
type JobRun struct {
	ID         uuid.UUID
	JobName    string
	StartedAt  time.Time
	FinishedAt *time.Time
	Status     string
	Detail     *string
	Duration   *time.Duration
}

// ReadRepo reads the job_runs ledger for operator visibility. It runs on the
// OWNER pool: job_runs is a cross-tenant SYSTEM table with no tenant_id and no
// RLS policy (see migration 0079), so it is owner-only by construction and must
// never be reached by the request-scoped fuelgrid_app role.
type ReadRepo struct{ pool *database.Pool }

// NewReadRepo constructs the job_runs read repository over the owner pool.
func NewReadRepo(pool *database.Pool) *ReadRepo { return &ReadRepo{pool: pool} }

// LatestPerJob returns the most recent run for every distinct job that has ever
// run, newest started_at first. This is the dominant ops query — "what is each
// job's last run, status, and duration?" — backing the System health page.
//
// A deployment that has not applied migration 0079 (no job_runs table) returns
// an empty slice rather than an error, so the endpoint degrades gracefully to
// "no runs recorded yet".
func (r *ReadRepo) LatestPerJob(ctx context.Context) ([]JobRun, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (job_name)
			id, job_name, started_at, finished_at, status, detail
		FROM job_runs
		ORDER BY job_name, started_at DESC`)
	if err != nil {
		if isMissingLedger(err) {
			return []JobRun{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	out, err := scanJobRuns(rows)
	if err != nil {
		return nil, err
	}
	// DISTINCT ON forces a job_name ordering; re-sort newest-run-first so the
	// freshest activity is at the top of the operator's list.
	sortByStartedDesc(out)
	return out, nil
}

// RecentRuns returns the most recent runs across all jobs, newest first, capped
// at limit. It backs an optional history view. Missing-ledger degrades to an
// empty slice exactly like LatestPerJob.
func (r *ReadRepo) RecentRuns(ctx context.Context, limit int) ([]JobRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, job_name, started_at, finished_at, status, detail
		FROM job_runs
		ORDER BY started_at DESC, id DESC
		LIMIT $1`, limit)
	if err != nil {
		if isMissingLedger(err) {
			return []JobRun{}, nil
		}
		return nil, err
	}
	defer rows.Close()
	return scanJobRuns(rows)
}

// scanJobRuns materialises rows into JobRun values, deriving Duration from the
// start/finish timestamps when the run has finished.
func scanJobRuns(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]JobRun, error) {
	out := []JobRun{}
	for rows.Next() {
		var jr JobRun
		if err := rows.Scan(&jr.ID, &jr.JobName, &jr.StartedAt, &jr.FinishedAt, &jr.Status, &jr.Detail); err != nil {
			return nil, err
		}
		if jr.FinishedAt != nil {
			d := jr.FinishedAt.Sub(jr.StartedAt)
			jr.Duration = &d
		}
		out = append(out, jr)
	}
	return out, rows.Err()
}

// sortByStartedDesc orders runs newest-started first using a simple insertion
// sort — the set is tiny (one row per job), so this avoids pulling in sort for
// a handful of elements.
func sortByStartedDesc(runs []JobRun) {
	for i := 1; i < len(runs); i++ {
		for j := i; j > 0 && runs[j].StartedAt.After(runs[j-1].StartedAt); j-- {
			runs[j], runs[j-1] = runs[j-1], runs[j]
		}
	}
}
