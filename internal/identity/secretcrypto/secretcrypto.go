// Package secretcrypto encrypts small secrets — TOTP/MFA seeds today — at rest
// with AES-256-GCM (an authenticated cipher). The 256-bit key is derived from
// a caller-supplied root secret via HKDF-SHA256 with a fixed info label, so the
// same root secret already used for password peppering yields an *independent*
// key here (HKDF domain separation). The key lives only in process memory from
// an app secret, never in the database, so a database-only compromise cannot
// recover the seeds and mint valid MFA codes (audit AUTH-13).
//
// Stored values are versioned: Encrypt returns "v1:" + base64(nonce || sealed).
// Decrypt treats any value WITHOUT the "v1:" prefix as legacy plaintext and
// returns it unchanged, so secrets written before encryption was introduced
// keep verifying and are transparently upgraded the next time they are
// re-enrolled.
package secretcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/hkdf"
)

const (
	version = "v1"
	keyInfo = "fuelgrid-os/secret-encryption/v1"
	keyLen  = 32 // AES-256
)

// Cipher seals and opens secrets with a derived AES-256-GCM key.
type Cipher struct {
	aead cipher.AEAD
}

// New derives an AES-256-GCM cipher from secret via HKDF-SHA256. An empty
// secret is permitted (development and tests) and yields a deterministic key —
// adequate for round-trip correctness, not for production confidentiality. The
// production root secret (AUTH_PASSWORD_PEPPER) is a non-empty app secret.
func New(secret string) *Cipher {
	key := make([]byte, keyLen)
	kdf := hkdf.New(sha256.New, []byte(secret), nil, []byte(keyInfo))
	if _, err := io.ReadFull(kdf, key); err != nil {
		panic(fmt.Sprintf("secretcrypto: derive key: %v", err)) // HKDF/SHA-256 cannot fail to fill 32 bytes
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(fmt.Sprintf("secretcrypto: aes: %v", err)) // a 32-byte key is always valid
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(fmt.Sprintf("secretcrypto: gcm: %v", err))
	}
	return &Cipher{aead: aead}
}

// Encrypt seals plaintext and returns "v1:" + base64(nonce || ciphertext+tag).
// A fresh random nonce is used per call.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("secretcrypto: nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return version + ":" + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. A value without the "v1:" prefix is returned
// unchanged as legacy plaintext (graceful migration). A value that claims to be
// encrypted but fails authentication returns an error — it is never silently
// trusted.
func (c *Cipher) Decrypt(stored string) (string, error) {
	prefix := version + ":"
	if !strings.HasPrefix(stored, prefix) {
		return stored, nil // legacy plaintext, pre-encryption
	}
	raw, err := base64.RawStdEncoding.DecodeString(stored[len(prefix):])
	if err != nil {
		return "", fmt.Errorf("secretcrypto: decode: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("secretcrypto: ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	plain, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("secretcrypto: open: %w", err)
	}
	return string(plain), nil
}
