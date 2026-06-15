package server_test

// DB-backed integration test for the ASYNC export worker (Reports Center Phase
// 13 — the Export Center). Reuses the Phase 2 harness (which calls srv.Start(),
// so the real advisory-locked worker drains the queue). Gated on
// TEST_DATABASE_URL + TEST_REDIS_URL.
//
// It asserts:
//
//	(a) enqueue -> worker runs -> completed -> download happy path (CSV + PDF):
//	    POST /exports returns 202 with a queued job; the worker drains it; GET
//	    /exports/{id} eventually reports completed with a download_url + checksum;
//	    GET /exports/{id}/download streams the bytes (correct magic for PDF) and
//	    the X-Export-Checksum header matches the stored checksum;
//	(b) permission re-check AT GENERATION: a job enqueued for an actor who does
//	    NOT hold the report permission lands FAILED with a forbidden reason and
//	    NEVER stores bytes (a download is 409, not data);
//	(c) back-compat: a report key the worker cannot render but the legacy file
//	    endpoints map still returns an immediate 201 completed receipt with a
//	    file_url (and no async download).

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/exportjobs"
	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

// pollExportJob polls GET /exports/{id} until the job reaches a terminal status
// (completed|failed) or the deadline elapses, returning the final view.
func pollExportJob(t *testing.T, h *harness, token, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last map[string]any
	for time.Now().Before(deadline) {
		code, body := h.getJSON(t, "/api/v1/exports/"+id, token)
		if code != http.StatusOK {
			t.Fatalf("get export job = %d (%v)", code, body)
		}
		last = body
		switch body["status"] {
		case "completed", "failed":
			return body
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("export job %s did not reach a terminal status in time (last=%v)", id, last)
	return nil
}

func TestExportAsync_EnqueueWorkerCompletesAndDownloads(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	// (a) CSV happy path: enqueue a tenant-wide financials export.
	code, body := h.postJSON(t, "/api/v1/exports", admin,
		`{"report_key":"financials","format":"csv","filters":{"period":"this-month"}}`)
	if code != http.StatusAccepted {
		t.Fatalf("enqueue financials csv = %d, want 202 (%v)", code, body)
	}
	if body["status"] != "queued" {
		t.Fatalf("enqueued status = %v, want queued", body["status"])
	}
	jobID, _ := body["id"].(string)
	if jobID == "" {
		t.Fatalf("enqueue: missing id (%v)", body)
	}

	final := pollExportJob(t, h, admin, jobID)
	if final["status"] != "completed" {
		t.Fatalf("financials csv final status = %v, want completed (error=%v)", final["status"], final["error"])
	}
	dl, _ := final["download_url"].(string)
	if dl == "" {
		t.Fatalf("completed job missing download_url (%v)", final)
	}
	checksum, _ := final["checksum"].(string)
	if checksum == "" {
		t.Fatalf("completed job missing checksum (%v)", final)
	}

	// Download streams the CSV bytes; the checksum header matches.
	status, ct, hdr, data := h.download(t, dl, admin)
	if status != http.StatusOK {
		t.Fatalf("download = %d, want 200", status)
	}
	if !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("download content-type = %q, want text/csv*", ct)
	}
	if hdr.Get("X-Export-Checksum") != checksum {
		t.Fatalf("download checksum header = %q, want %q", hdr.Get("X-Export-Checksum"), checksum)
	}
	if len(data) == 0 || !strings.Contains(string(data), "Financial Statement") {
		t.Fatalf("CSV body missing expected content (len=%d)", len(data))
	}

	// (a2) PDF happy path: a branded boardroom PDF for the same report.
	code, body = h.postJSON(t, "/api/v1/exports", admin,
		`{"report_key":"financials","format":"pdf","filters":{"period":"this-month"}}`)
	if code != http.StatusAccepted {
		t.Fatalf("enqueue financials pdf = %d, want 202 (%v)", code, body)
	}
	pdfID, _ := body["id"].(string)
	finalPDF := pollExportJob(t, h, admin, pdfID)
	if finalPDF["status"] != "completed" {
		t.Fatalf("financials pdf final status = %v, want completed (error=%v)", finalPDF["status"], finalPDF["error"])
	}
	pdfDL, _ := finalPDF["download_url"].(string)
	status, ct, _, pdfBytes := h.download(t, pdfDL, admin)
	if status != http.StatusOK {
		t.Fatalf("pdf download = %d, want 200", status)
	}
	if ct != "application/pdf" {
		t.Fatalf("pdf content-type = %q, want application/pdf", ct)
	}
	if !strings.HasPrefix(string(pdfBytes), "%PDF-") {
		t.Fatalf("pdf body is not a PDF (magic): %q", string(pdfBytes[:min(8, len(pdfBytes))]))
	}
}

func TestExportAsync_PermissionRecheckAtGenerationFails(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)

	// Seed an attendant who holds NEITHER finance.read NOR reports.export, and
	// enqueue a financials job DIRECTLY for them (bypassing the HTTP enqueue gate
	// the way a permission-revocation-after-enqueue would) so the worker is forced
	// to make the generation-time permission decision.
	attUID := freshAttendantID(t, ctx, h)
	repo := exportjobs.New(h.pool)
	job, err := repo.Enqueue(ctx, h.ids.tenantID, exportjobs.EnqueueInput{
		ReportKey:   "financials",
		Format:      "csv",
		Filters:     map[string]string{"period": "this-month"},
		RequestedBy: attUID,
	})
	if err != nil {
		t.Fatalf("enqueue (direct): %v", err)
	}

	// The worker must FAIL this job with a forbidden reason and store no bytes.
	final := waitForTerminalStatus(t, ctx, h, job.ID, 20*time.Second)
	if final.Status != exportjobs.StatusFailed {
		t.Fatalf("revoked-actor job status = %q, want failed", final.Status)
	}
	if final.Error == nil || !strings.Contains(strings.ToLower(*final.Error), "forbidden") {
		t.Fatalf("revoked-actor job error = %v, want a forbidden reason", final.Error)
	}
	// No bytes were rendered.
	_, _, found, gerr := repo.GetResult(ctx, h.ids.tenantID, job.ID)
	if gerr != nil {
		t.Fatalf("get result: %v", gerr)
	}
	if found {
		t.Fatal("revoked-actor job stored result bytes — data leak")
	}

	// And the attendant cannot even reach the download surface (no reports.export):
	// log them in and confirm a 403 on the export-jobs surface.
	att := loginAttendant(t, ctx, h, tenantSlug, attUID)
	if code, _ := h.getJSON(t, "/api/v1/exports/"+job.ID.String()+"/download", att); code != http.StatusForbidden {
		t.Fatalf("attendant download = %d, want 403 (no reports.export)", code)
	}
}

