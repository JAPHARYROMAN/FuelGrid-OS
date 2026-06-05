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

// ListGroupsPage is the paginated variant of ListGroups (REL-REPO). name is not
// unique, so id is appended as a deterministic tiebreaker.
func (r *Repo) ListGroupsPage(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]StationGroup, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, kind, status FROM station_groups WHERE tenant_id = $1 ORDER BY name, id LIMIT $2 OFFSET $3`, tenantID, limit, offset)
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

// ErrScopeTargetNotFound is returned by GrantScope when a non-tenant scope's
// scope_id does not resolve to a row owned by the granting tenant (SR-L4). The
// HTTP layer maps it to a 400 so a caller cannot mint a grant that references
// another tenant's company/region/group/station id.
var ErrScopeTargetNotFound = errors.New("enterprise: scope target not found for tenant")

// scopeTargetTable maps a non-tenant scope_type to the table its scope_id must
// reference. A `tenant` scope carries no scope_id and is not listed here.
var scopeTargetTable = map[string]string{
	"company": "companies",
	"region":  "regions",
	"group":   "station_groups",
	"station": "stations",
}

// GrantScope records a delegated enterprise scope for a user. SR-L4: for any
// scope_type other than `tenant` it first verifies (in the same tenant-scoped
// tx, so RLS bounds the lookup) that scope_id is non-nil and resolves to a row
// of the matching kind owned by the tenant — otherwise it returns
// ErrScopeTargetNotFound rather than persisting a grant that points at another
// tenant's (or a non-existent) entity. This is defensive hardening: enterprise
// scopes drive UI lensing only, and resource authorization is enforced
// separately, but a cross-tenant scope_id should never be storable.
func (r *Repo) GrantScope(ctx context.Context, tx pgx.Tx, tenantID, userID uuid.UUID, scopeType string, scopeID *uuid.UUID) (uuid.UUID, error) {
	if scopeType != "tenant" {
		table, known := scopeTargetTable[scopeType]
		if !known {
			// Unknown scope_type: let the DB CHECK constraint reject it as before.
			table = ""
		}
		if table != "" {
			if scopeID == nil {
				return uuid.Nil, ErrScopeTargetNotFound
			}
			var exists bool
			// #nosec G201 -- table is selected from a fixed in-code allowlist
			// (scopeTargetTable), never from caller input.
			q := "SELECT EXISTS (SELECT 1 FROM " + table + " WHERE tenant_id = $1 AND id = $2)"
			if err := tx.QueryRow(ctx, q, tenantID, *scopeID).Scan(&exists); err != nil {
				return uuid.Nil, err
			}
			if !exists {
				return uuid.Nil, ErrScopeTargetNotFound
			}
		}
	}
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

// ScopeOption is one switchable enterprise scope available to a user — a node
// of the hierarchy (tenant / company / region / group / station) their grants
// resolve to, with a human label and the number of stations it covers. It backs
// the context scope-switcher (Feature 13.1).
type ScopeOption struct {
	ScopeType    string
	ScopeID      *uuid.UUID
	Label        string
	StationCount int
}

// UserScopes enumerates the enterprise scopes a user may switch between, derived
// from their scope grants. A tenant grant yields a single "All stations" option
// (tenantWide=true). Otherwise each grant becomes a labelled option with its
// resolved station count; the labels are looked up from companies / regions /
// station_groups / stations so the switcher shows names, not raw ids. The set is
// deduplicated by (scope_type, scope_id).
//
// This is a read-only lens over the user's OWN grants: it never widens access,
// because scoped reads still enforce station access server-side. It exists so
// the UI can offer the user a way to narrow the chain view to a subset they are
// already entitled to.
func (r *Repo) UserScopes(ctx context.Context, tenantID, userID uuid.UUID) (opts []ScopeOption, tenantWide bool, err error) {
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
	seen := map[string]struct{}{}
	opts = []ScopeOption{}
	for _, g := range grants {
		if g.id == nil {
			continue
		}
		key := g.t + ":" + g.id.String()
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		opt := ScopeOption{ScopeType: g.t, ScopeID: g.id}
		var labelQ, countQ string
		switch g.t {
		case "company":
			labelQ = `SELECT name FROM companies WHERE tenant_id = $1 AND id = $2`
			countQ = `SELECT count(*) FROM stations WHERE tenant_id = $1 AND company_id = $2`
		case "region":
			labelQ = `SELECT name FROM regions WHERE tenant_id = $1 AND id = $2`
			countQ = `SELECT count(*) FROM stations WHERE tenant_id = $1 AND region_id = $2`
		case "group":
			labelQ = `SELECT name FROM station_groups WHERE tenant_id = $1 AND id = $2`
			countQ = `SELECT count(*) FROM station_group_memberships WHERE tenant_id = $1 AND station_group_id = $2`
		case "station":
			labelQ = `SELECT name FROM stations WHERE tenant_id = $1 AND id = $2`
			// A station scope always covers exactly itself.
			countQ = ""
			opt.StationCount = 1
		default:
			continue
		}
		var label *string
		if err := r.pool.QueryRow(ctx, labelQ, tenantID, *g.id).Scan(&label); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Grant points at a deleted node; skip it rather than surface a
				// dangling option.
				continue
			}
			return nil, false, err
		}
		if label != nil {
			opt.Label = *label
		}
		if countQ != "" {
			if err := r.pool.QueryRow(ctx, countQ, tenantID, *g.id).Scan(&opt.StationCount); err != nil {
				return nil, false, err
			}
		}
		opts = append(opts, opt)
	}
	return opts, false, nil
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

// ListPoliciesPage is the paginated variant of ListPolicies (REL-REPO).
// workflow_type is not unique, so id is appended as a deterministic tiebreaker.
func (r *Repo) ListPoliciesPage(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]map[string]any, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, workflow_type, min_amount::text, required_approvals, required_role, status
		FROM approval_policies WHERE tenant_id = $1
		ORDER BY workflow_type, id
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
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

// Policy is a single approval policy row, shaped for the edit/status endpoints
// (Feature 9.2). min_amount is the exact decimal string (::text from numeric),
// never a float.
type Policy struct {
	ID                uuid.UUID
	WorkflowType      string
	MinAmount         string
	RequiredApprovals int
	RequiredRole      *string
	Status            string
}

// GetPolicy reads one approval policy by id within the tenant.
func (r *Repo) GetPolicy(ctx context.Context, tenantID, id uuid.UUID) (*Policy, error) {
	var p Policy
	err := r.pool.QueryRow(ctx, `
		SELECT id, workflow_type, min_amount::text, required_approvals, required_role, status
		FROM approval_policies WHERE tenant_id = $1 AND id = $2
	`, tenantID, id).Scan(&p.ID, &p.WorkflowType, &p.MinAmount, &p.RequiredApprovals, &p.RequiredRole, &p.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdatePolicy edits a policy's matching rule (workflow_type, min_amount,
// required_approvals, required_role) in place. The status is left untouched —
// enable/disable goes through SetPolicyStatus. required_approvals is clamped to
// the table's CHECK (>= 1). Returns the updated row, or ErrNotFound when no
// policy with that id exists in the tenant.
func (r *Repo) UpdatePolicy(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, workflowType string, minAmount string, requiredApprovals int, requiredRole *string) (*Policy, error) {
	if requiredApprovals < 1 {
		requiredApprovals = 1
	}
	var p Policy
	err := tx.QueryRow(ctx, `
		UPDATE approval_policies
		SET workflow_type = $3, min_amount = COALESCE($4::numeric, 0),
		    required_approvals = $5, required_role = $6
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, workflow_type, min_amount::text, required_approvals, required_role, status
	`, tenantID, id, workflowType, nullableMoney(minAmount), requiredApprovals, requiredRole).
		Scan(&p.ID, &p.WorkflowType, &p.MinAmount, &p.RequiredApprovals, &p.RequiredRole, &p.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SetPolicyStatus enables ('active') or disables ('archived') a policy. A
// disabled policy is ignored by resolvePolicy (it filters status = 'active'),
// so a disabled policy no longer requires approval in simulation or when a
// request is raised. status must be one of the table's CHECK values; callers
// validate it. Returns ErrNotFound when no such policy exists in the tenant.
func (r *Repo) SetPolicyStatus(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, status string) (*Policy, error) {
	var p Policy
	err := tx.QueryRow(ctx, `
		UPDATE approval_policies SET status = $3
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, workflow_type, min_amount::text, required_approvals, required_role, status
	`, tenantID, id, status).
		Scan(&p.ID, &p.WorkflowType, &p.MinAmount, &p.RequiredApprovals, &p.RequiredRole, &p.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// PolicyResolution is the outcome of resolving the governance policies for a
// (workflow_type, amount) pair. It is the single source of truth shared by the
// approval engine (RaiseRequest) and the simulation endpoint.
type PolicyResolution struct {
	// Matched is true when at least one active policy applies to the input.
	Matched bool
	// RequiredApprovals is the strictest matching policy's required-approvals
	// count, or 1 (the engine's default) when nothing matched.
	RequiredApprovals int
	// RequiredRole is the role required by the strictest matching policy, if any.
	// It is nil when no policy matched or the matching policy sets no role.
	RequiredRole *string
	// PolicyID is the id of the strictest matching policy, when one matched.
	PolicyID *uuid.UUID
}

// rowQuerier is the read surface shared by *database.Pool and pgx.Tx, letting
// ResolvePolicy run either read-only (simulation) or inside the request's
// transaction (RaiseRequest) without duplicating the resolution rule.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// resolvePolicy resolves the governance policies for a (workflow_type, amount)
// pair using the SAME selection rule the approval engine applies when raising a
// request: among active policies for the workflow whose min_amount is at or
// below the amount, the one demanding the most approvals wins (ties broken by
// the highest min_amount, then id). RaiseRequest and the simulation endpoint
// both call it so the live engine and the simulator never diverge.
func resolvePolicy(ctx context.Context, q rowQuerier, tenantID uuid.UUID, workflowType string, amount string) (PolicyResolution, error) {
	var (
		res  PolicyResolution
		id   uuid.UUID
		req  int
		role *string
	)
	err := q.QueryRow(ctx, `
		SELECT id, required_approvals, required_role FROM approval_policies
		WHERE tenant_id = $1 AND status = 'active' AND workflow_type = $2
		  AND min_amount <= COALESCE($3::numeric, 0)
		ORDER BY required_approvals DESC, min_amount DESC, id
		LIMIT 1
	`, tenantID, workflowType, nullableMoney(amount)).Scan(&id, &req, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		// No policy applies — the engine defaults to a single approval.
		return PolicyResolution{Matched: false, RequiredApprovals: 1}, nil
	}
	if err != nil {
		return PolicyResolution{}, err
	}
	res.Matched = true
	res.RequiredApprovals = req
	res.RequiredRole = role
	res.PolicyID = &id
	return res, nil
}

// ResolvePolicy resolves the governance policies for a (workflow_type, amount)
// pair read-only against the pool. It backs the simulation endpoint.
func (r *Repo) ResolvePolicy(ctx context.Context, tenantID uuid.UUID, workflowType string, amount string) (PolicyResolution, error) {
	return resolvePolicy(ctx, r.pool, tenantID, workflowType, amount)
}

// RaiseRequest creates an approval request, snapshotting the required-approvals
// count from the strictest matching active policy (default 1 when none).
func (r *Repo) RaiseRequest(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, workflowType string, refType *string, refID *uuid.UUID, amount string, stationID *uuid.UUID, requestedBy uuid.UUID) (*ApprovalRequest, error) {
	// Resolve within the request's transaction so it observes the same snapshot.
	resolved, err := resolvePolicy(ctx, tx, tenantID, workflowType, amount)
	if err != nil {
		return nil, err
	}
	required := resolved.RequiredApprovals
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

// ListRequestsPage is the paginated variant of ListRequests (REL-REPO).
// created_at is not unique, so id is appended as a deterministic tiebreaker.
func (r *Repo) ListRequestsPage(ctx context.Context, tenantID uuid.UUID, status string, limit, offset int) ([]ApprovalRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+approvalReqColumns+` FROM approval_requests
		WHERE tenant_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC, id
		LIMIT $3 OFFSET $4
	`, tenantID, status, limit, offset)
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
	// Separation of duties (four-eyes): the requester cannot decide their own
	// approval request — neither approve nor reject it.
	if a.RequestedBy == deciderID {
		return nil, ErrSelfApproval
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
