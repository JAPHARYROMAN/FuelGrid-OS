package password

import (
	"strings"
	"testing"
)

// Lighter params for tests — production parameters cost ~50ms per hash.
var testParams = Params{
	Memory:      16 * 1024,
	Iterations:  2,
	Parallelism: 2,
	SaltLength:  16,
	KeyLength:   32,
}

func TestHashAndVerify(t *testing.T) {
	t.Parallel()

	h := New(testParams, "test-pepper")

	encoded, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=") {
		t.Fatalf("unexpected hash prefix: %q", encoded)
	}

	match, rehash, err := h.Verify("correct horse battery staple", encoded)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !match {
		t.Fatal("expected password to match")
	}
	if rehash {
		t.Error("did not expect rehash with current params")
	}

	wrong, _, err := h.Verify("wrong password", encoded)
	if err != nil {
		t.Fatalf("Verify(wrong): %v", err)
	}
	if wrong {
		t.Fatal("wrong password matched")
	}
}

func TestPepperChangesHash(t *testing.T) {
	t.Parallel()

	a := New(testParams, "pepper-a")
	b := New(testParams, "pepper-b")

	encoded, err := a.Hash("same-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	// Same password with a different pepper must not verify.
	match, _, err := b.Verify("same-password", encoded)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if match {
		t.Fatal("password verified against the wrong pepper")
	}
}

func TestRehashFlagFiresWhenParamsRaised(t *testing.T) {
	t.Parallel()

	weak := New(testParams, "p")

	encoded, err := weak.Hash("hello")
	if err != nil {
		t.Fatal(err)
	}

	stronger := New(Params{
		Memory:      32 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}, "p")

	match, rehash, err := stronger.Verify("hello", encoded)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !match {
		t.Fatal("expected match")
	}
	if !rehash {
		t.Fatal("expected rehash signal when params are stricter than stored")
	}
}

func TestEmptyPasswordRejected(t *testing.T) {
	t.Parallel()
	h := New(testParams, "")
	if _, err := h.Hash(""); err == nil {
		t.Fatal("expected empty-password error")
	}
}

func TestMalformedEncodingFails(t *testing.T) {
	t.Parallel()
	h := New(testParams, "")
	cases := []string{
		"",
		"plain",
		"$argon2id$v=19$m=1,t=1,p=1$@@@@$@@@@",   // unparseable base64
		"$argon2id$v=18$m=1,t=1,p=1$AAAA$AAAA",   // wrong version
		"$bcrypt$v=19$m=65536,t=3,p=4$AAAA$AAAA", // wrong algorithm
		"$argon2id$v=19$m=65536,t=3,p=4$AAAA",    // missing fields
	}
	for _, c := range cases {
		if _, _, err := h.Verify("anything", c); err == nil {
			t.Errorf("expected error for malformed input %q", c)
		}
	}
}
