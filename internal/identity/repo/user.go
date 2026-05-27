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
func (r *UserRepo) SetPassword(ctx context.Context, userID uuid.UUID, hash string) error {
	_, err := r.pool.Exec(ctx, `
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
func (r *UserRepo) MarkLoginSuccess(ctx context.Context, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
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
func (r *UserRepo) MarkLoginFailure(ctx context.Context, userID uuid.UUID, lockAfter int, lockFor time.Duration) (int, error) {
	q := `
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
	if err := r.pool.QueryRow(ctx, q, userID, lockAfter, lockFor).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// EnrollMfa stores the secret without enabling MFA. Calling VerifyMfa
// with a valid code flips mfa_enabled to true.
func (r *UserRepo) EnrollMfa(ctx context.Context, userID uuid.UUID, secret string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE users
		SET mfa_secret = $1,
		    mfa_enabled = false
		WHERE id = $2
	`, secret, userID)
	return err
}

// ActivateMfa flips mfa_enabled to true. Caller must have already verified
// a valid code against the stored secret.
func (r *UserRepo) ActivateMfa(ctx context.Context, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE users SET mfa_enabled = true WHERE id = $1`, userID)
	return err
}

// IsNotFound is a small convenience for callers translating to sentinel errors.
func IsNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
