package server_test

// DB-backed integration test for report snapshots & locking (Reports Center
// Phase 14 — blueprint §15). Reuses the Phase 2 harness; gated on
// TEST_DATABASE_URL + TEST_REDIS_URL.
//
//	TEST_DATABASE_URL=postgres://fuelgrid:fuelgrid@127.0.0.1:5453/fuelgrid_snap?sslmode=disable \
//	TEST_REDIS_URL=redis://127.0.0.1:6399/0 \
//	go test ./services/api/internal/server -run Snapshot -v
//
// It proves the core property — IMMUTABILITY — not just at the Go layer but at
// the DB: the trigger BLOCKS an UPDATE/DELETE of the captured payload. It also
// asserts the stable canonical hash, the point-in-time view, the revision chain
// on reopen+recapture, audited state transitions, cross-tenant isolation, and the
// permission gate (an actor who cannot run the report cannot snapshot it).
//
// The station-scoped station-close report drives most cases (its envelope reads
// directly from a single revenue_days row, so mutating the live source to prove
// the stored view is frozen / a recapture differs is a one-line UPDATE). The
// permission gate uses the tenant-wide financials report, which an attendant
// (who DOES hold revenue.read at their own station) cannot run.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// seedStationCloseDay seeds a single revenue_days row for station1 so the
// station-close report renders real figures. Returns the operating day id.
func seedStationCloseDay(t *testing.T, ctx context.Context, h *harness, adminID uuid.UUID, businessDate, gross string) uuid.UUID {
	t.Helper()
	return seedStationCloseDayAt(t, ctx, h, h.ids.station1, adminID, businessDate, gross)
}

