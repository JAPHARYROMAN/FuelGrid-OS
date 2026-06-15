package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestNewFiltersDisabledJobs proves a job with a non-positive interval (or a nil
// body) is dropped at construction, so a zero-value Intervals — what the
// integration harness's Config{} yields — registers nothing to run.
func TestNewFiltersDisabledJobs(t *testing.T) {
	t.Parallel()
	noop := func(context.Context) (string, error) { return "", nil }
	s := New(nil, nil, nil, 0,
		Job{Name: "enabled", Interval: time.Minute, Run: noop},
		Job{Name: "zero_interval", Interval: 0, Run: noop},
		Job{Name: "negative_interval", Interval: -time.Second, Run: noop},
		Job{Name: "nil_body", Interval: time.Minute, Run: nil},
	)
	if got := len(s.jobs); got != 1 {
		t.Fatalf("expected 1 enabled job, got %d", got)
	}
	if s.jobs[0].Name != "enabled" {
		t.Fatalf("expected the enabled job to survive, got %q", s.jobs[0].Name)
	}
}

// TestNewLockTimeoutDefault: a non-positive lock timeout falls back to the
// in-code default rather than leaving every job with a zero deadline.
func TestNewLockTimeoutDefault(t *testing.T) {
	t.Parallel()
	if s := New(nil, nil, nil, 0); s.lockTimeout != 10*time.Minute {
		t.Fatalf("expected default lock timeout 10m, got %s", s.lockTimeout)
	}
	if s := New(nil, nil, nil, 30*time.Second); s.lockTimeout != 30*time.Second {
		t.Fatalf("expected explicit lock timeout to be kept, got %s", s.lockTimeout)
	}
}

// TestStartStopNoJobs: a scheduler with no enabled jobs starts and stops
// cleanly and never blocks (the harness path).
func TestStartStopNoJobs(t *testing.T) {
	t.Parallel()
	s := New(nil, nil, nil, 0)
	s.Start()
	s.Start() // idempotent
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop returned error with no jobs: %v", err)
	}
	if err := s.Stop(ctx); err != nil { // idempotent
		t.Fatalf("second Stop returned error: %v", err)
	}
}

// TestJobLockKeyStableAndDistinct: the advisory-lock key is deterministic for a
// given name (so every replica computes the same key for the same job) and
// distinct across the real job names (so two jobs don't needlessly serialise).
func TestJobLockKeyStableAndDistinct(t *testing.T) {
	t.Parallel()
	names := []string{
		"revenue_compute", "aging_refresh", "risk_detect",
		"enterprise_projection", "outbox_dead_letter_sweep", "session_token_cleanup",
		"retention_sweep", "scheduled_reports_dispatch",
	}
	seen := map[int64]string{}
	for _, n := range names {
		k1, k2 := jobLockKey(n), jobLockKey(n)
		if k1 != k2 {
			t.Fatalf("lock key for %q is not stable: %d vs %d", n, k1, k2)
		}
		if prev, ok := seen[k1]; ok {
			t.Fatalf("lock key collision: %q and %q both hash to %d", prev, n, k1)
		}
		seen[k1] = n
	}
}

// TestRunBodyRecoversPanic: a panicking job body is converted to an error so the
// runner isolates it exactly like a returned error (one bad job never crashes
// the process or its sibling jobs).
func TestRunBodyRecoversPanic(t *testing.T) {
	t.Parallel()
	s := New(nil, nil, nil, 0)
	_, err := s.runBody(context.Background(), Job{
		Name: "boom",
		Run:  func(context.Context) (string, error) { panic("kaboom") },
	})
	if err == nil {
		t.Fatal("expected an error from a panicking job body")
	}
	if !strings.Contains(err.Error(), "panic") || !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("expected wrapped panic error, got %v", err)
	}
}

// TestTruncateDetail caps overlong detail strings and leaves short ones alone.
func TestTruncateDetail(t *testing.T) {
	t.Parallel()
	if got := truncateDetail("short"); got != "short" {
		t.Fatalf("short string altered: %q", got)
	}
	long := strings.Repeat("x", 5000)
	if got := truncateDetail(long); len(got) != 1000 {
		t.Fatalf("expected truncation to 1000, got len %d", len(got))
	}
}

// TestBuildJobsCatalog: the catalog wires the expected six jobs by name with
// the supplied intervals, so the names match the metric labels and lock keys.
func TestBuildJobsCatalog(t *testing.T) {
	t.Parallel()
	jobs := BuildJobs(Deps{}, Intervals{
		RevenueCompute:   time.Hour,
		AgingRefresh:     time.Hour,
		RiskDetect:       time.Hour,
		Projection:       time.Hour,
		OutboxSweep:      time.Hour,
		SessionCleanup:   time.Hour,
		RetentionSweep:   time.Hour,
		ScheduledReports: time.Hour,
	})
	want := map[string]bool{
		"revenue_compute": false, "aging_refresh": false, "risk_detect": false,
		"enterprise_projection": false, "outbox_dead_letter_sweep": false, "session_token_cleanup": false,
		"retention_sweep": false, "scheduled_reports_dispatch": false,
	}
	if len(jobs) != len(want) {
		t.Fatalf("expected %d jobs, got %d", len(want), len(jobs))
	}
	for _, j := range jobs {
		if _, ok := want[j.Name]; !ok {
			t.Fatalf("unexpected job %q", j.Name)
		}
		want[j.Name] = true
		if j.Run == nil {
			t.Fatalf("job %q has nil body", j.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("missing job %q", name)
		}
	}
}
