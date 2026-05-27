// Package password provides argon2id password hashing with an optional
// server-side pepper.
//
// Hashes are encoded in the PHC string format so future parameter changes
// can be detected and rehashed on next successful login. The pepper is
// applied as HMAC-SHA256 of (password || pepper) before argon2id; this
// gives defense-in-depth against an attacker who steals only the database
// and not the application environment.
package password

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params controls the cost of argon2id. Defaults follow OWASP guidance
// for interactive password verification (2024 update).
type Params struct {
	Memory      uint32 // KiB
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultParams is the recommended setting. Verified to take ~50–80 ms on
// typical server hardware; tune up if hardware budget allows.
var DefaultParams = Params{
	Memory:      64 * 1024,
	Iterations:  3,
	Parallelism: 4,
	SaltLength:  16,
	KeyLength:   32,
}

// Hasher hashes and verifies passwords. Construct one per process and
// share — methods are safe for concurrent use.
type Hasher struct {
	params Params
	pepper []byte
}

// New builds a Hasher. pepper may be empty for dev / tests; in production
// it should be a stable secret from the environment.
func New(params Params, pepper string) *Hasher {
	return &Hasher{params: params, pepper: []byte(pepper)}
}

// Hash produces a PHC-formatted argon2id hash:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<saltB64>$<hashB64>
func (h *Hasher) Hash(password string) (string, error) {
	if password == "" {
		return "", errors.New("password: empty input")
	}

	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: read salt: %w", err)
	}

	input := h.peppered(password)
	key := argon2.IDKey(
		input,
		salt,
		h.params.Iterations,
		h.params.Memory,
		h.params.Parallelism,
		h.params.KeyLength,
	)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		h.params.Memory,
		h.params.Iterations,
		h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify returns (match, needsRehash, error). needsRehash is true when
// the stored params are weaker than the current DefaultParams — the
// caller should compute a fresh hash with the supplied password.
func (h *Hasher) Verify(password, encoded string) (match bool, needsRehash bool, err error) {
	params, salt, key, err := decode(encoded)
	if err != nil {
		return false, false, err
	}

	input := h.peppered(password)
	keyLen := safeUint32(len(key))
	candidate := argon2.IDKey(
		input,
		salt,
		params.Iterations,
		params.Memory,
		params.Parallelism,
		keyLen,
	)

	if subtle.ConstantTimeCompare(candidate, key) != 1 {
		return false, false, nil
	}

	rehash := params.Memory < h.params.Memory ||
		params.Iterations < h.params.Iterations ||
		params.Parallelism < h.params.Parallelism ||
		keyLen < h.params.KeyLength

	return true, rehash, nil
}

// safeUint32 converts a non-negative int to uint32. Sizes that come from
// `len()` on a decoded base64 hash or salt are bounded by argon2's own
// inputs (which are already uint32), so this conversion can't overflow in
// practice; the explicit helper exists to make that explicit to readers
// and to lint.
func safeUint32(n int) uint32 {
	if n < 0 {
		return 0
	}
	if n > int(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(n)
}

func (h *Hasher) peppered(password string) []byte {
	if len(h.pepper) == 0 {
		return []byte(password)
	}
	mac := hmac.New(sha256.New, h.pepper)
	mac.Write([]byte(password))
	return mac.Sum(nil)
}

func decode(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Params{}, nil, nil, errors.New("password: invalid PHC string")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return Params{}, nil, nil, fmt.Errorf("password: parse version: %w", err)
	}
	if version != argon2.Version {
		return Params{}, nil, nil, fmt.Errorf("password: unsupported argon2 version %d", version)
	}

	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Iterations, &p.Parallelism); err != nil {
		return Params{}, nil, nil, fmt.Errorf("password: parse params: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, fmt.Errorf("password: decode salt: %w", err)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, fmt.Errorf("password: decode hash: %w", err)
	}
	p.SaltLength = safeUint32(len(salt))
	p.KeyLength = safeUint32(len(key))

	return p, salt, key, nil
}
