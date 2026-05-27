// Package events is the FuelGrid OS domain-event plumbing: an envelope
// type that matches the architecture doc §13.2, a transactional outbox
// writer for emitting events from business transactions, an in-process
// bus for dispatching to handlers, and a background publisher that
// drains the outbox table.
//
// The shape is intentionally provider-agnostic: today the bus runs
// in-process and the publisher dispatches via channels. When Kafka or
// NATS arrives, only the Bus interface implementation changes; the
// callers continue to write into `outbox_events` in their normal
// business transaction.
package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Event is the envelope every domain event uses. Fields mirror the
// architecture doc §13.2 exactly so the on-the-wire shape never
// diverges from the database row layout.
//
// TenantID and ActorID are pointers because platform-level events
// (system bootstrap, cron jobs) legitimately have no tenant or actor.
type Event struct {
	ID            uuid.UUID       `json:"id"`
	TenantID      *uuid.UUID      `json:"tenant_id,omitempty"`
	Type          string          `json:"event_type"`
	Version       int             `json:"event_version"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	ActorID       *uuid.UUID      `json:"actor_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	OccurredAt    time.Time       `json:"occurred_at"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	CausationID   string          `json:"causation_id,omitempty"`
}
