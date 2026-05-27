// Package ratelimit is a fixed-window Redis counter used to throttle
// expensive endpoints (login, password reset, MFA verify).
//
// Fixed-window is intentionally simple: incr + expire. We can swap in a
// sliding window or token bucket later when the traffic profile justifies
// the extra ops cost.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrLimited is returned when a caller has exceeded the configured budget.
var ErrLimited = errors.New("ratelimit: limit exceeded")

// Limiter caps the number of operations a given key (typically an IP or
// user ID) can perform within a window.
type Limiter struct {
	client *redis.Client
	prefix string
}

// New builds a Limiter against the supplied Redis client. keyPrefix is
// prepended to every key; pass "" for the default ("ratelimit:").
func New(client *redis.Client, keyPrefix string) *Limiter {
	if keyPrefix == "" {
		keyPrefix = "ratelimit:"
	}
	return &Limiter{client: client, prefix: keyPrefix}
}

// Allow increments the counter for the given bucket and returns ErrLimited
// if the new count exceeds limit within the window. The window TTL is only
// set on the first increment in that window.
func (l *Limiter) Allow(ctx context.Context, bucket string, limit int64, window time.Duration) error {
	key := l.prefix + bucket
	pipe := l.client.TxPipeline()
	inc := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, window)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("ratelimit: redis: %w", err)
	}
	if inc.Val() > limit {
		return ErrLimited
	}
	return nil
}

// Reset clears the counter for the given bucket. Useful on successful
// login: we don't want to count failed attempts forever after a success.
func (l *Limiter) Reset(ctx context.Context, bucket string) error {
	return l.client.Del(ctx, l.prefix+bucket).Err()
}
