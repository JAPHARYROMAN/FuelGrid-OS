package enterprise

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---- Station groups (Stage 1) ----

type StationGroup struct {
	ID     uuid.UUID
	Name   string
	Kind   *string
	Status string
}

func (r *Repo) CreateGroup(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, name string, kind *string) (*StationGroup, error) {
	var g StationGroup
	if err := tx.QueryRow(ctx, `
		INSERT INTO station_groups (tenant_id, name, kind) VALUES ($1, $2, $3)
		RETURNING id, name, kind, status
	`, tenantID, name, kind).Scan(&g.ID, &g.Name, &g.Kind, &g.Status); err != nil {
		return nil, err
	}
	return &g, nil
}

func (r *Repo) ListGroups(ctx context.Context, tenantID uuid.UUID) ([]StationGroup, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, kind, status FROM station_groups WHERE tenant_id = $1 ORDER BY name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StationGroup{}
	for rows.Next() {
		var g StationGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Kind, &g.Status); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// AddGroupMember assigns a station to a group; the composite FK guarantees the
// station belongs to the tenant.
func (r *Repo) AddGroupMember(ctx context.Context, tx pgx.Tx, tenantID, groupID, stationID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO station_group_memberships (tenant_id, station_group_id, station_id)
		VALUES ($1, $2, $3) ON CONFLICT (tenant_id, station_group_id, station_id) DO NOTHING
	`, tenantID, groupID, stationID)
	return err
}

func (r *Repo) ListGroupMembers(ctx context.Context, tenantID, groupID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `SELECT station_id FROM station_group_memberships WHERE tenant_id = $1 AND station_group_id = $2`, tenantID, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ---- Delegated scopes (Stage 2) ----

func (r *Repo) GrantScope(ctx context.Context, tx pgx.Tx, tenantID, userID uuid.UUID, scopeType string, scopeID *uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO enterprise_scope_grants (tenant_id, user_id, scope_type, scope_id)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, tenantID, userID, scopeType, scopeID).Scan(&id)
	return id, err
}

// EffectiveStations resolves the set of station IDs a user may act on from
// their enterprise scope grants. A tenant or company/region scope expands to
// all matching stations; group scope expands via memberships; station scope is
// itself. Returns (nil, true) when the user has tenant-wide scope.
func (r *Repo) EffectiveStations(ctx context.Context, tenantID, userID uuid.UUID) (stations []uuid.UUID, tenantWide bool, err error) {
	rows, qerr := r.pool.Query(ctx, `SELECT scope_type, scope_id FROM enterprise_scope_grants WHERE tenant_id = $1 AND user_id = $2`, tenantID, userID)
	if qerr != nil {
		return nil, false, qerr
	}
	defer rows.Close()
	type grant struct {
		t  string
		id *uuid.UUID
	}
	var grants []grant
	for rows.Next() {
		var g grant
		if err := rows.Scan(&g.t, &g.id); err != nil {
			return nil, false, err
		}
		if g.t == "tenant" {
			return nil, true, nil
		}
		grants = append(grants, g)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	set := map[uuid.UUID]struct{}{}
	for _, g := range grants {
		if g.id == nil {
			continue
		}
		var q string
		switch g.t {
		case "station":
			set[*g.id] = struct{}{}
			continue
		case "company":
			q = `SELECT id FROM stations WHERE tenant_id = $1 AND company_id = $2`
		case "region":
			q = `SELECT id FROM stations WHERE tenant_id = $1 AND region_id = $2`
		case "group":
			q = `SELECT station_id FROM station_group_memberships WHERE tenant_id = $1 AND station_group_id = $2`
		default:
			continue
		}
		sr, err := r.pool.Query(ctx, q, tenantID, *g.id)
		if err != nil {
			return nil, false, err
		}
		for sr.Next() {
			var id uuid.UUID
			if err := sr.Scan(&id); err != nil {
				sr.Close()
				return nil, false, err
			}
			set[id] = struct{}{}
		}
		sr.Close()
	}
	for id := range set {
		stations = append(stations, id)
	}
	return stations, false, nil
}

// ---- Approval engine (Stage 3) ----

type ApprovalRequest struct {
	ID                uuid.UUID
	WorkflowType      string
	ReferenceType     *string
	ReferenceID       *uuid.UUID
	Amount            string
	RequiredApprovals int
	ApprovalsCount    int
	Status            string
	RequestedBy       uuid.UUID
	CreatedAt         time.Time
}

const approvalReqColumns = `
    id, workflow_type, reference_type, reference_id, amount::text, required_approvals,
    approvals_count, status, requested_by, created_at
`

func scanApprovalReq(row pgx.Row, a *ApprovalRequest) error {
	return row.Scan(&a.ID, &a.WorkflowType, &a.ReferenceType, &a.ReferenceID, &a.Amount,
		&a.RequiredApprovals, &a.ApprovalsCount, &a.Status, &a.RequestedBy, &a.CreatedAt)
}

func (r *Repo) CreatePolicy(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, workflowType string, minAmount string, requiredApprovals int, requiredRole *string) (uuid.UUID, error) {
	if requiredApprovals < 1 {
		requiredApprovals = 1
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO approval_policies (tenant_id, workflow_type, min_amount, required_approvals, required_role)
		VALUES ($1, $2, COALESCE($3::numeric, 0), $4, $5) RETURNING id
	`, tenantID, workflowType, nullableMoney(minAmount), requiredApprovals, requiredRole).Scan(&id)
	return id, err
}