// seedStationCloseDayAt seeds a revenue_days row for a SPECIFIC station, so a test
// can stand up captureable station-close data on more than one station (e.g. to
// prove a station-A actor's snapshot list never returns station-B's snapshots).
func seedStationCloseDayAt(t *testing.T, ctx context.Context, h *harness, stationID, adminID uuid.UUID, businessDate, gross string) uuid.UUID {
	t.Helper()
	var dayID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO operating_days (tenant_id, station_id, business_date, opened_by)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, h.ids.tenantID, stationID, businessDate, adminID).Scan(&dayID); err != nil {
		t.Fatalf("seed operating day: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO revenue_days
		    (tenant_id, station_id, operating_day_id, business_date,
		     gross_revenue, net_revenue, cash_total, tender_total, cash_variance, status)
		VALUES ($1, $2, $3, $4, $5, $5, $5, $5, 0, 'draft')
	`, h.ids.tenantID, stationID, dayID, businessDate, gross); err != nil {
		t.Fatalf("seed revenue day: %v", err)
	}
	return dayID
}

// captureStationClose captures a snapshot of the station-close report for
// station1 and returns the created snapshot view.
func captureStationClose(t *testing.T, h *harness, token string) map[string]any {
	t.Helper()
	code, body := h.postJSON(t, "/api/v1/reports/station-close/snapshots", token,
		`{"filters":{"station_id":"`+h.ids.station1.String()+`"}}`)
	if code != http.StatusCreated {
		t.Fatalf("capture station-close snapshot = %d, want 201 (%v)", code, body)
	}
	return body
}

func snapID(t *testing.T, body map[string]any) string {
	t.Helper()
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("snapshot missing id (%v)", body)
	}
	return id
}

// TestSnapshot_CaptureImmutableStableHash proves capture creates an immutable
// snapshot with a STABLE canonical hash: capturing identical report data twice
// yields the SAME content hash (the per-render timestamp is excluded from the
// canonical form), and the DB trigger BLOCKS a direct UPDATE/DELETE of the
// captured payload — immutability proven at the database, not just asserted in Go.
func TestSnapshot_CaptureImmutableStableHash(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)
	seedStationCloseDay(t, ctx, h, adminID, "2026-05-10", "500000")

	// (a) capture twice; the canonical hash must be identical (same data -> same hash).
	first := captureStationClose(t, h, admin)
	second := captureStationClose(t, h, admin)
	h1, _ := first["content_hash"].(string)
	h2, _ := second["content_hash"].(string)
	if h1 == "" || h2 == "" {
		t.Fatalf("snapshots missing content_hash (%v / %v)", first, second)
	}
	if h1 != h2 {
		t.Fatalf("canonical hash not stable: %q vs %q (identical data must hash identically)", h1, h2)
	}
	if rev, _ := first["revision"].(float64); rev != 1 {
		t.Fatalf("first revision = %v, want 1", first["revision"])
	}
	if st, _ := first["status"].(string); st != "draft" {
		t.Fatalf("captured status = %v, want draft", first["status"])
	}

	id := snapID(t, first)

	// (b) DB-LEVEL IMMUTABILITY: a direct UPDATE of the envelope/content_hash/
	// captured_* must be rejected by the trigger (restrict_violation), not silently
	// applied.
	if _, uerr := h.pool.Exec(ctx,
		`UPDATE report_snapshots SET envelope = '{"tampered":true}'::jsonb WHERE id = $1`, id); uerr == nil {
		t.Fatalf("UPDATE of envelope succeeded — the immutability trigger must BLOCK it")
	}
	if _, uerr := h.pool.Exec(ctx,
		`UPDATE report_snapshots SET content_hash = 'deadbeef' WHERE id = $1`, id); uerr == nil {
		t.Fatalf("UPDATE of content_hash succeeded — the immutability trigger must BLOCK it")
	}
	if _, uerr := h.pool.Exec(ctx,
		`UPDATE report_snapshots SET captured_at = now() WHERE id = $1`, id); uerr == nil {
		t.Fatalf("UPDATE of captured_at succeeded — the immutability trigger must BLOCK it")
	}

	// (c) DELETE is blocked too (append-only).
	if _, derr := h.pool.Exec(ctx, `DELETE FROM report_snapshots WHERE id = $1`, id); derr == nil {
		t.Fatalf("DELETE succeeded — report_snapshots must be append-only")
	}

	// The row is intact after the blocked mutations.
	var hashNow string
	if err := h.pool.QueryRow(ctx, `SELECT content_hash FROM report_snapshots WHERE id = $1`, id).Scan(&hashNow); err != nil {
		t.Fatalf("re-read snapshot: %v", err)
	}
	if hashNow != h1 {
		t.Fatalf("content_hash changed despite blocked UPDATE: %q != %q", hashNow, h1)
	}

	// A permitted lifecycle UPDATE (sign-off) still works — only the payload is frozen.
	if code, _ := h.postJSON(t, "/api/v1/reports/snapshots/"+id+"/sign-off", admin, `{}`); code != http.StatusOK {
		t.Fatalf("sign-off after capture = %d, want 200 (lifecycle columns must remain mutable)", code)
	}
}

// TestSnapshot_ViewIsStoredNotLive proves the view returns the STORED envelope (a
// point-in-time capture), not a live re-run: after capturing, we MUTATE the live
// source (change the revenue day's gross), and the snapshot view must be
// UNCHANGED — byte-for-byte the captured envelope, with the original hash.
func TestSnapshot_ViewIsStoredNotLive(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)
	dayID := seedStationCloseDay(t, ctx, h, adminID, "2026-05-10", "500000")

	snap := captureStationClose(t, h, admin)
	id := snapID(t, snap)
	capturedHash, _ := snap["content_hash"].(string)

	// View the snapshot: it must return the stored envelope + the captured hash.
	code, view := h.getJSON(t, "/api/v1/reports/snapshots/"+id, admin)
	if code != http.StatusOK {
		t.Fatalf("view snapshot = %d, want 200 (%v)", code, view)
	}
	env, ok := view["envelope"].(map[string]any)
	if !ok {
		t.Fatalf("view missing stored envelope (%v)", view)
	}
	meta, _ := env["metadata"].(map[string]any)
	if rk, _ := meta["report_key"].(string); rk != "station-close" {
		t.Fatalf("stored envelope report_key = %v, want station-close", meta["report_key"])
	}
	if vh, _ := view["content_hash"].(string); vh != capturedHash {
		t.Fatalf("view content_hash = %q, want captured %q", view["content_hash"], capturedHash)
	}

	// MUTATE the live source: the live station-close would now render gross 999999,
	// but the SNAPSHOT must not move.
	if _, err := h.pool.Exec(ctx,
		`UPDATE revenue_days SET gross_revenue = 999999, net_revenue = 999999 WHERE operating_day_id = $1`, dayID); err != nil {
		t.Fatalf("mutate live revenue day: %v", err)
	}

	// Re-view: byte-for-byte identical stored envelope, same hash. The snapshot is a
	// frozen point-in-time view, unaffected by the live mutation.
	code, view2 := h.getJSON(t, "/api/v1/reports/snapshots/"+id, admin)
	if code != http.StatusOK {
		t.Fatalf("re-view snapshot = %d, want 200", code)
	}
	if vh, _ := view2["content_hash"].(string); vh != capturedHash {
		t.Fatalf("snapshot hash changed after live mutation: %q != %q (view must be stored, not live)", vh, capturedHash)
	}
	b1, _ := json.Marshal(view["envelope"])
	b2, _ := json.Marshal(view2["envelope"])
	if string(b1) != string(b2) {
		t.Fatalf("stored envelope changed after a live mutation — the view must be a frozen capture")
	}

	// Sanity: a LIVE run now reflects the mutated figure (proving the source moved
	// and the snapshot genuinely diverged from live).
	code, live := h.getJSON(t, "/api/v1/reports/station-close?station_id="+h.ids.station1.String(), admin)
	if code != http.StatusOK {
		t.Fatalf("live station-close = %d, want 200", code)
	}
	if v := summaryValue(live, "Sales value"); v != "999999.00" {
		t.Fatalf("live Sales value = %q, want 999999.00 (the live source moved)", v)
	}
}

// TestSnapshot_SignOffReopenRevisionChain proves the sign-off / reopen / recapture
// lifecycle: sign-off locks the snapshot (status signed_off, signer recorded);
// reopen REQUIRES a correction note and marks it reopened; a recapture superseding
// it creates a NEW revision (2) with supersedes_id -> the prior, the ORIGINAL
// preserved, and a DIFFERENT hash once the underlying data changed.
func TestSnapshot_SignOffReopenRevisionChain(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)
	dayID := seedStationCloseDay(t, ctx, h, adminID, "2026-05-10", "500000")

	rev1 := captureStationClose(t, h, admin)
	id1 := snapID(t, rev1)
	hash1, _ := rev1["content_hash"].(string)

	// Sign off: status -> signed_off, signer recorded.
	code, signed := h.postJSON(t, "/api/v1/reports/snapshots/"+id1+"/sign-off", admin, `{}`)
	if code != http.StatusOK {
		t.Fatalf("sign-off = %d, want 200 (%v)", code, signed)
	}
	if st, _ := signed["status"].(string); st != "signed_off" {
		t.Fatalf("signed status = %v, want signed_off", signed["status"])
	}
	if signed["signed_off_by"] == nil || signed["signed_off_at"] == nil {
		t.Fatalf("sign-off must record signer + time (%v)", signed)
	}

	// A signed-off snapshot now surfaces as the report's lock state (scope-accurate).
	code, lock := h.getJSON(t, "/api/v1/reports/station-close/lock-state?station_id="+h.ids.station1.String(), admin)
	if code != http.StatusOK {
		t.Fatalf("lock-state = %d, want 200 (%v)", code, lock)
	}
	if locked, _ := lock["locked"].(bool); !locked {
		t.Fatalf("lock-state locked = %v, want true after sign-off", lock["locked"])
	}
	if sid, _ := lock["snapshot_id"].(string); sid != id1 {
		t.Fatalf("lock-state snapshot_id = %v, want %s", lock["snapshot_id"], id1)
	}

	// Reopen WITHOUT a note -> 400 (a correction note is required).
	if code, _ := h.postJSON(t, "/api/v1/reports/snapshots/"+id1+"/reopen", admin, `{}`); code != http.StatusBadRequest {
		t.Fatalf("reopen without note = %d, want 400", code)
	}
	// Reopen WITH a note -> 200, status reopened, sign-off cleared.
	code, reopened := h.postJSON(t, "/api/v1/reports/snapshots/"+id1+"/reopen", admin,
		`{"correction_note":"restate the day's cash variance"}`)
	if code != http.StatusOK {
		t.Fatalf("reopen with note = %d, want 200 (%v)", code, reopened)
	}
	if st, _ := reopened["status"].(string); st != "reopened" {
		t.Fatalf("reopened status = %v, want reopened", reopened["status"])
	}
	if reopened["signed_off_by"] != nil {
		t.Fatalf("reopen must clear the sign-off stamp (%v)", reopened)
	}

	// MUTATE the live source so the recapture differs, then recapture superseding
	// the reopened snapshot -> a NEW revision 2 with a DIFFERENT hash.
	if _, err := h.pool.Exec(ctx,
		`UPDATE revenue_days SET gross_revenue = 600000, net_revenue = 600000 WHERE operating_day_id = $1`, dayID); err != nil {
		t.Fatalf("mutate live revenue day: %v", err)
	}
	code, rev2 := h.postJSON(t, "/api/v1/reports/station-close/snapshots", admin,
		`{"filters":{"station_id":"`+h.ids.station1.String()+`"},"supersedes_id":"`+id1+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("recapture = %d, want 201 (%v)", code, rev2)
	}
	if rev, _ := rev2["revision"].(float64); rev != 2 {
		t.Fatalf("recapture revision = %v, want 2", rev2["revision"])
	}
	if sup, _ := rev2["supersedes_id"].(string); sup != id1 {
		t.Fatalf("recapture supersedes_id = %v, want %s (the reopened original)", rev2["supersedes_id"], id1)
	}
	if hash2, _ := rev2["content_hash"].(string); hash2 == hash1 {
		t.Fatalf("recapture after a data change produced the same hash %q — the new revision must differ", hash2)
	}

	// The ORIGINAL revision 1 is preserved (still readable, immutable payload).
	code, orig := h.getJSON(t, "/api/v1/reports/snapshots/"+id1, admin)
	if code != http.StatusOK {
		t.Fatalf("read original after recapture = %d, want 200", code)
	}
	if vh, _ := orig["content_hash"].(string); vh != hash1 {
		t.Fatalf("original revision hash changed: %q != %q (the prior revision must be preserved)", vh, hash1)
	}

	// The list endpoint returns the full revision chain (2 snapshots, newest first).
	code, list := h.getJSON(t, "/api/v1/reports/station-close/snapshots?station_id="+h.ids.station1.String(), admin)
	if code != http.StatusOK {
		t.Fatalf("list snapshots = %d, want 200 (%v)", code, list)
	}
	items, _ := list["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("revision chain length = %d, want 2", len(items))
	}
}

// TestSnapshot_TransitionsAudited proves capture, sign-off and reopen state
// transitions are recorded in the append-only audit trail (the audit_logs the
// snapshot surface writes via audit.WriteWithOutbox).
func TestSnapshot_TransitionsAudited(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)
	seedStationCloseDay(t, ctx, h, adminID, "2026-05-10", "500000")

	snap := captureStationClose(t, h, admin)
	id := snapID(t, snap)
	if code, _ := h.postJSON(t, "/api/v1/reports/snapshots/"+id+"/sign-off", admin, `{}`); code != http.StatusOK {
		t.Fatalf("sign-off = %d, want 200", code)
	}
	if code, _ := h.postJSON(t, "/api/v1/reports/snapshots/"+id+"/reopen", admin,
		`{"correction_note":"audit trail check"}`); code != http.StatusOK {
		t.Fatalf("reopen = %d, want 200", code)
	}

	for _, action := range []string{"report.snapshot.captured", "report.snapshot.signed_off", "report.snapshot.reopened"} {
		var n int
		if err := h.pool.QueryRow(ctx, `
			SELECT count(*) FROM audit_logs
			WHERE tenant_id = $1 AND action = $2 AND entity_id = $3
		`, h.ids.tenantID, action, id).Scan(&n); err != nil {
			t.Fatalf("count audit %s: %v", action, err)
		}
		if n == 0 {
			t.Fatalf("expected an audit_logs row for action %q on snapshot %s", action, id)
		}
	}
}

