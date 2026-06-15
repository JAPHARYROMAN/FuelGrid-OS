package server_test

// DB-backed integration test for the export-jobs surface (Feature 10.7). Reuses
// the Phase 2 harness; gated on TEST_DATABASE_URL + TEST_REDIS_URL.
//
//	TEST_DATABASE_URL=postgres://fuelgrid:fuelgrid@localhost:5433/fuelgrid?sslmode=disable \
//	TEST_REDIS_URL=redis://localhost:6379/0 \
//	go test ./services/api/internal/server -run ExportJobs -v
//
// It asserts:
//
//	(a) POST /exports records a completed job mapping a {report_key, format,
//	    filters} onto the existing export file URL, and returns the job;
//	(b) the job appears in GET /exports and is fetchable by id;
//	(c) an unsupported report_key/format is rejected 400 (no row written);
//	(d) a freshly-created attendant (no reports.export) is forbidden the surface.

import (
	"context"
	"net/http"
	"testing"
)

func TestExportJobs_RecordListAndGet(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)

	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	// (a) enqueue a financials CSV export job. The surface is now ASYNCHRONOUS:
	// the POST returns 202 with a queued job (the worker renders it later).
	code, body := h.postJSON(t, "/api/v1/exports", admin,
		`{"report_key":"financials","format":"csv","filters":{"period":"this-month"}}`)
	if code != http.StatusAccepted {
		t.Fatalf("enqueue export job = %d, want 202 (%v)", code, body)
	}
	jobID, _ := body["id"].(string)
	if jobID == "" {
		t.Fatalf("enqueue export job: missing id (%v)", body)
	}
	if body["status"] != "queued" {
		t.Fatalf("status = %v, want queued", body["status"])
	}
	if body["report_key"] != "financials" || body["format"] != "csv" {
		t.Fatalf("job key/format = %v/%v, want financials/csv", body["report_key"], body["format"])
	}

	// (b) it shows in the history and is fetchable by id.
	code, list := h.getJSON(t, "/api/v1/exports", admin)
	if code != http.StatusOK {
		t.Fatalf("list export jobs = %d, want 200 (%v)", code, list)
	}
	items, _ := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("export jobs count = %d, want 1", len(items))
	}
	code, one := h.getJSON(t, "/api/v1/exports/"+jobID, admin)
	if code != http.StatusOK {
		t.Fatalf("get export job = %d, want 200 (%v)", code, one)
	}
	if one["id"] != jobID {
		t.Fatalf("get export job id = %v, want %s", one["id"], jobID)
	}

	// (c) an unsupported combination is rejected and writes no row.
	code, _ = h.postJSON(t, "/api/v1/exports", admin,
		`{"report_key":"financials","format":"docx"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("unsupported export = %d, want 400", code)
	}
	code, list = h.getJSON(t, "/api/v1/exports", admin)
	if code != http.StatusOK {
		t.Fatalf("re-list export jobs = %d, want 200", code)
	}
	if items, _ := list["items"].([]any); len(items) != 1 {
		t.Fatalf("export jobs count after rejected = %d, want 1 (rejected wrote no row)", len(items))
	}

	// (d) a freshly-created attendant holds no reports.export: 403 on the surface.
	att := freshAttendant(t, ctx, h, tenantSlug)
	if code, _ := h.getJSON(t, "/api/v1/exports", att); code != http.StatusForbidden {
		t.Fatalf("attendant list export jobs = %d, want 403", code)
	}
	if code, _ := h.postJSON(t, "/api/v1/exports", att,
		`{"report_key":"financials","format":"csv"}`); code != http.StatusForbidden {
		t.Fatalf("attendant create export job = %d, want 403", code)
	}
}
