package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// WriteOutbox inserts an event into outbox_events as part of the caller's
// open transaction. Missing fields are filled with defaults:
//
//   - ID:         a fresh uuid.New() if zero
//   - Version:    1 if zero
//   - OccurredAt: time.Now() if zero
//   - Payload:    JSON null if nil (preserves NOT NULL on the column)
//
// Type, AggregateType, and AggregateID are required — they're how
// consumers route and correlate.
func WriteOutbox(ctx context.Context, tx pgx.Tx, e Event) error {
	if e.Type == "" {
		return errors.New("events: WriteOutbox requires Type")
	}
	if e.AggregateType == "" {
		return errors.New("events: WriteOutbox requires AggregateType")
	}
	if e.AggregateID == "" {
		return errors.New("events: WriteOutbox requires AggregateID")
	}

	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.Version == 0 {
		e.Version = 1
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}
	if len(e.Payload) == 0 {
		e.Payload = json.RawMessage("null")
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO outbox_events
		    (id, tenant_id, event_type, event_version,
		     aggregate_type, aggregate_id, actor_id,
		     payload, metadata, occurred_at, correlation_id, causation_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		e.ID, e.TenantID, e.Type, e.Version,
		e.AggregateType, e.AggregateID, e.ActorID,
		e.Payload, e.Metadata, e.OccurredAt, e.CorrelationID, e.CausationID,
	)
	if err != nil {
		return fmt.Errorf("events: insert outbox: %w", err)
	}
	return nil
}
