package audit

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/events"
)

// TxRecord captures the per-action fields handlers fill in. The bundle
// helper writes the audit_logs row and the outbox_events row inside
// the caller's open transaction so both either commit or roll back.
type TxRecord struct {
	TenantID      uuid.UUID
	ActorID       uuid.UUID
	Action        string // e.g. "company.created"
	EventType     string // e.g. "CompanyCreated" — outbox-side name
	EntityType    string // e.g. "company"
	EntityID      string // typically the row id
	AggregateType string // outbox-side; usually same as EntityType
	PreviousValue any    // marshalled to jsonb
	NewValue      any    // marshalled to jsonb
	// EventPayload, when non-nil, overrides the outbox event payload (which
	// otherwise mirrors NewValue). Delete-style actions use it to publish a
	// meaningful payload while keeping the audit row's new_value NULL.
	EventPayload any
	Reason       string
	IP           string
	UserAgent    string
	RequestID    string
}

// WriteWithOutbox writes both audit_logs and outbox_events from a single
// TxRecord. Use it for sensitive admin actions; everyday reads obviously
// skip this entirely.
func WriteWithOutbox(ctx context.Context, tx pgx.Tx, r TxRecord) error {
	prev, err := marshalJSON(r.PreviousValue)
	if err != nil {
		return err
	}
	next, err := marshalJSON(r.NewValue)
	if err != nil {
		return err
	}

	if err := Write(ctx, tx, Entry{
		TenantID:      &r.TenantID,
		ActorID:       &r.ActorID,
		Action:        r.Action,
		EntityType:    r.EntityType,
		EntityID:      r.EntityID,
		PreviousValue: prev,
		NewValue:      next,
		Reason:        r.Reason,
		IP:            r.IP,
		UserAgent:     r.UserAgent,
		RequestID:     r.RequestID,
	}); err != nil {
		return err
	}

	aggregateType := r.AggregateType
	if aggregateType == "" {
		aggregateType = r.EntityType
	}

	payload := next
	if r.EventPayload != nil {
		payload, err = marshalJSON(r.EventPayload)
		if err != nil {
			return err
		}
	}

	return events.WriteOutbox(ctx, tx, events.Event{
		TenantID:      &r.TenantID,
		Type:          r.EventType,
		AggregateType: aggregateType,
		AggregateID:   r.EntityID,
		ActorID:       &r.ActorID,
		Payload:       payload,
		CorrelationID: r.RequestID,
	})
}

func marshalJSON(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		return raw, nil
	}
	return json.Marshal(v)
}
