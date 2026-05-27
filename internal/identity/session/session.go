// Package session manages opaque bearer tokens that authenticate API
// requests after a successful login.
//
// Tokens are 32 random bytes encoded as URL-safe base64 ("raw" form,
// no padding). The raw token only exists client-side and (briefly) as
// a Redis key. The token's sha256 hash is what gets persisted in the
// sessions table for audit. This means a Postgres compromise does not
// yield active sessions.
package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/google/uuid"
)

// Session is the in-memory representation of an active session. The raw
// token is only set on the result of Issue — load operations return the
// session without the raw token.
type Session struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	TenantID   uuid.UUID
	DeviceID   *uuid.UUID
	IP         string
	UserAgent  string
	IssuedAt   time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	// MfaSatisfied flags sessions that completed MFA during login.
	MfaSatisfied bool

	// RawToken is populated only on Issue. It is the value the client
	// should send back in subsequent requests as a bearer token.
	RawToken string `json:"-"`
}

// NewToken generates a fresh raw session token suitable for use as the
// Redis key and Authorization header value.
func NewToken() (raw string, hash []byte, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	hash = hashToken(raw)
	return raw, hash, nil
}

// HashToken returns the sha256 of a raw token, used to look it up in the
// durable sessions table. Returns the same bytes as NewToken for the same
// input.
func HashToken(raw string) []byte { return hashToken(raw) }

func hashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
