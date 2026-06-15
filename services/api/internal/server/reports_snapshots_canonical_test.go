package server

import (
	"strings"
	"testing"
	"time"
)

// TestCanonicalEnvelopeHash_StableAcrossRenderTime proves the snapshot content
// hash is STABLE across renders of identical data: two envelopes that differ ONLY
// in the volatile metadata.generated_at timestamp must hash identically (the
// canonical form excludes generated_at), while a real figure change must change
// the hash. This is the property the immutable-snapshot guarantee rests on.
func TestCanonicalEnvelopeHash_StableAcrossRenderTime(t *testing.T) {
	sid := "11111111-1111-1111-1111-111111111111"

	mk := func(gross, generatedAt string) ReportEnvelope {
		env := newEnvelope("station-close", "Daily Station Close", "current", &sid)
		env.Metadata.GeneratedAt = generatedAt
		env.Summary = []summaryMetric{{Label: "Sales value", Value: gross, Unit: "TZS"}}
		env.Table.Columns = []string{"business_date", "gross"}
		env.Table.Rows = [][]string{{"2026-05-10", gross}}
		return env
	}

	// Same data, DIFFERENT render timestamps -> identical hash.
	_, h1, err := canonicalEnvelopeJSON(mk("500000.00", time.Now().UTC().Format(time.RFC3339)))
	if err != nil {
		t.Fatalf("hash 1: %v", err)
	}
	_, h2, err := canonicalEnvelopeJSON(mk("500000.00", time.Now().Add(time.Hour).UTC().Format(time.RFC3339)))
	if err != nil {
		t.Fatalf("hash 2: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("hash not stable across render time: %q != %q (generated_at must be excluded)", h1, h2)
	}

	// A real figure change -> a DIFFERENT hash.
	_, h3, err := canonicalEnvelopeJSON(mk("600000.00", time.Now().UTC().Format(time.RFC3339)))
	if err != nil {
		t.Fatalf("hash 3: %v", err)
	}
	if h3 == h1 {
		t.Fatalf("hash unchanged after a figure change %q — the content hash must reflect the data", h3)
	}

	// The hash is a 64-char sha256 hex string.
	if len(h1) != 64 {
		t.Fatalf("content hash length = %d, want 64 (sha256 hex)", len(h1))
	}
}

// TestCanonicalEnvelopeJSON_StorageKeepsGeneratedAt proves the STORED bytes keep
// generated_at (an honest point-in-time view shows exactly what was rendered),
// even though the HASH excludes it.
func TestCanonicalEnvelopeJSON_StorageKeepsGeneratedAt(t *testing.T) {
	env := newEnvelope("financials", "Financial Statement", "this-month", nil)
	env.Metadata.GeneratedAt = "2026-05-10T08:00:00Z"
	storage, _, err := canonicalEnvelopeJSON(env)
	if err != nil {
		t.Fatalf("canonicalEnvelopeJSON: %v", err)
	}
	if !strings.Contains(string(storage), "2026-05-10T08:00:00Z") {
		t.Fatalf("stored envelope dropped generated_at; storage = %s", storage)
	}
}
