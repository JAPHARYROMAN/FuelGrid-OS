package identity

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp"
	potp "github.com/pquerna/otp/totp"

	"github.com/japharyroman/fuelgrid-os/internal/identity/secretcrypto"
	"github.com/japharyroman/fuelgrid-os/internal/identity/totp"
)

// fakeConsumeStore is an in-memory set-if-not-exists store with SetNX
// atomicity, standing in for Redis so the replay gate can be exercised without
// a live server. Keys never expire during a test.
type fakeConsumeStore struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func newFakeConsumeStore() *fakeConsumeStore {
	return &fakeConsumeStore{seen: make(map[string]struct{})}
}

func (f *fakeConsumeStore) SetNX(_ context.Context, key string, _ any, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.seen[key]; ok {
		return false, nil
	}
	f.seen[key] = struct{}{}
	return true, nil
}

// newTOTPTestService builds the minimal Service slice that checkTOTP exercises:
// the MFA cipher, the replay guard backed by an in-memory store, and a fixed
// clock. No database or Redis connection is needed.
func newTOTPTestService(store totp.ConsumeStore, at time.Time) *Service {
	return &Service{
		totpGuard: totp.NewGuard(store, "totp_used:"),
		mfaCipher: secretcrypto.New(""), // legacy plaintext passes through
		now:       func() time.Time { return at },
	}
}

func currentCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := potp.GenerateCodeCustom(secret, at, potp.ValidateOpts{
		Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	return code
}

// (a) The same valid TOTP code presented twice: first accepted, second rejected
// as a replay rather than re-accepted.
func TestCheckTOTPSameCodeTwiceRejectsSecond(t *testing.T) {
	t.Parallel()

	at := time.Now()
	enr, err := totp.Enroll("user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	svc := newTOTPTestService(newFakeConsumeStore(), at)
	uid := uuid.New()
	code := currentCode(t, enr.Secret, at)

	if err := svc.checkTOTP(context.Background(), uid, enr.Secret, code); err != nil {
		t.Fatalf("first presentation should succeed, got %v", err)
	}
	err = svc.checkTOTP(context.Background(), uid, enr.Secret, code)
	if !errors.Is(err, ErrTotpReplay) {
		t.Fatalf("second presentation should be ErrTotpReplay, got %v", err)
	}
}

func TestCheckTOTPWrongCodeIsInvalidNotReplay(t *testing.T) {
	t.Parallel()

	at := time.Now()
	enr, err := totp.Enroll("user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	svc := newTOTPTestService(newFakeConsumeStore(), at)

	err = svc.checkTOTP(context.Background(), uuid.New(), enr.Secret, "000000")
	if !errors.Is(err, ErrMfaInvalid) {
		t.Fatalf("a wrong code should be ErrMfaInvalid, got %v", err)
	}
}

// A wrong code must never consume a marker, so the user can immediately retry
// with the correct code in the same window.
func TestCheckTOTPWrongCodeDoesNotConsumeWindow(t *testing.T) {
	t.Parallel()

	at := time.Now()
	enr, err := totp.Enroll("user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	svc := newTOTPTestService(newFakeConsumeStore(), at)
	uid := uuid.New()

	if err := svc.checkTOTP(context.Background(), uid, enr.Secret, "000000"); !errors.Is(err, ErrMfaInvalid) {
		t.Fatalf("wrong code: want ErrMfaInvalid, got %v", err)
	}
	code := currentCode(t, enr.Secret, at)
	if err := svc.checkTOTP(context.Background(), uid, enr.Secret, code); err != nil {
		t.Fatalf("correct code after a wrong one should succeed, got %v", err)
	}
}

// (b) Concurrent presentation of the same valid code: exactly one wins, the
// rest see ErrTotpReplay.
func TestCheckTOTPConcurrentExactlyOneWinner(t *testing.T) {
	t.Parallel()

	at := time.Now()
	enr, err := totp.Enroll("user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	svc := newTOTPTestService(newFakeConsumeStore(), at)
	uid := uuid.New()
	code := currentCode(t, enr.Secret, at)

	const racers = 32
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners int
		replays int
	)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := svc.checkTOTP(context.Background(), uid, enr.Secret, code)
			mu.Lock()
			switch {
			case err == nil:
				winners++
			case errors.Is(err, ErrTotpReplay):
				replays++
			default:
				t.Errorf("unexpected error: %v", err)
			}
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	if winners != 1 {
		t.Fatalf("expected exactly one winner, got %d (replays=%d)", winners, replays)
	}
	if replays != racers-1 {
		t.Fatalf("expected %d replays, got %d", racers-1, replays)
	}
}
