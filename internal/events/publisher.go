package events

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// MaxOutboxAttempts is the number of failed dispatch attempts a single
// outbox row is allowed before it is dead-lettered. Once an event's
// attempt_count reaches this value its failed_at is stamped and the
// publisher stops retrying it, so one poison event can't monopolise the
// drain loop. The value is deliberately a const: it's a correctness
// guardrail, not a routine tuning knob.
const MaxOutboxAttempts = 10

// outboxFailure is the decision the publisher records for a row whose
// dispatch failed: the new attempt_count, and whether that count has
// exhausted the retry budget (dead-letter).
type outboxFailure struct {
	id           uuid.UUID
	attemptCount int
	deadLetter   bool
}

// classifyFailure computes the post-failure state for a row given the
// attempt_count it carried into this tick. It's a pure function so the
// retry/dead-letter decision can be unit-tested without a database.
func classifyFailure(id uuid.UUID, priorAttempts int) outboxFailure {
	next := priorAttempts + 1
	return outboxFailure{
		id:           id,
		attemptCount: next,
		deadLetter:   next >= MaxOutboxAttempts,
	}
}

// PublisherConfig captures the tuning knobs for the outbox drain loop.
// Defaults are tuned for development latency, not throughput; production
// scale will likely want a longer interval and larger batches.
type PublisherConfig struct {
	PollInterval time.Duration
	BatchSize    int
}

// SafeDefaults fills missing values with reasonable production-safe ones.
func (c PublisherConfig) SafeDefaults() PublisherConfig {
	if c.PollInterval <= 0 {
		c.PollInterval = 2 * time.Second
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	return c
}

// Publisher drains outbox_events into the supplied Bus on an interval.
// It's safe to run multiple Publisher instances against the same database
// — the polling query uses FOR UPDATE SKIP LOCKED to partition work
// without explicit coordination.
type Publisher struct {
	pool   *database.Pool
	bus    Bus
	cfg    PublisherConfig
	logger *slog.Logger

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// NewPublisher wires the publisher. Start must be called separately.
func NewPublisher(pool *database.Pool, bus Bus, cfg PublisherConfig, logger *slog.Logger) *Publisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Publisher{
		pool:   pool,
		bus:    bus,
		cfg:    cfg.SafeDefaults(),
		logger: logger,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// Start kicks off the polling goroutine. Idempotent.
func (p *Publisher) Start() {
	p.startOnce.Do(func() {
		go p.run()
		p.logger.Info("outbox publisher started",
			"poll_interval", p.cfg.PollInterval,
			"batch_size", p.cfg.BatchSize,
		)
	})
}

// Stop signals the loop to exit and waits for it to drain, up to ctx's
// deadline. Idempotent.
func (p *Publisher) Stop(ctx context.Context) error {
	var stopErr error
	p.stopOnce.Do(func() {
		close(p.stopCh)
		select {
		case <-p.doneCh:
			p.logger.Info("outbox publisher stopped")
		case <-ctx.Done():
			stopErr = ctx.Err()
		}
	})
	return stopErr
}

func (p *Publisher) run() {
	defer close(p.doneCh)

	// One immediate tick so brand-new events don't have to wait a full
	// interval before being picked up — feels much snappier in dev.
	p.processOnce(context.Background())

	t := time.NewTicker(p.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-t.C:
			p.processOnce(context.Background())
		}
	}
}

// processOnce drains up to BatchSize unpublished events. Errors are
// logged but never returned: the loop keeps running so a transient DB
// blip doesn't permanently disable event dispatch.
func (p *Publisher) processOnce(ctx context.Context) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		p.logger.Error("outbox: begin tx", "error", err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Skip rows that have already been dead-lettered (failed_at set): they
	// are unpublished but parked, so they no longer compete for the loop.
	rows, err := tx.Query(ctx, `
		SELECT id, tenant_id, event_type, event_version,
		       aggregate_type, aggregate_id, actor_id,
		       payload, metadata, occurred_at, correlation_id, causation_id,
		       attempt_count
		FROM outbox_events
		WHERE published_at IS NULL AND failed_at IS NULL
		ORDER BY occurred_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`, p.cfg.BatchSize)
	if err != nil {
		p.logger.Error("outbox: query", "error", err)
		return
	}

	var (
		events   []Event
		attempts []int
	)
	for rows.Next() {
		var (
			e       Event
			attempt int
		)
		if err := rows.Scan(
			&e.ID, &e.TenantID, &e.Type, &e.Version,
			&e.AggregateType, &e.AggregateID, &e.ActorID,
			&e.Payload, &e.Metadata, &e.OccurredAt,
			&e.CorrelationID, &e.CausationID,
			&attempt,
		); err != nil {
			p.logger.Error("outbox: scan", "error", err)
			rows.Close()
			return
		}
		events = append(events, e)
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		p.logger.Error("outbox: rows err", "error", err)
		return
	}
	rows.Close()

	if len(events) == 0 {
		// Commit (with no work) so the empty tx doesn't sit and inflate
		// idle-tx counters.
		_ = tx.Commit(ctx)
		return
	}

	// Dispatch outside the row iteration. A successful publish marks the
	// row published; a failed publish bumps attempt_count and, once the
	// retry budget is exhausted, stamps failed_at (dead-letter) so the row
	// stops being retried. The outbox is the durable record; the bus is
	// best-effort.
	dispatched := make([]uuid.UUID, 0, len(events))
	failures := make([]outboxFailure, 0)
	for i := range events {
		if err := p.bus.Publish(ctx, events[i]); err != nil {
			f := classifyFailure(events[i].ID, attempts[i])
			failures = append(failures, f)
			if f.deadLetter {
				p.logger.Error("outbox: publish failed; dead-lettering",
					"event_id", events[i].ID,
					"event_type", events[i].Type,
					"attempt_count", f.attemptCount,
					"max_attempts", MaxOutboxAttempts,
					"error", err,
				)
			} else {
				p.logger.Warn("outbox: publish failed; will retry",
					"event_id", events[i].ID,
					"event_type", events[i].Type,
					"attempt_count", f.attemptCount,
					"error", err,
				)
			}
			continue
		}
		dispatched = append(dispatched, events[i].ID)
	}

	if len(dispatched) == 0 && len(failures) == 0 {
		return
	}

	if len(dispatched) > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE outbox_events SET published_at = now() WHERE id = ANY($1)`,
			dispatched,
		); err != nil {
			p.logger.Error("outbox: mark published", "error", err)
			return
		}
	}

	// Persist failure bookkeeping: every failed row gets its new
	// attempt_count, and rows that exhausted the budget get failed_at set
	// so the next poll skips them. We update per row because each carries a
	// distinct attempt_count; batches are small (poison events are rare).
	for _, f := range failures {
		var failedAt *time.Time
		if f.deadLetter {
			now := time.Now()
			failedAt = &now
		}
		if _, err := tx.Exec(ctx,
			`UPDATE outbox_events SET attempt_count = $2, failed_at = $3 WHERE id = $1`,
			f.id, f.attemptCount, failedAt,
		); err != nil {
			p.logger.Error("outbox: record failure", "event_id", f.id, "error", err)
			return
		}
	}

	if err := tx.Commit(ctx); err != nil && !errors.Is(err, context.Canceled) {
		p.logger.Error("outbox: commit", "error", err)
		return
	}

	p.logger.Debug("outbox batch processed",
		"published", len(dispatched),
		"failed", len(failures),
	)
}
