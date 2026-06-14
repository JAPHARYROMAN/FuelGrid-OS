package server

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/exportjobs"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// Async export worker (Reports Center Phase 13 — Export Center).
//
// A background drain loop that turns the export_jobs queue into rendered files
// stored durably in Postgres (NO external blob store). It mirrors the scheduler's
// safety model:
//
//   - MULTI-INSTANCE SAFE: before draining, the loop takes a single session-level
//     Postgres advisory lock; only the replica that wins drains this tick, the
//     others skip without blocking. The per-row claim additionally uses FOR UPDATE
//     SKIP LOCKED, so even within one replica two drains never grab the same job.
//   - IDEMPOTENT: Complete/Fail guard on status = 'running', so a redelivered or
//     retried terminal write never double-writes. A job's bytes are stored once.
//   - PERMISSION-AWARE: every job re-checks the requesting actor's permission AT
//     GENERATION (renderExportJob); a revoked actor yields a FAILED/forbidden job,
//     never data.
//   - ERROR-ISOLATED: a panic or error in one job fails that job and continues;
//     the loop never dies. A poison job that keeps failing is capped by maxAttempts.
//
// Lifecycle: startExportWorker is called by Server.Start(); stopExportWorker by
// Server.Shutdown(). A nil DB (thin smoke deployment / a harness that never
// starts) is a no-op.

const (
	// exportWorkerTick is how often the loop wakes to drain the queue. It is short
	// so an enqueued export is picked up promptly (the UI polls for status), while
	// the advisory lock keeps multiple replicas from all draining at once.
	exportWorkerTick = 2 * time.Second
	// exportWorkerBatch caps how many jobs one tick drains, so a backlog is worked
	// down across ticks rather than monopolising a connection.
	exportWorkerBatch = 10
	// exportJobRenderTimeout bounds a single job's render so one slow report can't
	// hold the worker's connection indefinitely.
	exportJobRenderTimeout = 60 * time.Second
)

// startExportWorker launches the drain-loop goroutine. Idempotent-ish: it is
// called once from Start(). A nil DB makes it a no-op so the integration harness
// (which builds the Server against a real DB and DOES call Start) still runs the
// worker, while a thin smoke deployment without a DB does not.
func (s *Server) startExportWorker() {
	if s.deps.DB == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.stopExportWork = cancel
	s.exportWorkerDone = make(chan struct{})

	go func() {
		defer close(s.exportWorkerDone)
		t := time.NewTicker(exportWorkerTick)
		defer t.Stop()
		// Drain once immediately so a job enqueued just before boot is not stuck
		// waiting a full tick.
		s.drainExportQueue(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.drainExportQueue(ctx)
			}
		}
	}()
}

// stopExportWorker cancels the loop and waits for the goroutine to drain. Safe to
// call when the worker was never started (nil guards).
func (s *Server) stopExportWorker() {
	if s.stopExportWork != nil {
		s.stopExportWork()
	}
	if s.exportWorkerDone != nil {
		<-s.exportWorkerDone
	}
}

// exportWorkerLockKey is the session-level advisory-lock key the drain loop
// contends for, derived from a stable name via FNV-1a (same scheme as the
// scheduler's per-job keys). Only the replica holding it drains a tick.
func exportWorkerLockKey() int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("fuelgrid.export-worker"))
	return int64(h.Sum64()) //nolint:gosec // bit-reinterpret to a bigint lock key; not a number
}

// drainExportQueue is one tick: contend for the advisory lock and, if won, claim
// and process up to exportWorkerBatch queued jobs. All paths are non-fatal to the
// loop.
func (s *Server) drainExportQueue(ctx context.Context) {
	// Hold the advisory lock on a dedicated connection for the whole tick so a
	// second replica skips; releasing the connection frees the lock.
	conn, err := s.deps.DB.Acquire(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Warn("export worker: acquire connection", "error", err)
		}
		return
	}
	defer conn.Release()

	lockKey := exportWorkerLockKey()
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lockKey).Scan(&locked); err != nil {
		if ctx.Err() == nil {
			s.logger.Warn("export worker: advisory lock", "error", err)
		}
		return
	}
	if !locked {
		// Another replica is draining this tick — expected, not an error.
		return
	}
	defer func() {
		if _, uerr := conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", lockKey); uerr != nil {
			s.logger.Warn("export worker: advisory unlock", "error", uerr)
		}
	}()

	for i := 0; i < exportWorkerBatch; i++ {
		if ctx.Err() != nil {
			return
		}
		job, err := s.exportJobs.ClaimNext(ctx)
		if err != nil {
			s.logger.Error("export worker: claim", "error", err)
			return
		}
		if job == nil {
			return // queue empty
		}
		s.processExportJob(ctx, job)
	}
}

