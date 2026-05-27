// Package totp implements RFC 6238 time-based one-time passwords for
// MFA enrollment and verification. Wraps github.com/pquerna/otp/totp with
// FuelGrid OS defaults (6-digit code, 30s step, SHA1 — per RFC 6238).
package totp

import (
	"fmt"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// Issuer is the label shown in authenticator apps. Override per-tenant
// later if customers want their own branding.
const Issuer = "FuelGrid OS"

// Enrollment is the bundle returned to clients enrolling an authenticator.
type Enrollment struct {
	Secret     string // base32 secret to store in users.mfa_secret
	OTPAuthURL string // otpauth://... URL clients turn into a QR code
}

// Enroll generates a new TOTP secret and the matching otpauth:// URL.
// accountName should be human-recognizable (typically the user's email).
func Enroll(accountName string) (Enrollment, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      Issuer,
		AccountName: accountName,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
		SecretSize:  20,
	})
	if err != nil {
		return Enrollment{}, fmt.Errorf("totp: generate: %w", err)
	}
	return Enrollment{
		Secret:     key.Secret(),
		OTPAuthURL: key.URL(),
	}, nil
}

// Verify checks the given 6-digit code against the stored secret at the
// supplied time. It tolerates a one-period skew on either side to absorb
// minor clock drift between server and client.
func Verify(secret, code string, at time.Time) bool {
	valid, _ := totp.ValidateCustom(code, secret, at, totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	return valid
}
