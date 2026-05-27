package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrNotFound signals that a raw token does not resolve to any live
// session in Redis. Callers translate this into a 401.
var ErrNotFound = errors.New("session: not found")

// Store is the hot-path session lookup. Implementations live in Redis
// today; tomorrow we can swap to whatever low-latency KV we deploy.
type Store interface {
	Put(ctx context.Context, rawToken string, s *Session) error
	Get(ctx context.Context, rawToken string) (*Session, error)
	Touch(ctx context.Context, rawToken string, ttl time.Duration) error
	Delete(ctx context.Context, rawToken string) error
}

// RedisStore stores sessions as JSON blobs under "session:<rawToken>"
// with TTL equal to the session's remaining lifetime.
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

func (s *RedisStore) key(rawToken string) string {
	return s.prefix + rawToken
}

// Put writes the session under its raw token. TTL is derived from the
// session's ExpiresAt so Redis cleans up expired entries automatically.
func (s *RedisStore) Put(ctx context.Context, rawToken string, sess *Session) error {
	ttl := time.Until(sess.ExpiresAt)
	if ttl <= 0 {
		return fmt.Errorf("session: refused to persist an already-expired session")
	}
	payload, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.key(rawToken), payload, ttl).Err()
}

// Get returns the session for a raw token, or ErrNotFound.
func (s *RedisStore) Get(ctx context.Context, rawToken string) (*Session, error) {
	payload, err := s.client.Get(ctx, s.key(rawToken)).Bytes()
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
	ok, err := s.client.Expire(ctx, s.key(rawToken), ttl).Result()
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// Delete revokes a session. A missing key is not an error.
func (s *RedisStore) Delete(ctx context.Context, rawToken string) error {
	return s.client.Del(ctx, s.key(rawToken)).Err()
}
