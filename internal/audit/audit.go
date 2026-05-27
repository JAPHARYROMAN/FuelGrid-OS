// Package audit is the append-only record of sensitive actions taken in
// FuelGrid OS. Handlers (and services they call) write an Entry inside
// the same DB transaction as the change being audited so the record
// can't be lost in a crash between the change and the log.
//
// Today this package writes Postgres rows. The Stage-4 era logged audit
// events via slog instead; migrating those call sites to write here is
// tracked as ongoing work — the slog calls remain as a fallback signal
// during the transition.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Entry is one audit_logs row's worth of context.
//
// previous_value and new_value are jsonb so callers don't have to invent
// a schema per action; the consumer (auditor UI, exports) treats the
// payload as opaque.
type Entry struct {
	TenantID      *uuid.UUID
	ActorID       *uuid.UUID
	Action        string
	EntityType    string
	EntityID      string
	PreviousValue json.RawMessage
	NewValue      json.RawMessage
	Reason        string
	IP            string
	UserAgent     string
	RequestID     string
}

// Write inserts the entry as part of the caller's open transaction.
// Action and EntityType are required; everything else is optional.
//
// String IPs that don't parse as INET are stored as NULL rather than
// causing the transaction to fail — a malformed audit IP shouldn't
// derail the business action it accompanies.
func Write(ctx context.Context, tx pgx.Tx, e Entry) error {
	if e.Action == "" {
		return errors.New("audit: Action is required")
	}
	if e.EntityType == "" {
		return errors.New("audit: EntityType is required")
	}

	var ipArg any
	if e.IP != "" {
		// Postgres parses the string as INET. We let it; if the value is
		// junk, NULL is preferable to a transaction failure on a
		// secondary concern.
		ipArg = e.IP
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs
		    (tenant_id, actor_id, action, entity_type, entity_id,
		     previous_value, new_value, reason, ip, user_agent, request_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::inet, $10, $11)
	`,
		e.TenantID, e.ActorID, e.Action, e.EntityType, e.EntityID,
		nullJSON(e.PreviousValue), nullJSON(e.NewValue),
		nullString(e.Reason), ipArg, nullString(e.UserAgent), nullString(e.RequestID),
	); err != nil {
		return fmt.Errorf("audit: insert: %w", err)
	}
	return nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullJSON(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
