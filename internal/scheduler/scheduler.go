// Package scheduler runs recurring business processes alongside the API on a
// timer, the way internal/events.Publisher drains the outbox: a runner started
// on Start and stopped on Shutdown, one goroutine per registered job ticking on
// the job's interval.
//
// MULTI-INSTANCE SAFETY. Several API replicas may run this scheduler against the
// same database. Before a job does any work it takes a Postgres *session-level*
// advisory lock keyed by the job (pg_try_advisory_lock); only the replica that
// wins the lock runs the job this tick, and the others skip without blocking.
// The lock is released the moment the job finishes, so the next tick is free to
// land on whichever replica grabs it first. Advisory locks are cheap, require no
// schema, and are automatically released if the holding connection dies — so a
// crashed replica never wedges a job.
//
// ERROR ISOLATION. Each job runs in its own goroutine and its own
// panic-recovered execution, and every failure is logged and recorded but never
// propagated: one job erroring (or panicking) never stops the others, and a job
// erroring on one tick is retried on the next.
//
// VISIBILITY. When the job_runs ledger exists (migration 0079) each execution
// appends a row (running -> success/failure/skipped); a per-job Prometheus
// metric (success/failure count + duration) is recorded regardless. Ledger
// failures are themselves logged and swallowed so telemetry can never take down
// a job.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/observability"
)

// JobFunc is the body of a recurring job. It runs cross-tenant on the owner
// pool and returns a short human-readable detail string (recorded in the
// job_runs ledger) plus an error. A returned error marks the run a failure but
// never stops the scheduler.
type JobFunc func(ctx context.Context) (detail string, err error)

// Job is a registered recurring task: a stable name (also the advisory-lock
// key and metric label), the interval between runs, and the body. An interval
// <= 0 means the job is disabled and is never registered.
type Job struct {
	Name     string
	Interval time.Duration
	Run      JobFunc
}

// Scheduler owns the per-job ticker goroutines and their lifecycle. It mirrors
// events.Publisher's Start/Stop shape so main.go wires it the same way.
type Scheduler struct {
	pool        *database.Pool
	metrics     *observability.Metrics
	logger      *slog.Logger
	jobs        []Job
	lockTimeout time.Duration

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

// New wires a scheduler. Jobs with a non-positive interval are dropped here so
// the runner only ever ticks enabled jobs. lockTimeout bounds each individual
// job execution; <= 0 falls back to a safe default.
func New(pool *database.Pool, metrics *observability.Metrics, logger *slog.Logger, lockTimeout time.Duration, jobs ...Job) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	if lockTimeout <= 0 {
		lockTimeout = 10 * time.Minute
	}
	enabled := make([]Job, 0, len(jobs))
	for _, j := range jobs {
		if j.Interval > 0 && j.Run != nil {
			enabled = append(enabled, j)
		}
	}
	return &Scheduler{
		pool:        pool,
		metrics:     metrics,
		logger:      logger,
		jobs:        enabled,
		lockTimeout: lockTimeout,
		stopCh:      make(chan struct{}),
	}
}

// Start launches one goroutine per enabled job. Idempotent. With no enabled
// jobs (every interval disabled, e.g. a zero-value Config) it is a no-op, so a
// test harness that never sets SCHEDULER_* runs nothing.
func (s *Scheduler) Start() {
	s.startOnce.Do(func() {
		if len(s.jobs) == 0 {
			s.logger.Info("scheduler started with no enabled jobs")
			return
		}
		for _, j := range s.jobs {
			s.wg.Add(1)
			go s.runJob(j)
		}
		names := make([]string, len(s.jobs))
		for i, j := range s.jobs {
			names[i] = j.Name
		}
		s.logger.Info("scheduler started", "jobs", names)
	})
}

// Stop signals every job goroutine to exit and waits for them to drain, up to
// ctx's deadline. Idempotent. A job mid-execution when Stop is called runs to
// completion (or until its own lock timeout / ctx) before its goroutine exits.
func (s *Scheduler) Stop(ctx context.Context) error {
	var stopErr error
	s.stopOnce.Do(func() {
		close(s.stopCh)
		done := make(chan struct{})
		go func() {
			s.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
			s.logger.Info("scheduler stopped")
		case <-ctx.Done():
			stopErr = ctx.Err()
		}
	})
	return stopErr
}

// runJob is one job's ticker loop. Unlike the outbox publisher we deliberately
// do NOT fire an immediate tick on startup: these jobs are periodic maintenance
// (hourly revenue compute, etc.), not latency-sensitive drains, and a thundering
// herd of every job firing at boot across every replica is undesirable. The
// first run lands one interval in.
func (s *Scheduler) runJob(j Job) {
	defer s.wg.Done()
	t := time.NewTicker(j.Interval)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			s.execute(j)
		}
	}
}

