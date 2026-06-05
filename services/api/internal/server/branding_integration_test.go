package server_test

// DB-backed integration tests for the tenant branding / company-letterhead API
// (LETTERHEAD). They reuse the Phase 2 harness (boots the real API, seeds a
// tenant + a system_admin holding companies.manage) and exercise the full
// branding surface end-to-end: text upsert round-trip, logo upload validation
// (too-large -> 413, non-image -> 400, valid PNG -> 200), logo streaming, and
// logo clearing.
//
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL like the rest of the suite, so
// `go test ./...` stays green without infra.

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"testing"
)

// brandingPNG returns a small valid PNG for upload tests.
func brandingPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for x := 0; x < 16; x++ {
		for y := 0; y < 16; y++ {
			img.Set(x, y, color.RGBA{R: 20, G: 120, B: 220, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// uploadBytes posts a single "file" multipart part with the given filename and
// raw bytes.
func (h *harness) uploadBytes(t *testing.T, path, token, filename string, data []byte) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	_, _ = fw.Write(data)
	_ = mw.Close()
	return h.do(t, http.MethodPost, path, token, &buf, mw.FormDataContentType())
}

func TestLetterhead_BrandingUpsertRoundTrips(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	admin := h.login(t, slug(h), h.ids.adminEmail)

	// PUT the full text block.
	putBody := `{
		"display_name":"Acme Fuels",
		"legal_name":"Acme Petroleum Holdings Ltd",
		"tax_id":"P051234567X",
		"registration_no":"C.12345",
		"address_line1":"1 Refinery Road",
		"city":"Nairobi",
		"country":"Kenya",
		"phone":"+254700000000",
		"email":"ops@acme.example",
		"website":"acme.example",
		"footer_note":"Confidential"
	}`
	code, raw := h.do(t, http.MethodPut, "/api/v1/branding", admin, bytes.NewReader([]byte(putBody)), "application/json")
	if code != http.StatusOK {
		t.Fatalf("PUT branding: code=%d body=%s", code, raw)
	}

	// GET reflects what we wrote.
	code, m := h.getJSON(t, "/api/v1/branding", admin)
	if code != http.StatusOK {
		t.Fatalf("GET branding: code=%d", code)
	}
	if m["display_name"] != "Acme Fuels" || m["tax_id"] != "P051234567X" || m["footer_note"] != "Confidential" {
		t.Fatalf("branding round-trip mismatch: %+v", m)
	}
	if hl, _ := m["has_logo"].(bool); hl {
		t.Fatalf("expected no logo before upload, got has_logo=true")
	}

	// Clearing a field round-trips to empty (stored NULL, returned "").
	code, _ = h.do(t, http.MethodPut, "/api/v1/branding", admin,
		bytes.NewReader([]byte(`{"display_name":"Acme Fuels"}`)), "application/json")
	if code != http.StatusOK {
		t.Fatalf("PUT branding (clear): code=%d", code)
	}
	_, m = h.getJSON(t, "/api/v1/branding", admin)
	if m["footer_note"] != "" {
		t.Fatalf("expected footer_note cleared, got %q", m["footer_note"])
	}
}

func TestLetterhead_LogoUploadValidation(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	admin := h.login(t, slug(h), h.ids.adminEmail)

	// A too-large body (> 1 MiB) is rejected with 413.
	tooBig := bytes.Repeat([]byte{0x89}, (1<<20)+1024)
	if code, raw := h.uploadBytes(t, "/api/v1/branding/logo", admin, "big.png", tooBig); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload: code=%d (want 413) body=%s", code, raw)
	}

	// A non-image body is rejected with 400.
	if code, raw := h.uploadBytes(t, "/api/v1/branding/logo", admin, "notes.txt", []byte("this is plainly not an image")); code != http.StatusBadRequest {
		t.Fatalf("non-image upload: code=%d (want 400) body=%s", code, raw)
	}

	// A valid PNG is accepted; GET branding then shows has_logo + URL.
	if code, raw := h.uploadBytes(t, "/api/v1/branding/logo", admin, "logo.png", brandingPNG(t)); code != http.StatusOK {
		t.Fatalf("valid png upload: code=%d body=%s", code, raw)
	}
	_, m := h.getJSON(t, "/api/v1/branding", admin)
	if hl, _ := m["has_logo"].(bool); !hl {
		t.Fatalf("expected has_logo=true after upload: %+v", m)
	}
	if m["logo_url"] != "/api/v1/branding/logo" {
		t.Fatalf("expected logo_url, got %q", m["logo_url"])
	}

	// GET logo streams it with the PNG content type.
	code, raw := h.do(t, http.MethodGet, "/api/v1/branding/logo", admin, nil, "")
	if code != http.StatusOK {
		t.Fatalf("GET logo: code=%d", code)
	}
	if !bytes.HasPrefix(raw, []byte("\x89PNG")) {
		t.Fatalf("GET logo did not return PNG bytes")
	}

	// SR-L2: the logo download must carry X-Content-Type-Options: nosniff and an
	// inline Content-Disposition, for parity with the attachments handler.
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, h.baseURL+"/api/v1/branding/logo", nil)
	req.Header.Set("Authorization", "Bearer "+admin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET logo headers: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("logo download X-Content-Type-Options = %q; want nosniff", got)
	}
	if got := resp.Header.Get("Content-Disposition"); got != "inline" {
		t.Fatalf("logo download Content-Disposition = %q; want inline", got)
	}

	// DELETE clears it; a subsequent GET is 404.
	if code, _ := h.do(t, http.MethodDelete, "/api/v1/branding/logo", admin, nil, ""); code != http.StatusNoContent {
		t.Fatalf("DELETE logo: code=%d (want 204)", code)
	}
	if code, _ := h.do(t, http.MethodGet, "/api/v1/branding/logo", admin, nil, ""); code != http.StatusNotFound {
		t.Fatalf("GET logo after delete: code=%d (want 404)", code)
	}
}

// TestLetterhead_ReportPDFCarriesLetterhead proves the report PDF refactor:
// after setting branding, the daily-close and financials PDFs still return a
// valid application/pdf 200 (the letterhead header is rendered from branding).
func TestLetterhead_ReportPDFCarriesLetterhead(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	admin := h.login(t, slug(h), h.ids.adminEmail)

	_, _ = h.do(t, http.MethodPut, "/api/v1/branding", admin,
		bytes.NewReader([]byte(`{"display_name":"Acme Fuels","footer_note":"Confidential"}`)), "application/json")
	_, _ = h.uploadBytes(t, "/api/v1/branding/logo", admin, "logo.png", brandingPNG(t))

	// Financials PDF (tenant-wide, finance.read held by system_admin).
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		h.baseURL+"/api/v1/reports/financials.pdf", nil)
	req.Header.Set("Authorization", "Bearer "+admin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("financials pdf: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("financials pdf: code=%d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("financials pdf content-type=%q (want application/pdf)", ct)
	}
	var body bytes.Buffer
	_, _ = body.ReadFrom(resp.Body)
	if body.Len() < 800 || !bytes.HasPrefix(body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("financials pdf not a valid non-empty PDF (%d bytes)", body.Len())
	}
}
