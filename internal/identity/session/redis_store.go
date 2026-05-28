package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ErrNotFound signals that a raw token does not resolve to any live
// session in Redis. Callers translate this into a 401.
var ErrNotFound = errors.New("session: not found")

// Store is the hot-path session lookup. Implementations live in Redis
// today; tomorrow we can swap to whatever low-latency KV we deploy.
//
// DeleteByID is what makes Postgres-side revocation authoritative: the
// profile "revoke session" and password-reset "revoke all" paths only
// know a session's UUID, not its raw token. Without a reverse index a
// revoked session would keep resolving from Redis until its TTL expired.
type Store interface {
	Put(ctx context.Context, rawToken string, s *Session) error
	Get(ctx context.Context, rawToken string) (*Session, error)
	Touch(ctx context.Context, rawToken string, ttl time.Duration) error
	Delete(ctx context.Context, rawToken string) error
	DeleteByID(ctx context.Context, sessionID uuid.UUID) error
}

// RedisStore stores each session under two keys:
//
//	session:<rawToken>   → the JSON-serialized Session (hot-path lookup)
//	session:id:<uuid>    → the rawToken (reverse index for revoke-by-id)
//
// Both share the session's TTL, so they expire together.
type RedisStore struct {
	client *redis.Client
	prefix string
}

// NewRedisStore wires a fresh store against the given client. Pass a
// non-empty keyPrefix in multi-tenant deployments that share a Redis db.
func NewRedisStore(client *redis.Client, keyPrefix string) *RedisStore {
	if keyPrefix == "" {
		keyPrefix = "session:"
	}
	return &RedisStore{client: client, prefix: keyPrefix}
}

func (s *RedisStore) tokenKey(rawToken string) string {
	return s.prefix + rawToken
}

func (s *RedisStore) idKey(sessionID uuid.UUID) string {
	return s.prefix + "id:" + sessionID.String()
}

// Put writes the session under its raw token and the reverse-id index.
// TTL is derived from the session's ExpiresAt so Redis cleans up expired
// entries automatically.
func (s *RedisStore) Put(ctx context.Context, rawToken string, sess *Session) error {
	ttl := time.Until(sess.ExpiresAt)
	if ttl <= 0 {
		return fmt.Errorf("session: refused to persist an already-expired session")
	}
	payload, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	pipe := s.client.TxPipeline()
	pipe.Set(ctx, s.tokenKey(rawToken), payload, ttl)
	pipe.Set(ctx, s.idKey(sess.ID), rawToken, ttl)
	_, err = pipe.Exec(ctx)
	return err
}

// Get returns the session for a raw token, or ErrNotFound.
func (s *RedisStore) Get(ctx context.Context, rawToken string) (*Session, error) {
	payload, err := s.client.Get(ctx, s.tokenKey(rawToken)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal(payload, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

// Touch extends a session's TTL on activity. Use sparingly — slithering
// expiry forward on every request can mask legitimately stale sessions.
func (s *RedisStore) Touch(ctx context.Context, rawToken string, ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("session: refused to set non-positive TTL")
	}
	// Resolve the session id so we can extend the reverse index too,
	// keeping the two keys' TTLs aligned.
	sess, err := s.Get(ctx, rawToken)
	if err != nil {
		return err
	}
	pipe := s.client.TxPipeline()
	pipe.Expire(ctx, s.tokenKey(rawToken), ttl)
	pipe.Expire(ctx, s.idKey(sess.ID), ttl)
	_, err = pipe.Exec(ctx)
	return err
}

// Delete removes both keys for a raw token. A missing key is not an error.
func (s *RedisStore) Delete(ctx context.Context, rawToken string) error {
	// Read the session first so we know which id index to clear. If the
	// token is already gone, there's nothing to do.
	sess, err := s.Get(ctx, rawToken)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.client.Del(ctx, s.tokenKey(rawToken), s.idKey(sess.ID)).Err()
}

// DeleteByID clears both keys given only a session UUID — the path the
// profile and password-reset revocations use. A missing index is not an
// error (the session may have already expired).
func (s *RedisStore) DeleteByID(ctx context.Context, sessionID uuid.UUID) error {
	rawToken, err := s.client.Get(ctx, s.idKey(sessionID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.client.Del(ctx, s.tokenKey(rawToken), s.idKey(sessionID)).Err()
}