// TestSnapshot_CrossTenantIsolation proves tenant B cannot read or sign off tenant
// A's snapshot — the snapshot is invisible (404) across the tenant boundary.
func TestSnapshot_CrossTenantIsolation(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)
	seedStationCloseDay(t, ctx, h, adminID, "2026-05-10", "500000")

	snap := captureStationClose(t, h, admin)
	id := snapID(t, snap)

	// Stand up a second tenant + its admin in the same DB.
	ids2 := seedTenant(t, ctx, h.pool)
	defer cleanupTenant(ctx, h.pool, ids2.tenantID)
	var slug2 string
	if err := h.pool.QueryRow(ctx, `SELECT slug FROM tenants WHERE id = $1`, ids2.tenantID).Scan(&slug2); err != nil {
		t.Fatalf("tenant2 slug: %v", err)
	}
	other := h.login(t, slug2, ids2.adminEmail)

	// Tenant B cannot READ tenant A's snapshot (404 — not even existence leaks).
	if code, _ := h.getJSON(t, "/api/v1/reports/snapshots/"+id, other); code != http.StatusNotFound {
		t.Fatalf("cross-tenant read = %d, want 404", code)
	}
	// Tenant B cannot SIGN OFF tenant A's snapshot (404).
	if code, _ := h.postJSON(t, "/api/v1/reports/snapshots/"+id+"/sign-off", other, `{}`); code != http.StatusNotFound {
		t.Fatalf("cross-tenant sign-off = %d, want 404", code)
	}
}

