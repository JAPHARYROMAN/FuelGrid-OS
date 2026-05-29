package events

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

// Handler reacts to a single event. Returning an error causes the bus to
// log the failure and (depending on the bus impl) potentially redeliver.
// The in-process bus today logs and continues — the outbox is the
// durable record, so a missed handler is recoverable by re-running it.
type Handler func(ctx context.Context, e Event) error

// Bus is the abstraction over the event transport. The in-process bus
// covers everything today; a Kafka/NATS implementation can plug in
// without changing event producers.
type Bus interface {
	// Subscribe registers a handler for events with the given type. The
	// special wildcard "*" matches every event type — useful for audit
	// sinks, metrics, and tracing fan-out.
	Subscribe(eventType string, h Handler)
	// Publish dispatches an event to every matching handler. If any handler
	// fails, Publish returns a non-nil (joined) error so a durable caller
	// (the outbox publisher) leaves the event unpublished and retries it.
	// Delivery is therefore at-least-once: handlers must be idempotent.
	Publish(ctx context.Context, e Event) error
}

// InProcessBus dispatches synchronously to registered handlers. Safe for
// concurrent Subscribe / Publish from multiple goroutines.
type InProcessBus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
	logger   *slog.Logger
}

// NewInProcessBus builds a bus. A nil logger falls back to slog.Default().
func NewInProcessBus(logger *slog.Logger) *InProcessBus {
	if logger == nil {
		logger = slog.Default()
	}
	return &InProcessBus{
		handlers: make(map[string][]Handler),
		logger:   logger,
	}
}

// Subscribe registers a handler. Use "*" for the catch-all.
func (b *InProcessBus) Subscribe(eventType string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], h)
}

// Publish runs every matching handler in series under the caller's context.
// Every handler runs even if an earlier one fails; the errors are joined
// and returned so the durable outbox publisher will not mark the event
// published and will retry it on the next tick. Because all handlers re-run
// on retry, handlers must be idempotent.
func (b *InProcessBus) Publish(ctx context.Context, e Event) error {
	b.mu.RLock()
	hs := append([]Handler{}, b.handlers[e.Type]...)
	hs = append(hs, b.handlers["*"]...)
	b.mu.RUnlock()

	var errs []error
	for _, h := range hs {
		if err := h(ctx, e); err != nil {
			b.logger.Error("event handler failed",
				"event_type", e.Type,
				"event_id", e.ID,
				"error", err,
			)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
