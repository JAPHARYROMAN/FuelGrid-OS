package totp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeStore is an in-memory ConsumeStore with the same set-if-not-exists
// atomicity guarantee as Redis SetNX: a key may be written exactly once until
// it is dropped. TTL is recorded for assertions but entries are not expired
// during a test (the windows under test are far shorter than any test runtime).
type fakeStore struct {
	mu   sync.Mutex
	keys map[string]time.Duration
}

func newFakeStore() *fakeStore {
	return &fakeStore{keys: make(map[string]time.Duration)}
}

func (f *fakeStore) SetNX(_ context.Context, key string, _ any, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.keys[key]; exists {
		return false, nil
	}
	f.keys[key] = ttl
	return true, nil
}

func TestGuardConsumeFirstUseSucceeds(t *testing.T) {
	t.Parallel()

	g := NewGuard(newFakeStore(), "")
	uid := uuid.New()

	if err := g.Consume(context.Background(), uid, "123456"); err != nil {
		t.Fatalf("first use should succeed, got %v", err)
	}
}

// (a) the same code used twice -> the second is rejected with ErrReplay.
func TestGuardConsumeSameCodeTwiceRejectsSecond(t *testing.T) {
	t.Parallel()

	g := NewGuard(newFakeStore(), "")
	uid := uuid.New()

	if err := g.Consume(context.Background(), uid, "654321"); err != nil {
		t.Fatalf("first use should succeed, got %v", err)
	}
	err := g.Consume(context.Background(), uid, "654321")
	if !errors.Is(err, ErrReplay) {
		t.Fatalf("second use should be ErrReplay, got %v", err)
	}
}

func TestGuardConsumeIsolatesUsersAndCodes(t *testing.T) {
	t.Parallel()

	g := NewGuard(newFakeStore(), "")
	alice, bob := uuid.New(), uuid.New()

	if err := g.Consume(context.Background(), alice, "111111"); err != nil {
		t.Fatalf("alice first use: %v", err)
	}
	// Same code, different user — independent marker, must succeed.
	if err := g.Consume(context.Background(), bob, "111111"); err != nil {
		t.Fatalf("bob using the same digits should be independent, got %v", err)
	}
	// Same user, different code — independent marker, must succeed.
	if err := g.Consume(context.Background(), alice, "222222"); err != nil {
		t.Fatalf("alice using a different code should succeed, got %v", err)
	}
}

// (b) concurrent use of the same code -> exactly one winner.
func TestGuardConsumeConcurrentExactlyOneWinner(t *testing.T) {
	t.Parallel()

	g := NewGuard(newFakeStore(), "")
	uid := uuid.New()
	const racers = 64

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
			<-start // line everyone up so the calls actually contend
			err := g.Consume(context.Background(), uid, "424242")
			mu.Lock()
			switch {
			case err == nil:
				winners++
			case errors.Is(err, ErrReplay):
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

// errStore always fails SetNX, modelling a backing-store outage.
type errStore struct{}

func (errStore) SetNX(context.Context, string, any, time.Duration) (bool, error) {
	return false, errors.New("store down")
}

func TestGuardConsumeStoreErrorFailsClosed(t *testing.T) {
	t.Parallel()

	g := NewGuard(errStore{}, "")
	err := g.Consume(context.Background(), uuid.New(), "999999")
	if err == nil {
		t.Fatal("a store error must surface (fail closed), got nil")
	}
	if errors.Is(err, ErrReplay) {
		t.Fatalf("a store error must not be reported as a replay, got %v", err)
	}
}

func TestGuardConsumeUsesAcceptanceWindowTTL(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	g := NewGuard(store, "")
	uid := uuid.New()
	if err := g.Consume(context.Background(), uid, "303030"); err != nil {
		t.Fatalf("consume: %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, ttl := range store.keys {
		if ttl != AcceptanceWindow {
			t.Fatalf("marker TTL = %v, want %v", ttl, AcceptanceWindow)
		}
	}
}