// TestSnapshot_PermissionGated proves an actor who cannot RUN the report cannot
// capture or view its snapshot. The financials report needs finance.read, which a
// fresh attendant lacks (even though the attendant DOES hold revenue.read at its
// own station): the attendant is forbidden to capture a financials snapshot, and
// forbidden to view one an admin captured + signed off — a signed-off snapshot
// must not leak data the actor could not run live.
func TestSnapshot_PermissionGated(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	att := freshAttendant(t, ctx, h, tenantSlug)

	// The attendant (no finance.read) cannot CAPTURE a financials snapshot.
	if code, _ := h.postJSON(t, "/api/v1/reports/financials/snapshots", att,
		`{"filters":{"period":"this-month"}}`); code != http.StatusForbidden {
		t.Fatalf("attendant capture = %d, want 403 (no finance.read)", code)
	}

	// The admin captures + signs off a financials snapshot; the attendant must NOT be
	// able to VIEW it (it would expose finance data the attendant cannot run live).
	code, snap := h.postJSON(t, "/api/v1/reports/financials/snapshots", admin,
		`{"filters":{"period":"this-month"}}`)
	if code != http.StatusCreated {
		t.Fatalf("admin financials capture = %d, want 201 (%v)", code, snap)
	}
	id := snapID(t, snap)
	if code, _ := h.postJSON(t, "/api/v1/reports/snapshots/"+id+"/sign-off", admin, `{}`); code != http.StatusOK {
		t.Fatalf("admin sign-off = %d, want 200", code)
	}
	if code, _ := h.getJSON(t, "/api/v1/reports/snapshots/"+id, att); code != http.StatusForbidden {
		t.Fatalf("attendant view of signed-off snapshot = %d, want 403 (must not leak)", code)
	}
	// And the attendant cannot list financials snapshots either.
	if code, _ := h.getJSON(t, "/api/v1/reports/financials/snapshots", att); code != http.StatusForbidden {
		t.Fatalf("attendant list = %d, want 403", code)
	}
}