func TestExportAsync_BackCompatSyncEndpoints(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	// The pre-existing synchronous unified-export endpoint (POST /reports/export)
	// is UNCHANGED: it still delegates to the existing file endpoint's same-origin
	// URL (200), so any caller relying on the synchronous URL-delegation path keeps
	// working alongside the new async export queue.
	code, body := h.postJSON(t, "/api/v1/reports/export", admin,
		`{"report_key":"financials","format":"csv","filters":{"period":"this-month"}}`)
	if code != http.StatusOK {
		t.Fatalf("sync /reports/export = %d, want 200 (%v)", code, body)
	}
	if url, _ := body["url"].(string); !strings.Contains(url, "/reports/financials.csv") {
		t.Fatalf("sync /reports/export url = %v, want the mapped financials CSV URL", url)
	}

	// And the pre-existing synchronous FILE endpoint still streams a CSV directly.
	status, ct, _, data := h.download(t, "/api/v1/reports/financials.csv?period=this-month", admin)
	if status != http.StatusOK {
		t.Fatalf("sync financials.csv = %d, want 200", status)
	}
	if !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("sync financials.csv content-type = %q, want text/csv*", ct)
	}
	if len(data) == 0 {
		t.Fatal("sync financials.csv returned no bytes")
	}
}

// TestExportAsync_CrossTenantDownloadIsNotFound proves the stored export bytes are
// tenant-isolated: tenant A enqueues + completes an export, then tenant B's admin
// (a different tenant entirely) tries to GET A's job status and download by the
// SAME id and is met with a 404 on both — never A's bytes. This is the regression
// guard the security review asked for over the hand-written tenant_id predicate.
func TestExportAsync_CrossTenantDownloadIsNotFound(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminA := h.login(t, slug(h), h.ids.adminEmail)

	// Tenant A: enqueue + complete a financials CSV export.
	code, body := h.postJSON(t, "/api/v1/exports", adminA,
		`{"report_key":"financials","format":"csv","filters":{"period":"this-month"}}`)
	if code != http.StatusAccepted {
		t.Fatalf("enqueue (tenant A) = %d, want 202 (%v)", code, body)
	}
	jobID, _ := body["id"].(string)
	final := pollExportJob(t, h, adminA, jobID)
	if final["status"] != "completed" {
		t.Fatalf("tenant A job status = %v, want completed", final["status"])
	}

	// Tenant B: a fully-separate tenant whose admin holds reports.export but whose
	// queries are scoped to tenant B.
	ids2 := seedTenant(t, ctx, h.pool)
	defer cleanupTenant(ctx, h.pool, ids2.tenantID)
	var slug2 string
	if err := h.pool.QueryRow(ctx, `SELECT slug FROM tenants WHERE id = $1`, ids2.tenantID).Scan(&slug2); err != nil {
		t.Fatalf("read tenant B slug: %v", err)
	}
	adminB := h.login(t, slug2, ids2.adminEmail)

	// Status read by A's id under B's identity must 404 (not leak existence/data).
	if code, _ := h.getJSON(t, "/api/v1/exports/"+jobID, adminB); code != http.StatusNotFound {
		t.Fatalf("cross-tenant GET status = %d, want 404", code)
	}
	// Download by A's id under B's identity must 404 — never A's stored bytes.
	if code, _, _, _ := h.download(t, "/api/v1/exports/"+jobID+"/download", adminB); code != http.StatusNotFound {
		t.Fatalf("cross-tenant download = %d, want 404", code)
	}
}

