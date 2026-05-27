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

// Insert records a freshly issued session and returns the generated row id.
func (r *SessionRepo) Insert(
	ctx context.Context,
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
	err := r.pool.QueryRow(ctx, `
		INSERT INTO sessions (token_hash, user_id, tenant_id, device_id,
		                     ip, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, tokenHash, userID, tenantID, deviceID, ipArg, userAgent, expiresAt).Scan(&id)
	return id, err
}

// Revoke marks the session row as revoked. The Redis key is deleted
// separately by the caller.
func (r *SessionRepo) Revoke(ctx context.Context, id uuid.UUID, reason string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE sessions
		SET revoked_at = now(), revoke_reason = $2
		WHERE id = $1 AND revoked_at IS NULL
	`, id, reason)
	return err
}

// RevokeAllForUser is used by "log out of all devices" flows.
func (r *SessionRepo) RevokeAllForUser(ctx context.Context, userID uuid.UUID, reason string) error {
	_, err := r.pool.Exec(ctx, `
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