func (r *Repo) ListPolicies(ctx context.Context, tenantID uuid.UUID) ([]map[string]any, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, workflow_type, min_amount::text, required_approvals, required_role, status
		FROM approval_policies WHERE tenant_id = $1 ORDER BY workflow_type
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var wf, minAmt, status string
		var req int
		var role *string
		if err := rows.Scan(&id, &wf, &minAmt, &req, &role, &status); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "workflow_type": wf, "min_amount": minAmt, "required_approvals": req, "required_role": role, "status": status})
	}
	return out, rows.Err()
}

// RaiseRequest creates an approval request, snapshotting the required-approvals
// count from the strictest matching active policy (default 1 when none).
func (r *Repo) RaiseRequest(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, workflowType string, refType *string, refID *uuid.UUID, amount string, stationID *uuid.UUID, requestedBy uuid.UUID) (*ApprovalRequest, error) {
	var required int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(required_approvals), 1) FROM approval_policies
		WHERE tenant_id = $1 AND status = 'active' AND workflow_type = $2 AND min_amount <= COALESCE($3::numeric, 0)
	`, tenantID, workflowType, nullableMoney(amount)).Scan(&required); err != nil {
		return nil, err
	}
	var a ApprovalRequest
	if err := scanApprovalReq(tx.QueryRow(ctx, `
		INSERT INTO approval_requests (tenant_id, workflow_type, reference_type, reference_id, amount, required_approvals, station_id, requested_by)
		VALUES ($1, $2, $3, $4, COALESCE($5::numeric, 0), $6, $7, $8)
		RETURNING `+approvalReqColumns,
		tenantID, workflowType, refType, refID, nullableMoney(amount), required, stationID, requestedBy,
	), &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repo) GetRequest(ctx context.Context, tenantID, id uuid.UUID) (*ApprovalRequest, error) {
	var a ApprovalRequest
	err := scanApprovalReq(r.pool.QueryRow(ctx, `SELECT `+approvalReqColumns+` FROM approval_requests WHERE tenant_id = $1 AND id = $2`, tenantID, id), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repo) ListRequests(ctx context.Context, tenantID uuid.UUID, status string) ([]ApprovalRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+approvalReqColumns+` FROM approval_requests
		WHERE tenant_id = $1 AND ($2 = '' OR status = $2) ORDER BY created_at DESC
	`, tenantID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ApprovalRequest{}
	for rows.Next() {
		var a ApprovalRequest
		if err := scanApprovalReq(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Decide records an approve/reject decision (one per decider) and advances the
// request: a reject rejects it; reaching required_approvals approves it.
func (r *Repo) Decide(ctx context.Context, tx pgx.Tx, tenantID, requestID, deciderID uuid.UUID, decision string, comment *string) (*ApprovalRequest, error) {
	a, err := r.requestForUpdate(ctx, tx, tenantID, requestID)
	if err != nil {
		return nil, err
	}
	if a.Status != "requested" {
		return nil, ErrBadState
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO approval_decisions (tenant_id, approval_request_id, decision, comment, decided_by)
		VALUES ($1, $2, $3, $4, $5)
	`, tenantID, requestID, decision, comment, deciderID); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	var updated ApprovalRequest
	if decision == "reject" {
		err = scanApprovalReq(tx.QueryRow(ctx, `
			UPDATE approval_requests SET status = 'rejected' WHERE tenant_id = $1 AND id = $2
			RETURNING `+approvalReqColumns, tenantID, requestID), &updated)
	} else {
		err = scanApprovalReq(tx.QueryRow(ctx, `
			UPDATE approval_requests SET approvals_count = approvals_count + 1,
			    status = CASE WHEN approvals_count + 1 >= required_approvals THEN 'approved' ELSE 'requested' END
			WHERE tenant_id = $1 AND id = $2
			RETURNING `+approvalReqColumns, tenantID, requestID), &updated)
	}
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (r *Repo) requestForUpdate(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (*ApprovalRequest, error) {
	var a ApprovalRequest
	err := scanApprovalReq(tx.QueryRow(ctx, `SELECT `+approvalReqColumns+` FROM approval_requests WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, id), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