// TestSnapshot_ListIsStationScoped proves the snapshot LIST for a station-scoped
// report returns only the requested station's snapshots — a station-A actor (or
// admin querying ?station_id=A) must NOT receive station-B snapshot metadata
// (station ids, capturer/signer ids, hashes, timestamps, correction notes). This
// closes the cross-station metadata leak: ListForReport was keyed only by
// tenant+report_key, ignoring the station the actor was authorized for.
func TestSnapshot_ListIsStationScoped(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	// Capture a station-close snapshot on station1 AND on station2.
	seedStationCloseDayAt(t, ctx, h, h.ids.station1, adminID, "2026-05-10", "500000")
	seedStationCloseDayAt(t, ctx, h, h.ids.station2, adminID, "2026-05-10", "700000")

	s1 := captureStationClose(t, h, admin) // station1
	code, s2 := h.postJSON(t, "/api/v1/reports/station-close/snapshots", admin,
		`{"filters":{"station_id":"`+h.ids.station2.String()+`"}}`)
	if code != http.StatusCreated {
		t.Fatalf("capture station2 snapshot = %d, want 201 (%v)", code, s2)
	}
	id1, id2 := snapID(t, s1), snapID(t, s2)

	// List for station1 -> only station1's snapshot is returned.
	code, list1 := h.getJSON(t, "/api/v1/reports/station-close/snapshots?station_id="+h.ids.station1.String(), admin)
	if code != http.StatusOK {
		t.Fatalf("list station1 = %d, want 200 (%v)", code, list1)
	}
	items1, _ := list1["items"].([]any)
	if len(items1) != 1 {
		t.Fatalf("station1 list length = %d, want 1 (must not include station2)", len(items1))
	}
	if got := items1[0].(map[string]any)["id"]; got != id1 {
		t.Fatalf("station1 list returned id %v, want %s", got, id1)
	}

	// List for station2 -> only station2's snapshot. The leak would show id1 here.
	code, list2 := h.getJSON(t, "/api/v1/reports/station-close/snapshots?station_id="+h.ids.station2.String(), admin)
	if code != http.StatusOK {
		t.Fatalf("list station2 = %d, want 200 (%v)", code, list2)
	}
	items2, _ := list2["items"].([]any)
	if len(items2) != 1 {
		t.Fatalf("station2 list length = %d, want 1 (must not include station1)", len(items2))
	}
	if got := items2[0].(map[string]any)["id"]; got != id2 {
		t.Fatalf("station2 list returned id %v, want %s", got, id2)
	}
}

