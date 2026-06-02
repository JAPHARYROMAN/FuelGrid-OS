package totp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrReplay is returned by Guard.Consume when a code has already been used
// for the same user within its acceptance window. A TOTP code is single-use:
// even though it is cryptographically valid for the whole window (period plus
// the skew on either side), accepting it more than once would let an attacker
// who observes a valid code (shoulder-surf, phishing relay, MITM) replay it
// before it naturally expires. Handlers map this to a 401 — from the client's
// perspective the code is no longer valid.
var ErrReplay = errors.New("totp: code already used")

// AcceptanceWindow is how long a consumed-code marker must live. A code is
// validated against the current period plus one period of skew on either side
// (Skew: 1, Period: 30s), so the widest interval over which the same code can
// re-validate is 3 periods = 90s. Holding the marker for this long guarantees
// a replay is caught for as long as the code itself could re-verify, after
// which the marker can expire because the code can no longer authenticate.
const AcceptanceWindow = 90 * time.Second

// ConsumeStore is the set-if-not-exists primitive the Guard needs. The
// production implementation is *redis.Client (SetNX); tests supply an
// in-memory fake. It must be atomic: a concurrent pair of SetNX calls for the
// same key must return ok=true for exactly one of them.
//
// *redis.Client.SetNX has the signature
//
//	SetNX(ctx, key string, value any, ttl time.Duration) *redis.BoolCmd
//
// which this interface mirrors (returning the bool + error directly) so the
// client satisfies it via a thin adapter.
type ConsumeStore interface {
	SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error)
}

// Guard enforces one-time use of TOTP codes on top of the stateless
// cryptographic Verify. It records each accepted (userID, code) pair in the
// ConsumeStore with a TTL covering the acceptance window; a second presentation
// of the same code within that window is rejected with ErrReplay.
type Guard struct {
	store  ConsumeStore
	prefix string
}

// NewGuard wires a Guard against the given store. Pass a non-empty keyPrefix
// in deployments that share the backing store; "" defaults to "totp_used:".
func NewGuard(store ConsumeStore, keyPrefix string) *Guard {
	if keyPrefix == "" {
		keyPrefix = "totp_used:"
	}
	return &Guard{store: store, prefix: keyPrefix}
}

// key derives the consumed-code marker key. The code is hashed (sha256,
// truncated, base64) rather than stored in the clear so a dump of the
// consumed-code namespace never reveals the OTPs themselves. Keying on the
// code (not the matched window index) means the same code is rejected no
// matter which of the accepted skew windows it landed in.
func (g *Guard) key(userID uuid.UUID, code string) string {
	sum := sha256.Sum256([]byte(code))
	return g.prefix + userID.String() + ":" + base64.RawURLEncoding.EncodeToString(sum[:16])
}

// Consume atomically records code as used for userID. It returns nil when the
// code had not been seen (this caller is the single winner and may proceed),
// and ErrReplay when the code was already consumed within its window. A store
// error is surfaced as-is so the caller fails closed rather than authenticating
// without a guarantee of single use.
//
// Consume must be called only after Verify has confirmed the code is
// cryptographically valid: it does not itself check the code against a secret.
func (g *Guard) Consume(ctx context.Context, userID uuid.UUID, code string) error {
	ok, err := g.store.SetNX(ctx, g.key(userID, code), "1", AcceptanceWindow)
	if err != nil {
		return fmt.Errorf("totp: consume: %w", err)
	}
	if !ok {
		return ErrReplay
	}
	return nil
}
