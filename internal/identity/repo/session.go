package repo

import (
	"context"
	"net"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// SessionRepo is the durable side of session storage. The Redis store is
// the source of truth for active session lookups; this table is the audit
// trail (when was the session issued / revoked / on what device).
type SessionRepo struct {
	pool *database.Pool
}

// NewSessionRepo wires a SessionRepo against the supplied pool.
func NewSessionRepo(pool *database.Pool) *SessionRepo {
	return &SessionRepo{pool: pool}
}

// SessionRow is the minimal column set callers care about.
type SessionRow struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TenantID  uuid.UUID
	IssuedAt  time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
	UserAgent string
}

// Insert records a freshly issued session and returns the generated row
// id. Takes a Querier so the login flow can write it in the same
// transaction as the audit + outbox rows.
func (r *SessionRepo) Insert(
	ctx context.Context,
	q database.Querier,
	tokenHash []byte,
	userID, tenantID uuid.UUID,
	deviceID *uuid.UUID,
	ip string,
	userAgent string,
	expiresAt time.Time,
) (uuid.UUID, error) {
	// Pass the IP as a string. Postgres' INET column parses it; sending a
	// nil/empty string maps to NULL. Anything that doesn't ParseIP cleanly
	// is treated as missing rather than letting the DB reject the insert.
	var ipArg any
	if net.ParseIP(ip) != nil {
		ipArg = ip
	}

	var id uuid.UUID
	err := q.QueryRow(ctx, `
		INSERT INTO sessions (token_hash, user_id, tenant_id, device_id,
		                     ip, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, tokenHash, userID, tenantID, deviceID, ipArg, userAgent, expiresAt).Scan(&id)
	return id, err
}

// Revoke marks the session row as revoked. The Redis key is deleted
// separately by the caller.
func (r *SessionRepo) Revoke(ctx context.Context, q database.Querier, id uuid.UUID, reason string) error {
	_, err := q.Exec(ctx, `
		UPDATE sessions
		SET revoked_at = now(), revoke_reason = $2
		WHERE id = $1 AND revoked_at IS NULL
	`, id, reason)
	return err
}

// RevokeAllForUser is used by "log out of all devices" flows.
func (r *SessionRepo) RevokeAllForUser(ctx context.Context, q database.Querier, userID uuid.UUID, reason string) error {
	_, err := q.Exec(ctx, `
		UPDATE sessions
		SET revoked_at = now(), revoke_reason = $2
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID, reason)
	return err
}

// TouchExpiry extends the durable row's expires_at and last_seen_at.
// Mirrors what RedisStore.Touch does on the hot path.
func (r *SessionRepo) TouchExpiry(ctx context.Context, id uuid.UUID, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE sessions
		SET last_seen_at = now(), expires_at = $2
		WHERE id = $1 AND revoked_at IS NULL
	`, id, expiresAt)
	return err
}

// ListActiveForUser returns every non-revoked, non-expired session for
// the user, ordered by issued_at desc. The /profile UI uses this to
// show a "logged in on these devices" table.
func (r *SessionRepo) ListActiveForUser(ctx context.Context, userID uuid.UUID) ([]SessionRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, tenant_id, issued_at, expires_at, revoked_at,
		       coalesce(user_agent, '')
		FROM sessions
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
		ORDER BY issued_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionRow
	for rows.Next() {
		var s SessionRow
		if err := rows.Scan(
			&s.ID, &s.UserID, &s.TenantID, &s.IssuedAt, &s.ExpiresAt, &s.RevokedAt, &s.UserAgent,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListActiveForUserPage returns a page of every non-revoked, non-expired
// session for the user, ordered by issued_at desc (with id as a stable
// tiebreaker), applying the supplied limit and offset.
func (r *SessionRepo) ListActiveForUserPage(ctx context.Context, userID uuid.UUID, limit, offset int) ([]SessionRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, tenant_id, issued_at, expires_at, revoked_at,
		       coalesce(user_agent, '')
		FROM sessions
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
		ORDER BY issued_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionRow
	for rows.Next() {
		var s SessionRow
		if err := rows.Scan(
			&s.ID, &s.UserID, &s.TenantID, &s.IssuedAt, &s.ExpiresAt, &s.RevokedAt, &s.UserAgent,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// FindActiveOwnedBy returns a session by id only if it belongs to the
// given user and is still live. Used to gate the per-session revoke
// endpoint so a user can only kill their own sessions.
func (r *SessionRepo) FindActiveOwnedBy(ctx context.Context, sessionID, userID uuid.UUID) (*SessionRow, error) {
	var s SessionRow
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, tenant_id, issued_at, expires_at, revoked_at,
		       coalesce(user_agent, '')
		FROM sessions
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL AND expires_at > now()
	`, sessionID, userID).Scan(
		&s.ID, &s.UserID, &s.TenantID, &s.IssuedAt, &s.ExpiresAt, &s.RevokedAt, &s.UserAgent,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}