// TestSnapshot_RevisionChainCannotFork proves a reopened snapshot can be superseded
// AT MOST ONCE: after a recapture supersedes a reopened prior, a SECOND capture
// trying to supersede the SAME prior is rejected (the unique supersedes index keeps
// the revision chain a single line, never a fork). The chain stays linear.
func TestSnapshot_RevisionChainCannotFork(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)
	dayID := seedStationCloseDay(t, ctx, h, adminID, "2026-05-10", "500000")

	rev1 := captureStationClose(t, h, admin)
	id1 := snapID(t, rev1)

	// Sign off then reopen rev1, so it becomes a supersede-able 'reopened' prior.
	if code, _ := h.postJSON(t, "/api/v1/reports/snapshots/"+id1+"/sign-off", admin, `{}`); code != http.StatusOK {
		t.Fatalf("sign-off = %d, want 200", code)
	}
	if code, _ := h.postJSON(t, "/api/v1/reports/snapshots/"+id1+"/reopen", admin,
		`{"correction_note":"restate"}`); code != http.StatusOK {
		t.Fatalf("reopen = %d, want 200", code)
	}

	// First supersede succeeds (rev2).
	if _, err := h.pool.Exec(ctx,
		`UPDATE revenue_days SET gross_revenue = 600000, net_revenue = 600000 WHERE operating_day_id = $1`, dayID); err != nil {
		t.Fatalf("mutate live: %v", err)
	}
	code, rev2 := h.postJSON(t, "/api/v1/reports/station-close/snapshots", admin,
		`{"filters":{"station_id":"`+h.ids.station1.String()+`"},"supersedes_id":"`+id1+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("first supersede = %d, want 201 (%v)", code, rev2)
	}

	// SECOND supersede of the SAME reopened prior must be rejected — the chain
	// cannot fork. (id1 is still 'reopened', so the status guard alone would let it
	// through; the unique supersedes index is what blocks it -> 409.)
	code, forked := h.postJSON(t, "/api/v1/reports/station-close/snapshots", admin,
		`{"filters":{"station_id":"`+h.ids.station1.String()+`"},"supersedes_id":"`+id1+`"}`)
	if code != http.StatusConflict {
		t.Fatalf("second supersede of same prior = %d, want 409 conflict — the chain must not fork (%v)", code, forked)
	}

	// And the chain is still a single line: exactly one row supersedes id1.
	var superseders int
	if err := h.pool.QueryRow(ctx,
		`SELECT count(*) FROM report_snapshots WHERE supersedes_id = $1`, id1).Scan(&superseders); err != nil {
		t.Fatalf("count superseders: %v", err)
	}
	if superseders != 1 {
		t.Fatalf("id1 superseded %d times, want exactly 1 (no fork)", superseders)
	}
}

// TestSnapshot_AliasKeysRejected proves the snapshot surface accepts only the
// CANONICAL report key each builder stamps, never an export-surface alias. The
// "sales"/"revenue" aliases route to the station-close builder on the live/export
// paths, but capturing them as snapshots would store station-close figures under a
// mislabeled report_key on an immutable, sign-off-bearing record. They must 404.
func TestSnapshot_AliasKeysRejected(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)
	seedStationCloseDay(t, ctx, h, adminID, "2026-05-10", "500000")

	for _, alias := range []string{"sales", "revenue", "reconciliation", "receivables", "customer-aging"} {
		body := `{"filters":{"station_id":"` + h.ids.station1.String() + `"}}`
		if code, resp := h.postJSON(t, "/api/v1/reports/"+alias+"/snapshots", admin, body); code != http.StatusNotFound {
			t.Fatalf("capture alias %q = %d, want 404 (only canonical keys are snapshot-able) (%v)", alias, code, resp)
		}
	}

	// The canonical key still works.
	if code, _ := h.postJSON(t, "/api/v1/reports/station-close/snapshots", admin,
		`{"filters":{"station_id":"`+h.ids.station1.String()+`"}}`); code != http.StatusCreated {
		t.Fatalf("capture canonical station-close = %d, want 201", code)
	}
}
