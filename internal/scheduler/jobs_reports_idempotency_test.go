package scheduler

// DB-backed proof of the once-per-period idempotency guard for the canned
// report-email digests — the actual double-delivery protection, which the
// pure-unit tests (configured / before-send-hour / date math) never exercised.
//
// It covers the three states the guard must distinguish:
//   - a prior 'success' run this period  -> suppress (already delivered),
//   - a prior 'running' run this period  -> suppress (CRASH WINDOW: a replica
//     sent every email then died before flipping the ledger to 'success';
//     re-sending the full financial digest is the failure we must prevent),
//   - a prior 'failure' run this period  -> do NOT suppress (retry next tick),
// plus the critical self-exclusion: the tick's OWN in-flight 'running' row
// (inserted by scheduler.execute before the body runs, threaded on ctx via
// withRunID) must not make the job skip itself, or nothing would ever send.
//
// Gated on TEST_DATABASE_URL like the other integration tests; it stands up a
// minimal job_runs table on a throwaway DB so it does not depend on the full
// migration set.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

const jobRunsDDL = `
CREATE TABLE IF NOT EXISTS job_runs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_name    text NOT NULL,
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    status      text NOT NULL DEFAULT 'running',
    detail      text,
    CONSTRAINT chk_job_runs_status CHECK (status IN ('running', 'success', 'failure', 'skipped'))
);`

// idempotencyTestPool stands up an isolated job_runs table on the throwaway DB
// and returns a wrapped pool plus a cleanup that truncates between sub-tests.
func idempotencyTestPool(t *testing.T) (*database.Pool, context.Context) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run the report-digest idempotency test")
	}
	ctx := context.Background()
	pgxPool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pgxPool.Close)
	if _, err := pgxPool.Exec(ctx, jobRunsDDL); err != nil {
		t.Fatalf("create job_runs: %v", err)
	}
	if _, err := pgxPool.Exec(ctx, `TRUNCATE job_runs`); err != nil {
		t.Fatalf("truncate job_runs: %v", err)
	}
	return &database.Pool{Pool: pgxPool}, ctx
}

// insertRun appends a job_runs row with the given status and start time and
// returns its id, mimicking scheduler.ledgerStart/ledgerFinish.
func insertRun(t *testing.T, ctx context.Context, pool *database.Pool, jobName, status string, startedAt time.Time) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO job_runs (job_name, status, started_at) VALUES ($1, $2, $3) RETURNING id::text`,
		jobName, status, startedAt,
	).Scan(&id); err != nil {
		t.Fatalf("insert %s run: %v", status, err)
	}
	return id
}

func TestSentSinceIdempotency(t *testing.T) {
	pool, ctx := idempotencyTestPool(t)
	d := ReportDeps{Pool: pool}
	const job = "report_daily_close_digest"
	now := time.Date(2026, 6, 15, 8, 0, 0, 0, time.UTC)
	since := startOfDay(now)

	reset := func() {
		if _, err := pool.Exec(ctx, `TRUNCATE job_runs`); err != nil {
			t.Fatalf("truncate: %v", err)
		}
	}

	t.Run("no prior run sends", func(t *testing.T) {
		reset()
		sent, err := d.sentSince(ctx, job, since)
		if err != nil {
			t.Fatalf("sentSince: %v", err)
		}
		if sent {
			t.Fatal("expected not-yet-sent with an empty ledger")
		}
	})

	t.Run("prior success this period suppresses", func(t *testing.T) {
		reset()
		insertRun(t, ctx, pool, job, "success", now.Add(-time.Hour))
		sent, err := d.sentSince(ctx, job, since)
		if err != nil {
			t.Fatalf("sentSince: %v", err)
		}
		if !sent {
			t.Fatal("a successful send earlier today must suppress re-send")
		}
	})

	t.Run("crash window: stuck running this period suppresses", func(t *testing.T) {
		reset()
		// A replica sent every email then died before ledgerFinish wrote
		// 'success'. The row is stuck 'running'. The next tick must NOT re-send.
		insertRun(t, ctx, pool, job, "running", now.Add(-30*time.Minute))
		sent, err := d.sentSince(ctx, job, since)
		if err != nil {
			t.Fatalf("sentSince: %v", err)
		}
		if !sent {
			t.Fatal("an in-period stuck 'running' run must suppress re-send (double-delivery guard)")
		}
	})

	t.Run("prior failure this period does not suppress", func(t *testing.T) {
		reset()
		insertRun(t, ctx, pool, job, "failure", now.Add(-30*time.Minute))
		sent, err := d.sentSince(ctx, job, since)
		if err != nil {
			t.Fatalf("sentSince: %v", err)
		}
		if sent {
			t.Fatal("a failed run must NOT suppress the next attempt (retry)")
		}
	})

	t.Run("prior success in a previous period does not suppress", func(t *testing.T) {
		reset()
		insertRun(t, ctx, pool, job, "success", since.Add(-time.Hour)) // yesterday
		sent, err := d.sentSince(ctx, job, since)
		if err != nil {
			t.Fatalf("sentSince: %v", err)
		}
		if sent {
			t.Fatal("yesterday's send must not suppress today's digest")
		}
	})

	t.Run("self-exclusion: own running row does not suppress this tick", func(t *testing.T) {
		reset()
		// scheduler.execute inserts THIS tick's 'running' row before the body
		// runs and threads its id on ctx. The guard must exclude that row, or the
		// job would find the row it just wrote and skip itself forever.
		selfID := insertRun(t, ctx, pool, job, "running", now)
		selfCtx := withRunID(ctx, selfID)
		sent, err := d.sentSince(selfCtx, job, since)
		if err != nil {
			t.Fatalf("sentSince: %v", err)
		}
		if sent {
			t.Fatal("the tick's own 'running' row must not suppress itself")
		}
		// But a SECOND, earlier 'running' row (a prior crashed tick) still
		// suppresses even while this tick's own row is excluded.
		insertRun(t, ctx, pool, job, "running", now.Add(-time.Hour))
		sent, err = d.sentSince(selfCtx, job, since)
		if err != nil {
			t.Fatalf("sentSince: %v", err)
		}
		if !sent {
			t.Fatal("a prior crashed 'running' run must still suppress, excluding only self")
		}
	})

	t.Run("per-job isolation", func(t *testing.T) {
		reset()
		insertRun(t, ctx, pool, "report_monthly_pnl", "success", now.Add(-time.Hour))
		sent, err := d.sentSince(ctx, job, since)
		if err != nil {
			t.Fatalf("sentSince: %v", err)
		}
		if sent {
			t.Fatal("another job's success must not suppress this job")
		}
	})
}