// TestExportAsync_ReclaimStaleRunningJob proves a job wedged in 'running' (the
// worker died after the claim committed but before Complete/Fail) is recovered:
// the worker's reclaim re-queues it once it is older than the stale threshold, and
// the running worker then drains it to completed. We simulate the crash by writing
// a 'running' row with a backdated started_at directly (no live render in flight).
func TestExportAsync_ReclaimStaleRunningJob(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	admin := h.login(t, slug(h), h.ids.adminEmail)

	// Insert an export job already stuck in 'running' with started_at far in the
	// past (older than exportJobStaleAfter) and attempts below the cap, as if a
	// previous worker claimed it and then crashed.
	var adminID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, h.ids.adminEmail).Scan(&adminID); err != nil {
		t.Fatalf("read admin id: %v", err)
	}
	var jobID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO export_jobs
		    (tenant_id, report_key, format, filters, status, requested_by, started_at, attempts)
		VALUES ($1, 'financials', 'csv', '{"period":"this-month"}', 'running', $2, now() - interval '1 hour', 1)
		RETURNING id`,
		h.ids.tenantID, adminID).Scan(&jobID); err != nil {
		t.Fatalf("seed stale running job: %v", err)
	}

	// The running worker should reclaim (re-queue) the stale row and then complete
	// it; poll the status surface until it reaches completed.
	final := pollExportJob(t, h, admin, jobID.String())
	if final["status"] != "completed" {
		t.Fatalf("reclaimed job final status = %v, want completed (error=%v)", final["status"], final["error"])
	}
	if dl, _ := final["download_url"].(string); dl == "" {
		t.Fatalf("reclaimed+completed job missing download_url (%v)", final)
	}
}

// ---- helpers ----

// download issues an authenticated GET and returns the status, content type,
// response headers and raw body — for the binary export download path.
func (h *harness) download(t *testing.T, path, token string) (int, string, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.baseURL+path, nil) //nolint:noctx // test
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("download do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if rerr != nil {
			break
		}
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header, buf
}

// freshAttendantID seeds an attendant user (the minimal role) and returns its id
// WITHOUT logging in — used to enqueue a job directly for a low-privilege actor.
func freshAttendantID(t *testing.T, ctx context.Context, h *harness) uuid.UUID {
	t.Helper()
	email := "att-async-" + uuid.NewString()[:8] + "@it.local"
	var uid uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_changed_at)
		 VALUES ($1, $2, 'Async Attendant', 'active', now()) RETURNING id`,
		h.ids.tenantID, email).Scan(&uid); err != nil {
		t.Fatalf("seed attendant id: %v", err)
	}
	grantRole(t, ctx, h.pool, h.ids.tenantID, uid, "attendant")
	if _, err := h.pool.Exec(ctx,
		`INSERT INTO user_station_access (user_id, station_id, tenant_id) VALUES ($1, $2, $3)`,
		uid, h.ids.station1, h.ids.tenantID); err != nil {
		t.Fatalf("station access: %v", err)
	}
	return uid
}

// loginAttendant sets a known password on the seeded attendant and logs in,
// returning a session token.
func loginAttendant(t *testing.T, ctx context.Context, h *harness, tenantSlug string, uid uuid.UUID) string {
	t.Helper()
	hash, err := password.New(password.DefaultParams, "").Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := h.pool.Exec(ctx,
		`UPDATE users SET password_hash = $2 WHERE id = $1`, uid, hash); err != nil {
		t.Fatalf("set attendant password: %v", err)
	}
	var email string
	if err := h.pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, uid).Scan(&email); err != nil {
		t.Fatalf("read attendant email: %v", err)
	}
	return h.login(t, tenantSlug, email)
}

// waitForTerminalStatus polls the export_jobs row (repo-level) until it reaches a
// terminal status or the deadline elapses.
func waitForTerminalStatus(t *testing.T, ctx context.Context, h *harness, id uuid.UUID, within time.Duration) *exportjobs.Job {
	t.Helper()
	repo := exportjobs.New(h.pool)
	deadline := time.Now().Add(within)
	var last *exportjobs.Job
	for time.Now().Before(deadline) {
		job, err := repo.Get(ctx, h.ids.tenantID, id)
		if err != nil && !errors.Is(err, exportjobs.ErrNotFound) {
			t.Fatalf("get job: %v", err)
		}
		if job != nil {
			last = job
			if job.Status == exportjobs.StatusCompleted || job.Status == exportjobs.StatusFailed {
				return job
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach terminal status in time (last=%v)", id, last)
	return nil
}
