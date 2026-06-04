// Package retention is the data lifecycle / governance core (Feature 13.2): a
// tenant's retention policies per data scope and the maker-checker workflow for
// reopening or relocking a closed accounting period. The retention sweep job
// (registered with internal/scheduler) reads the policies; in this slice it logs
// its intent and the candidate count rather than purging (the audit ledger and
// other sources are append-only/immutable and need their own hardening pass).
package retention

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// Lifecycle / lookup errors. They map to 4xx responses at the handler.
var (
	// ErrPolicyNotFound is returned when a retention-policy id doesn't resolve
	// within the tenant.
	ErrPolicyNotFound = errors.New("retention: policy not found")
	// ErrInvalidScope is returned for a scope outside the allowed set.
	ErrInvalidScope = errors.New("retention: invalid scope")
	// ErrInvalidStatus is returned for a policy status outside the allowed set.
	ErrInvalidStatus = errors.New("retention: invalid status")
	// ErrInvalidDays is returned for a non-positive retention_days.
	ErrInvalidDays = errors.New("retention: retention_days must be positive")
	// ErrScopeExists is returned when a policy already exists for the scope (a
	// tenant has at most one policy per scope).
	ErrScopeExists = errors.New("retention: a policy already exists for this scope")

	// ErrPeriodNotFound is returned when the referenced accounting period doesn't
	// resolve within the tenant.
	ErrPeriodNotFound = errors.New("retention: accounting period not found")
	// ErrPeriodNotClosed is returned when a change request targets a period that
	// is not in a closed/locked state (only closed/locked periods need one).
	ErrPeriodNotClosed = errors.New("retention: period is not closed or locked")
	// ErrChangeRequestNotFound is returned when a change-request id doesn't
	// resolve within the tenant.
	ErrChangeRequestNotFound = errors.New("retention: change request not found")
	// ErrChangeRequestBadState is returned for a transition the request's status
	// doesn't allow (e.g. deciding one that isn't requested).
	ErrChangeRequestBadState = errors.New("retention: change request is not in the required state")
	// ErrChangeRequestSelfDecide is returned when the decider is the requester —
	// separation of duties forbids deciding your own change request.
	ErrChangeRequestSelfDecide = errors.New("retention: decider cannot be the requester")
	// ErrPendingExists is returned when the period already has a pending change
	// request (at most one pending per period).
	ErrPendingExists = errors.New("retention: period already has a pending change request")
	// ErrInvalidChangeType is returned for a change_type outside the allowed set.
	ErrInvalidChangeType = errors.New("retention: invalid change type")
)

// Valid scopes / statuses (mirror the migration CHECK constraints).
var validScopes = map[string]bool{"audit": true, "session": true, "export": true}
var validStatuses = map[string]bool{"active": true, "disabled": true}
var validChangeTypes = map[string]bool{"reopen": true, "relock": true}

// Repo is the retention data access layer over the application pool.
type Repo struct{ pool *database.Pool }

// New constructs the retention repository.
func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

// Policy is one retention policy: keep <Scope> data for RetentionDays days.
type Policy struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Scope         string
	RetentionDays int
	Status        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

const policyColumns = `
    id, tenant_id, scope, retention_days, status, created_at, updated_at
`

func scanPolicy(row pgx.Row, p *Policy) error {
	return row.Scan(&p.ID, &p.TenantID, &p.Scope, &p.RetentionDays, &p.Status, &p.CreatedAt, &p.UpdatedAt)
}

