// Package cache provides the Redis client used for sessions, rate limits,
// and short-lived application state. Stage 4 wires session storage on top.
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client is the shared Redis client type. Aliased so callers don't import
// the underlying driver directly.
type Client = redis.Client

// Connect parses the URL, builds the client, and verifies reachability
// with a short timeout.
func Connect(ctx context.Context, url string) (*Client, error) {
	if url == "" {
		return nil, fmt.Errorf("redis url is required")
	}

	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	client := redis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("initial ping: %w", err)
	}

	return client, nil
}
