package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/retention"
)

// ---- DTOs ----

// retentionPolicyDTO is the wire shape of a retention policy.
type retentionPolicyDTO struct {
	ID            uuid.UUID `json:"id"`
	Scope         string    `json:"scope"`
	RetentionDays int       `json:"retention_days"`
	Status        string    `json:"status"`
	CreatedAt     string    `json:"created_at"`
	UpdatedAt     string    `json:"updated_at"`
}

func toRetentionPolicyDTO(p *retention.Policy) retentionPolicyDTO {
	return retentionPolicyDTO{
		ID: p.ID, Scope: p.Scope, RetentionDays: p.RetentionDays, Status: p.Status,
		CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// changeRequestDTO is the wire shape of a closed-period change request.
type changeRequestDTO struct {
	ID           uuid.UUID  `json:"id"`
	PeriodID     uuid.UUID  `json:"period_id"`
	ChangeType   string     `json:"change_type"`
	Reason       string     `json:"reason"`
	Status       string     `json:"status"`
	RequestedBy  uuid.UUID  `json:"requested_by"`
	DecidedBy    *uuid.UUID `json:"decided_by,omitempty"`
	DecisionNote *string    `json:"decision_note,omitempty"`
	RequestedAt  string     `json:"requested_at"`
	DecidedAt    *string    `json:"decided_at,omitempty"`
}

func toChangeRequestDTO(c *retention.ChangeRequest) changeRequestDTO {
	dto := changeRequestDTO{
		ID: c.ID, PeriodID: c.PeriodID, ChangeType: c.ChangeType, Reason: c.Reason,
		Status: c.Status, RequestedBy: c.RequestedBy, DecidedBy: c.DecidedBy,
		DecisionNote: c.DecisionNote, RequestedAt: c.RequestedAt.UTC().Format(time.RFC3339),
	}
	if c.DecidedAt != nil {
		s := c.DecidedAt.UTC().Format(time.RFC3339)
		dto.DecidedAt = &s
	}
	return dto
}

// retentionPolicyError maps a retention-policy domain error to an HTTP response
// when err is non-nil.
func (s *Server) retentionPolicyError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, retention.ErrPolicyNotFound):
		writeError(w, http.StatusNotFound, "retention policy not found")
	case errors.Is(err, retention.ErrInvalidScope):
		writeError(w, http.StatusBadRequest, "scope must be one of audit, session, export")
	case errors.Is(err, retention.ErrInvalidStatus):
		writeError(w, http.StatusBadRequest, "status must be active or disabled")
	case errors.Is(err, retention.ErrInvalidDays):
		writeError(w, http.StatusBadRequest, "retention_days must be a positive integer")
	case errors.Is(err, retention.ErrScopeExists):
		writeError(w, http.StatusConflict, "a retention policy already exists for this scope")
	default:
		s.logger.Error("retention policy", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// changeRequestError maps a closed-period change-request domain error to an HTTP
// response when err is non-nil.
func (s *Server) changeRequestError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, retention.ErrPeriodNotFound):
		writeError(w, http.StatusNotFound, "accounting period not found")
	case errors.Is(err, retention.ErrChangeRequestNotFound):
		writeError(w, http.StatusNotFound, "change request not found")
	case errors.Is(err, retention.ErrPeriodNotClosed):
		writeError(w, http.StatusUnprocessableEntity, "the period is not closed or locked; a change request is only needed for a closed/locked period")
	case errors.Is(err, retention.ErrInvalidChangeType):
		writeError(w, http.StatusBadRequest, "change_type must be reopen or relock")
	case errors.Is(err, retention.ErrChangeRequestSelfDecide):
		writeError(w, http.StatusForbidden, "separation of duties: you cannot decide a change request you requested")
	case errors.Is(err, retention.ErrPendingExists):
		writeError(w, http.StatusConflict, "this period already has a pending change request")
	case errors.Is(err, retention.ErrChangeRequestBadState):
		writeError(w, http.StatusConflict, "change request is not in the required state for this action")
	default:
		s.logger.Error("closed-period change request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// ---- Retention policies (gated retention.manage) ----

// handleListRetentionPolicies returns the tenant's retention policies.
func (s *Server) handleListRetentionPolicies(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.retention.ListPolicies(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("list retention policies", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]retentionPolicyDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toRetentionPolicyDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

// handleCreateRetentionPolicy creates a new retention policy for a scope.
func (s *Server) handleCreateRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Scope         string `json:"scope"`
		RetentionDays int    `json:"retention_days"`
		Status        string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	var p *retention.Policy
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "retention.policy_changed", EventType: "RetentionPolicyChanged", EntityType: "retention_policy",
		NewValue: map[string]any{"scope": req.Scope, "retention_days": req.RetentionDays, "status": req.Status, "op": "create"},
	}, func(tx pgx.Tx) (string, error) {
		out, cerr := s.retention.CreatePolicy(r.Context(), tx, actor.TenantID, req.Scope, req.RetentionDays, req.Status)
		if cerr != nil {
			s.retentionPolicyError(w, cerr)
			return "", cerr
		}
		p = out
		return out.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, toRetentionPolicyDTO(p))
}

// handleUpdateRetentionPolicy mutates a policy's retention_days and/or status.
func (s *Server) handleUpdateRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid policy id")
		return
	}
	var req struct {
		RetentionDays *int    `json:"retention_days,omitempty"`
		Status        *string `json:"status,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.RetentionDays == nil && req.Status == nil {
		writeError(w, http.StatusBadRequest, "nothing to update: provide retention_days and/or status")
		return
	}

	var p *retention.Policy
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "retention.policy_changed", EventType: "RetentionPolicyChanged", EntityType: "retention_policy", EntityID: id.String(),
		NewValue: map[string]any{"retention_days": req.RetentionDays, "status": req.Status, "op": "update"},
	}, func(tx pgx.Tx) (string, error) {
		out, uerr := s.retention.UpdatePolicy(r.Context(), tx, actor.TenantID, id, req.RetentionDays, req.Status)
		if uerr != nil {
			s.retentionPolicyError(w, uerr)
			return "", uerr
		}
		p = out
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toRetentionPolicyDTO(p))
}

// handleDeleteRetentionPolicy removes a retention policy.
func (s *Server) handleDeleteRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid policy id")
		return
	}
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "retention.policy_changed", EventType: "RetentionPolicyChanged", EntityType: "retention_policy", EntityID: id.String(),
		NewValue: map[string]any{"op": "delete"},
	}, func(tx pgx.Tx) (string, error) {
		derr := s.retention.DeletePolicy(r.Context(), tx, actor.TenantID, id)
		if derr != nil {
			s.retentionPolicyError(w, derr)
			return "", derr
		}
		return id.String(), nil
	})
	if !ok {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListRetentionJobRuns returns the recent runs of the retention sweep job
// from the job_runs ledger (reusing the scheduler read repo), so the governance
// page can show when the sweep last ran and what it found.
func (s *Server) handleListRetentionJobRuns(w http.ResponseWriter, r *http.Request) {
	if _, err := identity.Require(r.Context()); err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if s.jobRuns == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []jobRunDTO{}, "count": 0})
		return
	}
	runs, err := s.jobRuns.RecentRuns(r.Context(), 50)
	if err != nil {
		s.logger.Error("list retention job runs", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]jobRunDTO, 0, len(runs))
	for _, jr := range runs {
		if jr.JobName != "retention_sweep" {
			continue
		}
		out = append(out, toJobRunDTO(jr))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

// ---- Closed-period change requests (gated closed_period.change; maker-checker) ----

// handleListChangeRequests lists the tenant's closed-period change requests,
// optionally filtered by status.
func (s *Server) handleListChangeRequests(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.retention.ListChangeRequests(r.Context(), actor.TenantID, r.URL.Query().Get("status"), limit+1, offset)
	if err != nil {
		s.logger.Error("list change requests", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]changeRequestDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toChangeRequestDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

// handleRequestPeriodChange opens a change request against a closed/locked
// accounting period.
func (s *Server) handleRequestPeriodChange(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	periodID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid period id")
		return
	}
	var req struct {
		ChangeType string `json:"change_type"`
		Reason     string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}

	var c *retention.ChangeRequest
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "closed_period.change_requested", EventType: "ClosedPeriodChangeRequested",
		EntityType: "closed_period_change_request",
		NewValue:   map[string]any{"period_id": periodID, "change_type": req.ChangeType, "reason": strings.TrimSpace(req.Reason)},
	}, func(tx pgx.Tx) (string, error) {
		out, cerr := s.retention.RequestChange(r.Context(), tx, actor.TenantID, periodID, actor.UserID, req.ChangeType, strings.TrimSpace(req.Reason))
		if cerr != nil {
			s.changeRequestError(w, cerr)
			return "", cerr
		}
		c = out
		return out.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, toChangeRequestDTO(c))
}

// handleApprovePeriodChange moves a change request requested -> approved.
// Separation of duties (requester != approver) is enforced in the repo.
func (s *Server) handleApprovePeriodChange(w http.ResponseWriter, r *http.Request) {
	s.decidePeriodChange(w, r, "closed_period.change_approved", "ClosedPeriodChangeApproved",
		func(tx pgx.Tx, actor identity.Actor, id uuid.UUID, note *string) (*retention.ChangeRequest, error) {
			return s.retention.ApproveChange(r.Context(), tx, actor.TenantID, id, actor.UserID, note)
		})
}

// handleRejectPeriodChange moves a change request requested -> rejected.
func (s *Server) handleRejectPeriodChange(w http.ResponseWriter, r *http.Request) {
	s.decidePeriodChange(w, r, "closed_period.change_rejected", "ClosedPeriodChangeRejected",
		func(tx pgx.Tx, actor identity.Actor, id uuid.UUID, note *string) (*retention.ChangeRequest, error) {
			return s.retention.RejectChange(r.Context(), tx, actor.TenantID, id, actor.UserID, note)
		})
}

// decidePeriodChange runs an approve/reject transition inside an audited tx.
func (s *Server) decidePeriodChange(w http.ResponseWriter, r *http.Request, auditAction, eventType string, fn func(tx pgx.Tx, actor identity.Actor, id uuid.UUID, note *string) (*retention.ChangeRequest, error)) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Note *string `json:"note,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // body optional

	var c *retention.ChangeRequest
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: auditAction, EventType: eventType, EntityType: "closed_period_change_request", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		out, derr := fn(tx, actor, id, req.Note)
		if derr != nil {
			s.changeRequestError(w, derr)
			return "", derr
		}
		c = out
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toChangeRequestDTO(c))
}
