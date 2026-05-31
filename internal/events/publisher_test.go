package events

import (
	"testing"

	"github.com/google/uuid"
)

// TestClassifyFailure covers the retry/dead-letter decision the publisher
// applies to a row whose dispatch failed. This is the DB-free core of the
// outbox retry logic; the actual column updates are exercised by the
// DB-backed integration tests in CI.
func TestClassifyFailure(t *testing.T) {
	t.Parallel()

	id := uuid.New()

	tests := []struct {
		name          string
		priorAttempts int
		wantAttempt   int
		wantDeadLet   bool
	}{
		{
			name:          "first failure increments and keeps retrying",
			priorAttempts: 0,
			wantAttempt:   1,
			wantDeadLet:   false,
		},
		{
			name:          "mid-budget failure still retries",
			priorAttempts: 5,
			wantAttempt:   6,
			wantDeadLet:   false,
		},
		{
			name:          "one short of budget still retries",
			priorAttempts: MaxOutboxAttempts - 2,
			wantAttempt:   MaxOutboxAttempts - 1,
			wantDeadLet:   false,
		},
		{
			name:          "reaching the budget dead-letters",
			priorAttempts: MaxOutboxAttempts - 1,
			wantAttempt:   MaxOutboxAttempts,
			wantDeadLet:   true,
		},
		{
			name:          "past the budget stays dead-lettered",
			priorAttempts: MaxOutboxAttempts + 3,
			wantAttempt:   MaxOutboxAttempts + 4,
			wantDeadLet:   true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := classifyFailure(id, tt.priorAttempts)

			if got.id != id {
				t.Errorf("id = %v, want %v", got.id, id)
			}
			if got.attemptCount != tt.wantAttempt {
				t.Errorf("attemptCount = %d, want %d", got.attemptCount, tt.wantAttempt)
			}
			if got.deadLetter != tt.wantDeadLet {
				t.Errorf("deadLetter = %t, want %t", got.deadLetter, tt.wantDeadLet)
			}
		})
	}
}
