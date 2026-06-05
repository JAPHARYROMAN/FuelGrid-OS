package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/enterprise"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// ---- Station groups (Stage 1) ----

func (s *Server) handleCreateStationGroup(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Name string  `json:"name"`
		Kind *string `json:"kind,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	var g *enterprise.StationGroup
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "station_group.created", EventType: "StationGroupCreated", EntityType: "station_group",
	}, func(tx pgx.Tx) (string, error) {
		out, err := s.enterprise.CreateGroup(r.Context(), tx, actor.TenantID, req.Name, req.Kind)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		g = out
		return out.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": g.ID, "name": g.Name, "kind": g.Kind, "status": g.Status})
}

func (s *Server) handleListStationGroups(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.enterprise.ListGroupsPage(r.Context(), actor.TenantID, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, map[string]any{"id": rows[i].ID, "name": rows[i].Name, "kind": rows[i].Kind, "status": rows[i].Status})
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	groupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid group id")
		return
	}
	var req struct {
		StationID uuid.UUID `json:"station_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.StationID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "station_id is required")
		return
	}
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "station_group.membership_changed", EventType: "StationGroupMembershipChanged", EntityType: "station_group",
		EntityID: groupID.String(), NewValue: map[string]any{"station_id": req.StationID},
	}, func(tx pgx.Tx) (string, error) {
		if err := s.enterprise.AddGroupMember(r.Context(), tx, actor.TenantID, groupID, req.StationID); err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown group or station")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		return groupID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group_id": groupID, "station_id": req.StationID})
}

// ---- Delegated scopes (Stage 2) ----

func (s *Server) handleGrantScope(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		UserID    uuid.UUID  `json:"user_id"`
		ScopeType string     `json:"scope_type"`
		ScopeID   *uuid.UUID `json:"scope_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == uuid.Nil || req.ScopeType == "" {
		writeError(w, http.StatusBadRequest, "user_id and scope_type are required")
		return
	}
	var grantID uuid.UUID
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "enterprise_role.assigned", EventType: "EnterpriseScopeChanged", EntityType: "enterprise_scope_grant",
		NewValue: map[string]any{"user_id": req.UserID, "scope_type": req.ScopeType},
	}, func(tx pgx.Tx) (string, error) {
		id, err := s.enterprise.GrantScope(r.Context(), tx, actor.TenantID, req.UserID, req.ScopeType, req.ScopeID)
		if err != nil {
			switch {
			case errors.Is(err, enterprise.ErrScopeTargetNotFound):
				// SR-L4: scope_id does not resolve to a tenant-owned row.
				writeError(w, http.StatusBadRequest, "scope_id is not a valid target for this tenant")
			case isForeignKeyViolation(err):
				writeError(w, http.StatusBadRequest, "unknown user")
			default:
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return "", err
		}
		grantID = id
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": grantID})
}

func (s *Server) handleEffectiveStations(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	stations, tenantWide, err := s.enterprise.EffectiveStations(r.Context(), actor.TenantID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user_id": userID, "tenant_wide": tenantWide, "station_ids": stations})
}

// handleListEnterpriseScopes returns the enterprise scopes the calling user may
// switch between (Feature 13.1). It is a read-only lens over the user's own
// grants — picking a scope narrows the chain view to a subset they already have
// access to and never widens it, since scoped reads still enforce station
// access server-side. Gated enterprise.scope.switch.
func (s *Server) handleListEnterpriseScopes(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	opts, tenantWide, err := s.enterprise.UserScopes(r.Context(), actor.TenantID, actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	items := make([]map[string]any, 0, len(opts))
	for i := range opts {
		items = append(items, map[string]any{
			"scope_type":    opts[i].ScopeType,
			"scope_id":      opts[i].ScopeID,
			"label":         opts[i].Label,
			"station_count": opts[i].StationCount,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenant_wide": tenantWide, "scopes": items})
}

// ---- Approval engine (Stage 3) ----

func (s *Server) handleCreateApprovalPolicy(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		WorkflowType      string  `json:"workflow_type"`
		MinAmount         string  `json:"min_amount,omitempty"`
		RequiredApprovals int     `json:"required_approvals,omitempty"`
		RequiredRole      *string `json:"required_role,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WorkflowType == "" {
		writeError(w, http.StatusBadRequest, "workflow_type is required")
		return
	}
	var policyID uuid.UUID
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "approval_policy.created", EventType: "ApprovalPolicyCreated", EntityType: "approval_policy",
	}, func(tx pgx.Tx) (string, error) {
		id, err := s.enterprise.CreatePolicy(r.Context(), tx, actor.TenantID, req.WorkflowType, req.MinAmount, req.RequiredApprovals, req.RequiredRole)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		policyID = id
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": policyID})
}

