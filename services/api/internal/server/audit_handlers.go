package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// auditLogEntry is the response shape for one audit_logs row.
type auditLogEntry struct {
	ID            uuid.UUID       `json:"id"`
	TenantID      *uuid.UUID      `json:"tenant_id,omitempty"`
	ActorID       *uuid.UUID      `json:"actor_id,omitempty"`
	Action        string          `json:"action"`
	EntityType    string          `json:"entity_type"`
	EntityID      *string         `json:"entity_id,omitempty"`
	PreviousValue json.RawMessage `json:"previous_value,omitempty"`
	NewValue      json.RawMessage `json:"new_value,omitempty"`
	Reason        *string         `json:"reason,omitempty"`
	IP            *string         `json:"ip,omitempty"`
	UserAgent     *string         `json:"user_agent,omitempty"`
	RequestID     *string         `json:"request_id,omitempty"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

// handleListAuditLogs returns audit_logs scoped to the actor's tenant,
// filtered by optional query params, using the standard limit/offset paging
// helper — the auditor UI pages rather than scrolling the full history in one
// shot.
//
// Query params:
//
//	action       string  exact match
//	entity_type  string  exact match
//	entity_id    string  exact match
//	actor_id     uuid    exact match
//	since        RFC3339 inclusive lower bound on occurred_at
//	until        RFC3339 inclusive upper bound
//	limit/offset int     standard page params (see parsePage)
func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}

	q := r.URL.Query()
	var (
		args  = []any{actor.TenantID}
		where = "WHERE tenant_id = $1"
	)

	addArg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}

	if v := q.Get("action"); v != "" {
		where += " AND action = " + addArg(v)
	}
	if v := q.Get("entity_type"); v != "" {
		where += " AND entity_type = " + addArg(v)
	}
	if v := q.Get("entity_id"); v != "" {
		where += " AND entity_id = " + addArg(v)
	}
	if v := q.Get("actor_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid actor_id")
			return
		}
		where += " AND actor_id = " + addArg(id)
	}
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since (expect RFC3339)")
			return
		}
		where += " AND occurred_at >= " + addArg(t)
	}
	if v := q.Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid until (expect RFC3339)")
			return
		}
		where += " AND occurred_at <= " + addArg(t)
	}

	// occurred_at is not unique, so add id as a deterministic tiebreaker for
	// stable paging. Fetch limit+1 to learn whether a further page exists.
	limitArg := addArg(limit + 1)
	offsetArg := addArg(offset)
	query := `
		SELECT id, tenant_id, actor_id, action, entity_type, entity_id,
		       previous_value, new_value, reason,
		       host(ip), user_agent, request_id, occurred_at
		FROM audit_logs
		` + where + `
		ORDER BY occurred_at DESC, id DESC
		LIMIT ` + limitArg + ` OFFSET ` + offsetArg

	rows, err := s.deps.DB.Query(r.Context(), query, args...)
	if err != nil {
		s.logger.Error("list audit logs", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()

	out := make([]auditLogEntry, 0, limit)
	for rows.Next() {
		var e auditLogEntry
		if err := rows.Scan(
			&e.ID, &e.TenantID, &e.ActorID, &e.Action, &e.EntityType, &e.EntityID,
			&e.PreviousValue, &e.NewValue, &e.Reason,
			&e.IP, &e.UserAgent, &e.RequestID, &e.OccurredAt,
		); err != nil {
			s.logger.Error("scan audit log", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		s.logger.Error("audit log rows", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}
