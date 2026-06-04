package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

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

// retentionSweepJob is the data-lifecycle sweep for the per-tenant retention
// policies (Feature 13.2, migration 0090). For every active 'audit' policy it
// counts the audit_logs rows older than now() - retention_days that WOULD be
// purged and records that intent in the run detail.
//
// IMPORTANT — this slice does NOT purge. The audit_logs ledger is append-only
// and immutable (migration 0070); deleting from it (and from the other scoped
// sources) needs its own hardening pass that relaxes the immutability guard for
// a controlled retention path. Until then this job is intentionally a dry-run:
// it proves the policy is readable and surfaces the candidate count so operators
// can see what a real purge would remove, without touching frozen records. The
// 'session' scope is already pruned by sessionCleanupJob; 'export' purging is
// likewise deferred. A deployment without migration 0090 (no retention_policies
// table) is a safe no-op.
func retentionSweepJob(pool *database.Pool) JobFunc {
	return func(ctx context.Context) (string, error) {
		rows, err := pool.Query(ctx, `
			SELECT tenant_id, scope, retention_days
			FROM retention_policies
			WHERE status = 'active'
			ORDER BY tenant_id, scope
		`)
		if err != nil {
			if isMissingLedger(err) {
				return "policies=0 (retention_policies table absent)", nil
			}
			return "", fmt.Errorf("list retention policies: %w", err)
		}
		type policy struct {
			tenant uuid.UUID
			scope  string
			days   int
		}
		var policies []policy
		for rows.Next() {
			var p policy
			if err := rows.Scan(&p.tenant, &p.scope, &p.days); err != nil {
				rows.Close()
				return "", fmt.Errorf("scan retention policy: %w", err)
			}
			policies = append(policies, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return "", fmt.Errorf("retention policy rows: %w", err)
		}

		var auditPolicies, auditCandidates int64
		for _, p := range policies {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			// Only the 'audit' scope is counted here; 'session' is handled by
			// sessionCleanupJob and 'export' purging is deferred.
			if p.scope != "audit" {
				continue
			}
			auditPolicies++
			// retention_days is a validated positive integer; the cutoff
			// arithmetic runs in SQL so there is no float involved.
			var n int64
			if err := pool.QueryRow(ctx, `
				SELECT count(*) FROM audit_logs
				WHERE tenant_id = $1 AND occurred_at < now() - ($2 || ' days')::interval
			`, p.tenant, p.days).Scan(&n); err != nil {
				return "", fmt.Errorf("count audit purge candidates: %w", err)
			}
			auditCandidates += n
		}

		// Dry-run: report the intent and the candidate count. No rows are deleted
		// (audit_logs is immutable — see the job doc comment).
		return fmt.Sprintf(
			"policies=%d audit_policies=%d audit_purge_candidates=%d purged=0 (dry-run: audit ledger is append-only)",
			len(policies), auditPolicies, auditCandidates,
		), nil
	}
}
