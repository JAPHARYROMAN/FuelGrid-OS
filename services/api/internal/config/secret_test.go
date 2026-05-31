package config

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// TestSecretRedaction proves a populated Secret never renders its plaintext
// through String, fmt verbs, or slog, while an empty Secret stays empty so
// "is this configured?" log lines remain truthful (OPS-2/MT-5/SEC-7).
func TestSecretRedaction(t *testing.T) {
	const plaintext = "super-secret-pepper-value"
	s := Secret(plaintext)

	if got := s.String(); got != redactedPlaceholder {
		t.Fatalf("String() = %q, want %q", got, redactedPlaceholder)
	}
	if got := s.Reveal(); got != plaintext {
		t.Fatalf("Reveal() = %q, want plaintext", got)
	}

	// fmt %s/%v go through Stringer.
	for _, verb := range []string{"%s", "%v", "%+v"} {
		out := fmt.Sprintf(verb, s)
		if strings.Contains(out, plaintext) {
			t.Fatalf("Sprintf(%q) leaked plaintext: %q", verb, out)
		}
		if out != redactedPlaceholder {
			t.Fatalf("Sprintf(%q) = %q, want %q", verb, out, redactedPlaceholder)
		}
	}

	// slog reads LogValue, not String, when the value is an attribute.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	logger.Info("connecting", "dsn", s, "token", Secret("bearer-xyz"))
	if logged := buf.String(); strings.Contains(logged, plaintext) || strings.Contains(logged, "bearer-xyz") {
		t.Fatalf("slog leaked secret plaintext: %q", logged)
	}
}

// TestSecretEmptyStaysEmpty makes sure an unset secret does not print the
// placeholder — otherwise readiness/config log lines would falsely imply a
// value is present.
func TestSecretEmptyStaysEmpty(t *testing.T) {
	var s Secret
	if got := s.String(); got != "" {
		t.Fatalf("empty Secret String() = %q, want empty", got)
	}
	if got := fmt.Sprintf("%v", s); got != "" {
		t.Fatalf("empty Secret %%v = %q, want empty", got)
	}
	if got := s.LogValue().String(); got != "" {
		t.Fatalf("empty Secret LogValue() = %q, want empty", got)
	}
}
