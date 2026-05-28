// Package repo holds the Postgres data access for the identity domain.
// Repository methods take a tenant_id-scoped query as the first argument
// (or rely on the actor on the context) so cross-tenant access is
// impossible by construction.
package repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// User is the row shape consumed by the identity service. Auth-related
// columns are nullable because users can be invited before they pick a
// password.
type User struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	Email             string
	FullName          string
	Status            string
	PasswordHash      *string
	PasswordChangedAt *time.Time
	MfaSecret         *string
	MfaEnabled        bool
	LastLoginAt       *time.Time
	FailedLoginCount  int
	LockedUntil       *time.Time
}

// IsActive returns true when the user can authenticate. Doesn't speak to
// MFA or lockout — those are checked separately at login time.
func (u User) IsActive() bool { return u.Status == "active" }

// IsLocked reports whether the user is currently locked out.
func (u User) IsLocked(now time.Time) bool {
	return u.LockedUntil != nil && now.Before(*u.LockedUntil)
}

// UserRepo is the Postgres-backed user repository.
type UserRepo struct {
	pool *database.Pool
}

// NewUserRepo wires a UserRepo against the supplied pool.
func NewUserRepo(pool *database.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

const userColumns = `
    u.id, u.tenant_id, u.email, u.full_name, u.status,
    u.password_hash, u.password_changed_at,
    u.mfa_secret, u.mfa_enabled,
    u.last_login_at, u.failed_login_count, u.locked_until
`

func scanUser(row pgx.Row, u *User) error {
	return row.Scan(
		&u.ID, &u.TenantID, &u.Email, &u.FullName, &u.Status,
		&u.PasswordHash, &u.PasswordChangedAt,
		&u.MfaSecret, &u.MfaEnabled,
		&u.LastLoginAt, &u.FailedLoginCount, &u.LockedUntil,
	)
}

// FindForLogin looks up a user by tenant slug + email. Returns
// pgx.ErrNoRows if either the tenant or the user is missing.
func (r *UserRepo) FindForLogin(ctx context.Context, tenantSlug, email string) (*User, error) {
	q := `SELECT ` + userColumns + `
	      FROM users u
	      JOIN tenants t ON t.id = u.tenant_id
	      WHERE t.slug = $1
	        AND lower(u.email) = lower($2)
	        AND u.status <> 'deleted'
	        AND t.status <> 'deleted'`
	var u User
	if err := scanUser(r.pool.QueryRow(ctx, q, tenantSlug, email), &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByID returns a user by their UUID, scoped to a tenant for safety.
func (r *UserRepo) FindByID(ctx context.Context, tenantID, userID uuid.UUID) (*User, error) {
	q := `SELECT ` + userColumns + `
	      FROM users u
	      WHERE u.id = $1 AND u.tenant_id = $2 AND u.status <> 'deleted'`
	var u User
	if err := scanUser(r.pool.QueryRow(ctx, q, userID, tenantID), &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByEmail returns a user by email within a tenant. Used by the
// password reset flow before any login has happened.
func (r *UserRepo) FindByEmail(ctx context.Context, tenantSlug, email string) (*User, error) {
	return r.FindForLogin(ctx, tenantSlug, email)
}

// SetPassword updates the password hash and resets failed login counters.
// Takes a Querier so the identity service can run it inside the same
// transaction as its audit + outbox writes (pass the pool for stand-alone
// use).
func (r *UserRepo) SetPassword(ctx context.Context, q database.Querier, userID uuid.UUID, hash string) error {
	_, err := q.Exec(ctx, `
		UPDATE users
		SET password_hash = $1,
		    password_changed_at = now(),
		    failed_login_count = 0,
		    locked_until = NULL,
		    status = CASE WHEN status = 'invited' THEN 'active' ELSE status END
		WHERE id = $2
	`, hash, userID)
	return err
}

// MarkLoginSuccess clears failure counters and stamps last_login_at.
func (r *UserRepo) MarkLoginSuccess(ctx context.Context, q database.Querier, userID uuid.UUID) error {
	_, err := q.Exec(ctx, `
		UPDATE users
		SET last_login_at = now(),
		    failed_login_count = 0,
		    locked_until = NULL
		WHERE id = $1
	`, userID)
	return err
}

// MarkLoginFailure increments the failure counter and, after a threshold,
// sets locked_until. Returns the new failure count.
func (r *UserRepo) MarkLoginFailure(ctx context.Context, q database.Querier, userID uuid.UUID, lockAfter int, lockFor time.Duration) (int, error) {
	query := `
		UPDATE users
		SET failed_login_count = failed_login_count + 1,
		    locked_until = CASE
		        WHEN failed_login_count + 1 >= $2 THEN now() + $3::interval
		        ELSE locked_until
		    END
		WHERE id = $1
		RETURNING failed_login_count
	`
	var count int
	if err := q.QueryRow(ctx, query, userID, lockAfter, lockFor).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// EnrollMfa stores the secret without enabling MFA. Calling VerifyMfa
// with a valid code flips mfa_enabled to true.
func (r *UserRepo) EnrollMfa(ctx context.Context, q database.Querier, userID uuid.UUID, secret string) error {
	_, err := q.Exec(ctx, `
		UPDATE users
		SET mfa_secret = $1,
		    mfa_enabled = false
		WHERE id = $2
	`, secret, userID)
	return err
}

// ActivateMfa flips mfa_enabled to true. Caller must have already verified
// a valid code against the stored secret.
func (r *UserRepo) ActivateMfa(ctx context.Context, q database.Querier, userID uuid.UUID) error {
	_, err := q.Exec(ctx,
		`UPDATE users SET mfa_enabled = true WHERE id = $1`, userID)
	return err
}

// IsNotFound is a small convenience for callers translating to sentinel errors.
func IsNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// TenantOf returns the tenant a user belongs to. Used by flows that
// resolve a user by id alone (e.g. password reset, where the only handle
// is a token-derived user id) but still need the tenant for audit rows.
func (r *UserRepo) TenantOf(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	var tenantID uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT tenant_id FROM users WHERE id = $1`, userID,
	).Scan(&tenantID)
	return tenantID, err
}

// -----------------------------------------------------------------------
// Admin queries (Stage 9). These power /api/v1/users and friends.
// -----------------------------------------------------------------------

// Summary is the small projection returned by the list endpoint.
type Summary struct {
	ID          uuid.UUID
	Email       string
	FullName    string
	Status      string
	MfaEnabled  bool
	LastLoginAt *time.Time
	CreatedAt   time.Time
}

// List returns every non-deleted user in the tenant, newest first.
func (r *UserRepo) List(ctx context.Context, tenantID uuid.UUID) ([]Summary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, email, full_name, status, mfa_enabled, last_login_at, created_at
		FROM users
		WHERE tenant_id = $1 AND status <> 'deleted'
		ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Summary
	for rows.Next() {
		var s Summary
		if err := rows.Scan(
			&s.ID, &s.Email, &s.FullName, &s.Status, &s.MfaEnabled, &s.LastLoginAt, &s.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Invite creates a user with status='invited' and no password. The
// password is set later when the user accepts via the password-reset
// flow.
func (r *UserRepo) Invite(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, email, fullName string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO users (tenant_id, email, full_name, status)
		VALUES ($1, $2, $3, 'invited')
		RETURNING id
	`, tenantID, email, fullName).Scan(&id)
	return id, err
}

// UpdateStatus flips active/suspended; deleted is reached via soft-delete.
func (r *UserRepo) UpdateStatus(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, status string) error {
	tag, err := tx.Exec(ctx, `
		UPDATE users SET status = $3
		WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
	`, id, tenantID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// GrantStationAccess gives the user explicit access to a station. The
// presence of any user_station_access row downgrades the user from
// tenant-wide to station-scoped — see docs/multi-tenancy.md.
func (r *UserRepo) GrantStationAccess(ctx context.Context, tx pgx.Tx, userID, stationID, tenantID, grantedBy uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO user_station_access (user_id, station_id, tenant_id, granted_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, station_id) DO NOTHING
	`, userID, stationID, tenantID, grantedBy)
	return err
}

// RevokeStationAccess removes a single station from the user's scope.
func (r *UserRepo) RevokeStationAccess(ctx context.Context, tx pgx.Tx, userID, stationID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		DELETE FROM user_station_access WHERE user_id = $1 AND station_id = $2
	`, userID, stationID)
	return err
}

// ListStationAccess returns the station IDs in the user's scope (empty
// list means tenant-wide).
func (r *UserRepo) ListStationAccess(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT station_id FROM user_station_access WHERE user_id = $1 ORDER BY granted_at
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var sid uuid.UUID
		if err := rows.Scan(&sid); err != nil {
			return nil, err
		}
		out = append(out, sid)
	}
	return out, rows.Err()
}

// ListRoles returns the role codes a user holds, for the admin UI.
func (r *UserRepo) ListRoles(ctx context.Context, userID uuid.UUID) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT ro.code
		FROM user_roles ur
		JOIN roles ro ON ro.id = ur.role_id
		WHERE ur.user_id = $1
		ORDER BY ro.code
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, rows.Err()
}

// RevokeRole removes a single role grant.
func (r *UserRepo) RevokeRole(ctx context.Context, tx pgx.Tx, userID, roleID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		DELETE FROM user_roles WHERE user_id = $1 AND role_id = $2
	`, userID, roleID)
	return err
}

// RoleIDByCode resolves a system role to its uuid for grant/revoke ops.
func (r *UserRepo) RoleIDByCode(ctx context.Context, code string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM roles WHERE code = $1 AND is_system = true`,
		code,
	).Scan(&id)
	return id, err
}