// processExportJob renders one claimed (running) job and stamps the terminal
// status. A render error (including a permission denial) fails the job with a
// reason; success stores the bytes and audits the export. Panics are recovered
// into a failure so a single bad job never takes down the loop.
func (s *Server) processExportJob(ctx context.Context, job *exportjobs.Job) {
	log := s.logger.With("export_job_id", job.ID.String(), "report_key", job.ReportKey, "format", job.Format, "tenant_id", job.TenantID.String())

	// Bound the render so a slow report can't wedge the worker.
	rctx, cancel := context.WithTimeout(ctx, exportJobRenderTimeout)
	defer cancel()

	actor := identity.Actor{UserID: job.RequestedBy, TenantID: job.TenantID}

	data, contentType, filename, checksum, rerr := s.renderWithRecover(rctx, actor, job)
	if rerr != nil {
		reason := classifyExportError(rerr)
		// Use a background context for the terminal write so it lands even if the
		// render ctx was cancelled (shutdown / timeout mid-render).
		fctx, fcancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer fcancel()
		if ferr := s.exportJobs.Fail(fctx, job.TenantID, job.ID, reason); ferr != nil && !errors.Is(ferr, exportjobs.ErrNotFound) {
			log.Error("export worker: mark failed", "error", ferr)
		}
		if errors.Is(rerr, errExportForbidden) {
			log.Warn("export worker: job forbidden at generation", "reason", reason)
		} else {
			log.Error("export worker: render failed", "error", rerr)
		}
		return
	}

	wctx, wcancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer wcancel()
	if cerr := s.exportJobs.Complete(wctx, job.TenantID, job.ID, exportjobs.CompleteInput{
		Bytes: data, ContentType: contentType, Filename: filename, Checksum: checksum,
	}); cerr != nil {
		if errors.Is(cerr, exportjobs.ErrNotFound) {
			// Already terminal (redelivery) — nothing to do.
			return
		}
		log.Error("export worker: mark completed", "error", cerr)
		return
	}

	// Audit the completed export (mirrors the synchronous file handlers'
	// 'report.exported' with a content checksum), so an async export is just as
	// provably recorded. Best-effort: a failed audit does not un-complete the job.
	s.auditAsyncExport(wctx, actor, job, len(data), checksum)
	log.Info("export worker: job completed", "bytes", len(data), "checksum", checksum)
}

// renderWithRecover wraps renderExportJob with panic recovery so a panicking
// renderer fails the job rather than crashing the loop.
func (s *Server) renderWithRecover(
	ctx context.Context, actor identity.Actor, job *exportjobs.Job,
) (data []byte, contentType, filename, checksum string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("export: panic: %v", r)
		}
	}()
	return s.renderExportJob(ctx, actor, job.ReportKey, job.Format, job.Filters)
}

// classifyExportError maps a render error to the human reason stored on the
// failed job. Forbidden/unsupported get clear, stable messages; everything else
// is a generic "render failed" (the detailed error stays in the logs only, so a
// failed job never leaks internal detail to the API surface).
func classifyExportError(err error) string {
	switch {
	case errors.Is(err, errExportForbidden):
		return "forbidden: you are no longer permitted to view this report"
	case errors.Is(err, errExportUnsupported):
		return "unsupported report_key/format combination"
	default:
		return "report could not be generated"
	}
}

// auditAsyncExport records the completed async export in the audit log + outbox,
// carrying the job id, format, byte count and checksum. It runs in its own tx on
// the owner pool. Best-effort: a failure is logged but never un-completes the job.
func (s *Server) auditAsyncExport(ctx context.Context, actor identity.Actor, job *exportjobs.Job, byteCount int, checksum string) {
	newValue := map[string]any{
		"report_type":   job.ReportKey,
		"format":        job.Format,
		"export_job_id": job.ID.String(),
		"byte_count":    byteCount,
		"checksum":      checksum,
		"async":         true,
	}
	for k, v := range job.Filters {
		newValue["filter_"+k] = v
	}
	tx, terr := s.deps.DB.Begin(ctx)
	if terr != nil {
		s.logger.Error("export worker: audit begin", "error", terr, "export_job_id", job.ID.String())
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID:   actor.TenantID,
		ActorID:    actor.UserID,
		Action:     "report.exported",
		EventType:  "ReportExported",
		EntityType: "export_job",
		EntityID:   job.ID.String(),
		NewValue:   newValue,
	}); err != nil {
		s.logger.Error("export worker: audit write", "error", err, "export_job_id", job.ID.String())
		return
	}
	if err := tx.Commit(ctx); err != nil {
		s.logger.Error("export worker: audit commit", "error", err, "export_job_id", job.ID.String())
	}
}
