package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// outboxSweepJob requeues outbox events that were dead-lettered (failed_at set
// after exhausting the retry budget) and have been parked at least requeueAfter,
// giving them one more pass through the publisher — the typical case being a
// downstream consumer that was briefly broken and is now healthy. It resets only
// the publisher's progress columns (failed_at -> NULL, attempt_count -> 0),
// which is exactly the surface the 0075 immutability trigger permits, so the
// event's identity/payload stays frozen. It also prunes job_runs rows older than
// jobRunRetention so the visibility ledger doesn't grow unbounded.
//
// requeueAfter <= 0 disables the requeue half (only pruning runs); a sweep that
// requeues immediately would just fight the publisher's retry budget.
func outboxSweepJob(pool *database.Pool, requeueAfter, jobRunRetention time.Duration) JobFunc {
	return func(ctx context.Context) (string, error) {
		var requeued int64
		if requeueAfter > 0 {
			cutoff := time.Now().Add(-requeueAfter)
			tag, err := pool.Exec(ctx, `
				UPDATE outbox_events
				SET failed_at = NULL, attempt_count = 0
				WHERE published_at IS NULL AND failed_at IS NOT NULL AND failed_at <= $1
			`, cutoff)
			if err != nil {
				return "", fmt.Errorf("requeue dead-lettered events: %w", err)
			}
			requeued = tag.RowsAffected()
		}

		var pruned int64
		if jobRunRetention > 0 {
			cutoff := time.Now().Add(-jobRunRetention)
			// Pruning job_runs is best-effort: a deployment without migration
			// 0079 has no such table, so swallow undefined_table.
			tag, err := pool.Exec(ctx,
				`DELETE FROM job_runs WHERE finished_at IS NOT NULL AND finished_at <= $1`, cutoff)
			if err != nil {
				if !isMissingLedger(err) {
					return "", fmt.Errorf("prune job_runs: %w", err)
				}
			} else {
				pruned = tag.RowsAffected()
			}
		}

		return fmt.Sprintf("events_requeued=%d job_runs_pruned=%d", requeued, pruned), nil
	}
}

// sessionCleanupJob prunes the durable sessions audit table of rows that have
// been terminated (expired OR revoked) longer than retention. The Redis store
// is the source of truth for active sessions and already self-expires; the
// Postgres table is the audit trail, so we keep terminated rows for a window
// (retention) and then delete them rather than letting the table grow forever.
// One-time password-reset tokens live only in Redis with a TTL and expire
// themselves, so there is nothing to sweep for them here.
//
// retention <= 0 disables the job entirely (handled at registration), so the
// body always has a positive retention to work with.
func sessionCleanupJob(pool *database.Pool, retention time.Duration) JobFunc {
	return func(ctx context.Context) (string, error) {
		cutoff := time.Now().Add(-retention)
		tag, err := pool.Exec(ctx, `
			DELETE FROM sessions
			WHERE (revoked_at IS NOT NULL AND revoked_at <= $1)
			   OR (revoked_at IS NULL AND expires_at <= $1)
		`, cutoff)
		if err != nil {
			return "", fmt.Errorf("delete stale sessions: %w", err)
		}
		return fmt.Sprintf("sessions_deleted=%d retention=%s", tag.RowsAffected(), retention), nil
	}
}