// handleSimulateApprovalPolicy answers, without persisting anything, whether a
// given workflow + amount would require approval under the current active
// policies and which policy/required role applies. It reuses the approval
// engine's resolver (enterprise.ResolvePolicy) so the simulation can never
// diverge from what RaiseRequest would actually do.
func (s *Server) handleSimulateApprovalPolicy(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		WorkflowType string `json:"workflow_type"`
		Amount       string `json:"amount,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WorkflowType == "" {
		writeError(w, http.StatusBadRequest, "workflow_type is required")
		return
	}
	res, err := s.enterprise.ResolvePolicy(r.Context(), actor.TenantID, req.WorkflowType, req.Amount)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Approval is required when a policy matched; the engine defaults to a
	// single approval only as a fallback when nothing matches, which we do not
	// treat as "an approval is required by policy".
	writeJSON(w, http.StatusOK, map[string]any{
		"workflow_type":      req.WorkflowType,
		"approval_required":  res.Matched,
		"matched":            res.Matched,
		"required_approvals": res.RequiredApprovals,
		"required_role":      res.RequiredRole,
		"policy_id":          res.PolicyID,
	})
}

// policyMap is the wire shape shared by the edit/status responses.
func policyMap(p *enterprise.Policy) map[string]any {
	return map[string]any{
		"id": p.ID, "workflow_type": p.WorkflowType, "min_amount": p.MinAmount,
		"required_approvals": p.RequiredApprovals, "required_role": p.RequiredRole, "status": p.Status,
	}
}

// handleUpdateApprovalPolicy edits a policy's matching rule (Feature 9.2:
// edit). It does not touch status — enable/disable is a separate endpoint. The
// change is audited as approval.policy_changed with the before/after snapshot.
func (s *Server) handleUpdateApprovalPolicy(w http.ResponseWriter, r *http.Request) {
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
		WorkflowType      string  `json:"workflow_type"`
		MinAmount         string  `json:"min_amount,omitempty"`
		RequiredApprovals int     `json:"required_approvals,omitempty"`
		RequiredRole      *string `json:"required_role,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WorkflowType == "" {
		writeError(w, http.StatusBadRequest, "workflow_type is required")
		return
	}
	// Snapshot the prior state for the audit trail (financial-grade change log).
	before, err := s.enterprise.GetPolicy(r.Context(), actor.TenantID, id)
	if errors.Is(err, enterprise.ErrNotFound) {
		writeError(w, http.StatusNotFound, "approval policy not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var updated *enterprise.Policy
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "approval.policy_changed", EventType: "ApprovalPolicyChanged", EntityType: "approval_policy",
		EntityID: id.String(), PreviousValue: policyMap(before),
	}, func(tx pgx.Tx) (string, error) {
		out, err := s.enterprise.UpdatePolicy(r.Context(), tx, actor.TenantID, id, req.WorkflowType, req.MinAmount, req.RequiredApprovals, req.RequiredRole)
		if errors.Is(err, enterprise.ErrNotFound) {
			writeError(w, http.StatusNotFound, "approval policy not found")
			return "", err
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		updated = out
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, policyMap(updated))
}

// handleSetApprovalPolicyStatus enables ('active') or disables ('archived') a
// policy (Feature 9.2: enable/disable). A disabled policy is ignored by the
// approval engine, so it no longer requires approval. Audited as
// approval.policy_changed with the before/after snapshot.
func (s *Server) handleSetApprovalPolicyStatus(w http.ResponseWriter, r *http.Request) {
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
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Status != "active" && req.Status != "archived") {
		writeError(w, http.StatusBadRequest, "status must be active|archived")
		return
	}
	before, err := s.enterprise.GetPolicy(r.Context(), actor.TenantID, id)
	if errors.Is(err, enterprise.ErrNotFound) {
		writeError(w, http.StatusNotFound, "approval policy not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var updated *enterprise.Policy
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "approval.policy_changed", EventType: "ApprovalPolicyChanged", EntityType: "approval_policy",
		EntityID: id.String(), PreviousValue: policyMap(before), NewValue: map[string]any{"status": req.Status},
	}, func(tx pgx.Tx) (string, error) {
		out, err := s.enterprise.SetPolicyStatus(r.Context(), tx, actor.TenantID, id, req.Status)
		if errors.Is(err, enterprise.ErrNotFound) {
			writeError(w, http.StatusNotFound, "approval policy not found")
			return "", err
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		updated = out
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, policyMap(updated))
}

func (s *Server) handleListApprovalPolicies(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.enterprise.ListPoliciesPage(r.Context(), actor.TenantID, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	writePagedMore(w, http.StatusOK, rows, len(rows), limit, offset, hasMore)
}

func approvalRequestMap(a *enterprise.ApprovalRequest) map[string]any {
	return map[string]any{
		"id": a.ID, "workflow_type": a.WorkflowType, "reference_type": a.ReferenceType, "reference_id": a.ReferenceID,
		"amount": a.Amount, "required_approvals": a.RequiredApprovals, "approvals_count": a.ApprovalsCount,
		"status": a.Status, "requested_by": a.RequestedBy,
	}
}

func (s *Server) handleRaiseApprovalRequest(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		WorkflowType  string     `json:"workflow_type"`
		ReferenceType *string    `json:"reference_type,omitempty"`
		ReferenceID   *uuid.UUID `json:"reference_id,omitempty"`
		Amount        string     `json:"amount,omitempty"`
		StationID     *uuid.UUID `json:"station_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WorkflowType == "" {
		writeError(w, http.StatusBadRequest, "workflow_type is required")
		return
	}
	var ar *enterprise.ApprovalRequest
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "approval.requested", EventType: "ApprovalRequested", EntityType: "approval_request",
	}, func(tx pgx.Tx) (string, error) {
		out, err := s.enterprise.RaiseRequest(r.Context(), tx, actor.TenantID, req.WorkflowType, req.ReferenceType, req.ReferenceID, req.Amount, req.StationID, actor.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		ar = out
		return out.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, approvalRequestMap(ar))
}

func (s *Server) handleListApprovalRequests(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.enterprise.ListRequestsPage(r.Context(), actor.TenantID, r.URL.Query().Get("status"), limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, approvalRequestMap(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

func (s *Server) handleDecideApproval(w http.ResponseWriter, r *http.Request) {
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
		Decision string  `json:"decision"`
		Comment  *string `json:"comment,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Decision != "approve" && req.Decision != "reject") {
		writeError(w, http.StatusBadRequest, "decision must be approve|reject")
		return
	}
	var ar *enterprise.ApprovalRequest
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "approval." + req.Decision, EventType: "ApprovalDecided", EntityType: "approval_request", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		out, err := s.enterprise.Decide(r.Context(), tx, actor.TenantID, id, actor.UserID, req.Decision, req.Comment)
		if errors.Is(err, enterprise.ErrNotFound) {
			writeError(w, http.StatusNotFound, "approval request not found")
			return "", err
		}
		if errors.Is(err, enterprise.ErrBadState) {
			writeError(w, http.StatusConflict, "request is no longer pending")
			return "", err
		}
		if errors.Is(err, enterprise.ErrSelfApproval) {
			writeError(w, http.StatusForbidden, "separation of duties: you cannot decide an approval request you raised")
			return "", err
		}
		if errors.Is(err, enterprise.ErrConflict) {
			writeError(w, http.StatusConflict, "you have already decided this request")
			return "", err
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		ar = out
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, approvalRequestMap(ar))
}