// execute runs one tick of a job: contend for the advisory lock, and if won,
// record + run the body under a bounded context with panic recovery. All paths
// are non-fatal to the loop.
func (s *Scheduler) execute(j Job) {
	// Bound the whole tick (lock attempt + body) so a stuck job can't hold its
	// advisory lock — and its database connection — indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), s.lockTimeout)
	defer cancel()

	log := s.logger.With("job", j.Name)

	// Take a session-level advisory lock on a dedicated connection. Holding the
	// connection for the duration of the job is what scopes the lock to the run;
	// releasing the connection (or unlocking explicitly) frees it.
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		log.Warn("scheduler: acquire connection", "error", err)
		return
	}
	defer conn.Release()

	lockKey := jobLockKey(j.Name)
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lockKey).Scan(&locked); err != nil {
		log.Warn("scheduler: advisory lock attempt", "error", err)
		return
	}
	if !locked {
		// Another replica is running this job this tick — expected, not an error.
		log.Debug("scheduler: job skipped, lock held elsewhere")
		return
	}
	defer func() {
		// Best-effort unlock on the same connection. If it fails the connection
		// Release still drops the session lock, so the job is never wedged.
		if _, uerr := conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", lockKey); uerr != nil {
			log.Warn("scheduler: advisory unlock", "error", uerr)
		}
	}()

	started := time.Now()
	runID := s.ledgerStart(ctx, j.Name)
	log.Info("scheduler: job started")

	// Thread this run's own ledger id into the body's context. The
	// once-per-period guard (ReportDeps.sentSince) treats an in-period 'running'
	// row as "already handled" to close the crash-after-send double-delivery
	// window — but it must NOT count the row this very tick just inserted, or the
	// job would suppress itself and never send. The body excludes withRunID(ctx).
	if runID != nil {
		ctx = withRunID(ctx, *runID)
	}

	detail, runErr := s.runBody(ctx, j)
	dur := time.Since(started)

	switch {
	case runErr != nil:
		s.metrics.ObserveJob(j.Name, "failure", dur)
		s.ledgerFinish(runID, "failure", truncateDetail(runErr.Error()))
		log.Error("scheduler: job failed", "duration", dur, "error", runErr)
	default:
		s.metrics.ObserveJob(j.Name, "success", dur)
		s.ledgerFinish(runID, "success", truncateDetail(detail))
		log.Info("scheduler: job finished", "duration", dur, "detail", detail)
	}
}

// runBody invokes the job, converting a panic into an error so a panicking job
// is isolated exactly like one that returns an error.
func (s *Scheduler) runBody(ctx context.Context, j Job) (detail string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return j.Run(ctx)
}

// jobLockKey derives a stable 64-bit advisory-lock key from a job name via
// FNV-1a. pg_try_advisory_lock takes a bigint; collisions across the small,
// fixed set of job names would only ever cause two distinct jobs to serialise,
// never a correctness bug, but the 64-bit space makes even that vanishingly
// unlikely.
func jobLockKey(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("fuelgrid.scheduler:" + name))
	return int64(h.Sum64()) //nolint:gosec // intentional bit-reinterpret to fit bigint; value is a lock key, not a number
}

// truncateDetail caps the ledger detail string so a runaway message (e.g. a
// huge error) can't bloat the row.
func truncateDetail(s string) string {
	const maxLen = 1000
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

// ledgerStart appends a 'running' row to job_runs and returns its id, or a nil
// id when the ledger is unavailable (table missing, DB blip). Failures are
// swallowed: the ledger is best-effort telemetry, not a gate on the job.
func (s *Scheduler) ledgerStart(ctx context.Context, jobName string) *string {
	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO job_runs (job_name, status) VALUES ($1, 'running') RETURNING id::text`,
		jobName,
	).Scan(&id)
	if err != nil {
		if !isMissingLedger(err) {
			s.logger.Warn("scheduler: ledger start", "job", jobName, "error", err)
		}
		return nil
	}
	return &id
}

// ledgerFinish stamps the terminal status on a previously-started row. A nil id
// (ledger unavailable at start) makes this a no-op. It deliberately ignores the
// job's context and uses its own short-lived background context so the terminal
// write still lands even if the job's ctx was cancelled by the lock-timeout
// deadline.
func (s *Scheduler) ledgerFinish(id *string, status, detail string) {
	if id == nil {
		return
	}
	wctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var detailArg any
	if detail != "" {
		detailArg = detail
	}
	if _, err := s.pool.Exec(wctx,
		`UPDATE job_runs SET status = $2, detail = $3, finished_at = now() WHERE id = $1::uuid`,
		*id, status, detailArg,
	); err != nil && !isMissingLedger(err) {
		s.logger.Warn("scheduler: ledger finish", "id", *id, "error", err)
	}
}

// runIDCtxKey is the context key under which execute stashes the current run's
// job_runs ledger id, so a job body's once-per-period guard can exclude its own
// in-flight 'running' row. Unexported type prevents collisions.
type runIDCtxKey struct{}

// withRunID returns ctx carrying the current ledger run id.
func withRunID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, runIDCtxKey{}, id)
}

// runIDFromContext returns the current ledger run id and whether one was set.
// When the ledger is unavailable (no job_runs table / insert failed) no id is
// threaded and the second return is false.
func runIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(runIDCtxKey{}).(string)
	return id, ok && id != ""
}

// isMissingLedger reports whether err is "relation job_runs does not exist"
// (Postgres undefined_table, 42P01). A deployment that hasn't applied migration
// 0079 still runs every job; it just gets no ledger rows, so we don't spam logs.
func isMissingLedger(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "42P01"
	}
	return false
}

// withTx runs fn inside a transaction on the owner pool, committing on success
// and rolling back on error. It's a small helper the per-tenant jobs use so each
// tenant's recompute is atomic and isolated. A context.Canceled commit error
// (shutdown mid-job) is treated as a rollback, not a failure to surface.
func withTx(ctx context.Context, pool *database.Pool, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