// ListPolicies returns the tenant's retention policies ordered by scope.
func (r *Repo) ListPolicies(ctx context.Context, tenantID uuid.UUID) ([]Policy, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+policyColumns+` FROM retention_policies WHERE tenant_id = $1 ORDER BY scope`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Policy{}
	for rows.Next() {
		var p Policy
		if err := scanPolicy(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPolicy returns one policy by id within the tenant, or ErrPolicyNotFound.
func (r *Repo) GetPolicy(ctx context.Context, tenantID, id uuid.UUID) (*Policy, error) {
	var p Policy
	err := scanPolicy(r.pool.QueryRow(ctx, `SELECT `+policyColumns+` FROM retention_policies WHERE tenant_id = $1 AND id = $2`, tenantID, id), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPolicyNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreatePolicy inserts a new retention policy for the tenant inside the caller's
// tx. The scope must be valid and unique for the tenant; retention_days must be
// positive.
func (r *Repo) CreatePolicy(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, scope string, retentionDays int, status string) (*Policy, error) {
	if !validScopes[scope] {
		return nil, ErrInvalidScope
	}
	if retentionDays <= 0 {
		return nil, ErrInvalidDays
	}
	if status == "" {
		status = "active"
	}
	if !validStatuses[status] {
		return nil, ErrInvalidStatus
	}
	var p Policy
	err := scanPolicy(tx.QueryRow(ctx, `
		INSERT INTO retention_policies (tenant_id, scope, retention_days, status)
		VALUES ($1, $2, $3, $4)
		RETURNING `+policyColumns,
		tenantID, scope, retentionDays, status,
	), &p)
	if isUniqueViolation(err) {
		return nil, ErrScopeExists
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdatePolicy mutates an existing policy's retention_days and/or status inside
// the caller's tx. nil arguments leave a field unchanged. Returns
// ErrPolicyNotFound when the id doesn't resolve within the tenant.
func (r *Repo) UpdatePolicy(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, retentionDays *int, status *string) (*Policy, error) {
	if retentionDays != nil && *retentionDays <= 0 {
		return nil, ErrInvalidDays
	}
	if status != nil && !validStatuses[*status] {
		return nil, ErrInvalidStatus
	}
	var p Policy
	err := scanPolicy(tx.QueryRow(ctx, `
		UPDATE retention_policies
		SET retention_days = COALESCE($3, retention_days),
		    status         = COALESCE($4, status)
		WHERE tenant_id = $1 AND id = $2
		RETURNING `+policyColumns,
		tenantID, id, retentionDays, status,
	), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPolicyNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// DeletePolicy removes a policy by id within the tenant inside the caller's tx.
// Returns ErrPolicyNotFound when the id doesn't resolve.
func (r *Repo) DeletePolicy(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM retention_policies WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPolicyNotFound
	}
	return nil
}

// ChangeRequest is one row of the closed-period change-request maker-checker
// workflow: a request to reopen or relock a closed/locked accounting period.
type ChangeRequest struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	PeriodID     uuid.UUID
	ChangeType   string
	Reason       string
	Status       string
	RequestedBy  uuid.UUID
	DecidedBy    *uuid.UUID
	DecisionNote *string
	RequestedAt  time.Time
	DecidedAt    *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

const changeRequestColumns = `
    id, tenant_id, period_id, change_type, reason, status,
    requested_by, decided_by, decision_note, requested_at, decided_at,
    created_at, updated_at
`

func scanChangeRequest(row pgx.Row, c *ChangeRequest) error {
	return row.Scan(
		&c.ID, &c.TenantID, &c.PeriodID, &c.ChangeType, &c.Reason, &c.Status,
		&c.RequestedBy, &c.DecidedBy, &c.DecisionNote, &c.RequestedAt, &c.DecidedAt,
		&c.CreatedAt, &c.UpdatedAt,
	)
}

// ListChangeRequests returns the tenant's closed-period change requests,
// optionally filtered by status, newest first (id breaks ties for stable
// paging).
func (r *Repo) ListChangeRequests(ctx context.Context, tenantID uuid.UUID, status string, limit, offset int) ([]ChangeRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+changeRequestColumns+` FROM closed_period_change_requests
		WHERE tenant_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY requested_at DESC, id
		LIMIT $3 OFFSET $4
	`, tenantID, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChangeRequest{}
	for rows.Next() {
		var c ChangeRequest
		if err := scanChangeRequest(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetChangeRequest returns one change request by id within the tenant, or
// ErrChangeRequestNotFound.
func (r *Repo) GetChangeRequest(ctx context.Context, tenantID, id uuid.UUID) (*ChangeRequest, error) {
	var c ChangeRequest
	err := scanChangeRequest(r.pool.QueryRow(ctx, `SELECT `+changeRequestColumns+` FROM closed_period_change_requests WHERE tenant_id = $1 AND id = $2`, tenantID, id), &c)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChangeRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// RequestChange records a new closed-period change request in 'requested' state
// inside the caller's tx. The target period must exist and be in a
// closed/locked state (only those need a controlled change). At most one pending
// request per period (partial unique index -> ErrPendingExists).
func (r *Repo) RequestChange(ctx context.Context, tx pgx.Tx, tenantID, periodID, requestedBy uuid.UUID, changeType, reason string) (*ChangeRequest, error) {
	if !validChangeTypes[changeType] {
		return nil, ErrInvalidChangeType
	}
	// The period must exist within the tenant and be closed/locked. Lock the row
	// so the state check and the insert are consistent.
	var status string
	err := tx.QueryRow(ctx, `
		SELECT status FROM accounting_periods
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, periodID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPeriodNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "closed" && status != "locked" {
		return nil, ErrPeriodNotClosed
	}

	var c ChangeRequest
	err = scanChangeRequest(tx.QueryRow(ctx, `
		INSERT INTO closed_period_change_requests (tenant_id, period_id, change_type, reason, requested_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+changeRequestColumns,
		tenantID, periodID, changeType, reason, requestedBy,
	), &c)
	if isUniqueViolation(err) {
		return nil, ErrPendingExists
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ApproveChange moves requested -> approved. Separation of duties: the approver
// must not be the requester. The row is locked so the state + requester check
// and the transition are atomic. Approving authorizes the period transition; it
// does NOT itself transition the period.
func (r *Repo) ApproveChange(ctx context.Context, tx pgx.Tx, tenantID, id, approverID uuid.UUID, note *string) (*ChangeRequest, error) {
	return r.decideChange(ctx, tx, tenantID, id, approverID, "approved", note)
}

// RejectChange moves requested -> rejected. The requester may not reject their
// own request, mirroring approve's separation of duties.
func (r *Repo) RejectChange(ctx context.Context, tx pgx.Tx, tenantID, id, deciderID uuid.UUID, note *string) (*ChangeRequest, error) {
	return r.decideChange(ctx, tx, tenantID, id, deciderID, "rejected", note)
}

// decideChange is the shared approve/reject transition under a row lock with the
// separation-of-duties guard.
func (r *Repo) decideChange(ctx context.Context, tx pgx.Tx, tenantID, id, deciderID uuid.UUID, to string, note *string) (*ChangeRequest, error) {
	var status string
	var requestedBy uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT status, requested_by FROM closed_period_change_requests
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, id).Scan(&status, &requestedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChangeRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "requested" {
		return nil, ErrChangeRequestBadState
	}
	if requestedBy == deciderID {
		return nil, ErrChangeRequestSelfDecide
	}

	var c ChangeRequest
	err = scanChangeRequest(tx.QueryRow(ctx, `
		UPDATE closed_period_change_requests
		SET status = $3, decided_by = $4, decision_note = $5, decided_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'requested'
		RETURNING `+changeRequestColumns,
		tenantID, id, to, deciderID, note,
	), &c)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChangeRequestBadState
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505) — used to map index collisions to domain errors
// without leaking pgconn into the handler.
func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}
