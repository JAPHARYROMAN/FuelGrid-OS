package secretcrypto

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c := New("a-strong-app-pepper")
	const secret = "JBSWY3DPEHPK3PXP" // a base32 TOTP seed

	enc, err := c.Encrypt(secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if enc == secret {
		t.Fatal("ciphertext equals plaintext — not encrypted")
	}
	if got := enc[:3]; got != "v1:" {
		t.Fatalf("ciphertext missing version prefix: %q", got)
	}

	got, err := c.Decrypt(enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != secret {
		t.Fatalf("round-trip = %q, want %q", got, secret)
	}
}

func TestEncryptIsNondeterministic(t *testing.T) {
	c := New("pepper")
	a, _ := c.Encrypt("same")
	b, _ := c.Encrypt("same")
	if a == b {
		t.Fatal("two encryptions of the same plaintext are identical — nonce not random")
	}
}

func TestDecryptLegacyPlaintextPassesThrough(t *testing.T) {
	c := New("pepper")
	// A value with no "v1:" prefix predates encryption and must be returned
	// unchanged so existing enrollments keep verifying.
	const legacy = "JBSWY3DPEHPK3PXP"
	got, err := c.Decrypt(legacy)
	if err != nil {
		t.Fatalf("decrypt legacy: %v", err)
	}
	if got != legacy {
		t.Fatalf("legacy passthrough = %q, want %q", got, legacy)
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	enc, err := New("pepper-one").Encrypt("secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := New("pepper-two").Decrypt(enc); err == nil {
		t.Fatal("decrypt with a different key must fail authentication, got nil error")
	}
}

func TestDecryptTamperedFails(t *testing.T) {
	c := New("pepper")
	enc, err := c.Encrypt("secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Decode the body, flip a whole byte of the sealed bytes (nonce || ct ||
	// tag), and re-encode. Flipping a decoded byte is unambiguous — unlike
	// flipping a trailing base64 char, whose low bits can be unused padding.
	body := strings.TrimPrefix(enc, "v1:")
	raw, err := base64.RawStdEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw[len(raw)-1] ^= 0xFF // corrupt the last byte of the GCM tag
	tampered := "v1:" + base64.RawStdEncoding.EncodeToString(raw)
	if _, err := c.Decrypt(tampered); err == nil {
		t.Fatal("decrypt of tampered ciphertext must fail, got nil error")
	}
}

func TestDeterministicKeyAcrossInstances(t *testing.T) {
	// Same secret -> same key, so a value encrypted by one process decrypts in
	// another (e.g. across restarts).
	enc, err := New("shared").Encrypt("v")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got, err := New("shared").Decrypt(enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != "v" {
		t.Fatalf("cross-instance round-trip = %q, want %q", got, "v")
	}
}
