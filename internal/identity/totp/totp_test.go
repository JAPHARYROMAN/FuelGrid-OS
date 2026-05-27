package totp

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func TestEnrollProducesValidURL(t *testing.T) {
	t.Parallel()

	e, err := Enroll("user@example.com")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	if e.Secret == "" {
		t.Fatal("secret was empty")
	}
	if !strings.HasPrefix(e.OTPAuthURL, "otpauth://totp/") {
		t.Fatalf("unexpected URL: %s", e.OTPAuthURL)
	}
	if !strings.Contains(e.OTPAuthURL, "issuer=FuelGrid+OS") &&
		!strings.Contains(e.OTPAuthURL, "issuer=FuelGrid%20OS") {
		t.Fatalf("issuer not in URL: %s", e.OTPAuthURL)
	}
}

func TestVerifyAcceptsCurrentCode(t *testing.T) {
	t.Parallel()

	e, err := Enroll("user@example.com")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	code, err := totp.GenerateCodeCustom(e.Secret, now, totp.ValidateOpts{
		Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !Verify(e.Secret, code, now) {
		t.Fatal("freshly generated code did not verify")
	}
}

func TestVerifyAcceptsSkew(t *testing.T) {
	t.Parallel()

	e, err := Enroll("user@example.com")
	if err != nil {
		t.Fatal(err)
	}

	// Code generated for the previous 30s window should still validate
	// thanks to the configured skew tolerance.
	prev := time.Now().Add(-30 * time.Second)
	code, err := totp.GenerateCodeCustom(e.Secret, prev, totp.ValidateOpts{
		Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !Verify(e.Secret, code, time.Now()) {
		t.Fatal("expected verification to tolerate one-step skew")
	}
}

func TestVerifyRejectsWrongCode(t *testing.T) {
	t.Parallel()

	e, err := Enroll("user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if Verify(e.Secret, "000000", time.Now()) {
		t.Fatal("wrong code verified")
	}
}
