package server_test

// DB-backed integration tests for the generic per-entity Attachments framework
// (C.3). They reuse the Phase 2 harness (boots the real API, seeds a tenant
// whose system_admin holds attachment.read + attachment.manage) and exercise
// the full surface end-to-end against a seeded expense parent: upload
// validation (oversize -> 413, non-allowed type -> 400), list, stream (bytes +
// content type), soft-delete hides from the list, tenant isolation, and the
// posted-parent delete refusal (409).
//
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL like the rest of the suite.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

// attachmentPNG returns a small valid PNG.
func attachmentPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.Set(x, y, color.RGBA{R: 10, G: 90, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// uploadAttachment posts the entity_type + entity_id + file multipart form.
func (h *harness) uploadAttachment(
	t *testing.T, token, entityType, entityID, filename string, data []byte,
) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("entity_type", entityType)
	_ = mw.WriteField("entity_id", entityID)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	_, _ = fw.Write(data)
	_ = mw.Close()
	return h.do(t, http.MethodPost, "/api/v1/attachments", token, &buf, mw.FormDataContentType())
}

// seedExpense inserts a draft expense for the tenant and returns its id.
func (h *harness) seedExpense(t *testing.T, status string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var adminID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		SELECT u.id FROM users u
		JOIN user_roles ur ON ur.user_id = u.id AND ur.tenant_id = u.tenant_id
		JOIN roles r ON r.id = ur.role_id
		WHERE u.tenant_id = $1 AND r.code = 'system_admin'
		ORDER BY u.created_at LIMIT 1`, h.ids.tenantID).Scan(&adminID); err != nil {
		t.Fatalf("seed expense: admin lookup: %v", err)
	}
	var id uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO expenses (tenant_id, station_id, amount, status, created_by)
		VALUES ($1, $2, '120.00', $3, $4) RETURNING id`,
		h.ids.tenantID, h.ids.station1, status, adminID).Scan(&id); err != nil {
		t.Fatalf("seed expense: %v", err)
	}
	return id
}

func TestAttachments_UploadValidationAndLifecycle(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	admin := h.login(t, slug(h), h.ids.adminEmail)
	expID := h.seedExpense(t, "draft").String()

	// Oversized body (> 5 MiB) -> 413.
	tooBig := bytes.Repeat([]byte{0x89}, (5<<20)+1024)
	if code, raw := h.uploadAttachment(t, admin, "expense", expID, "big.pdf", tooBig); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload: code=%d (want 413) body=%s", code, raw)
	}

	// A non-allowed type (plain text) -> 400.
	if code, raw := h.uploadAttachment(t, admin, "expense", expID, "notes.txt", []byte("not a pdf or image at all")); code != http.StatusBadRequest {
		t.Fatalf("non-allowed upload: code=%d (want 400) body=%s", code, raw)
	}

	// A valid PNG -> 201 with metadata.
	code, raw := h.uploadAttachment(t, admin, "expense", expID, "receipt.png", attachmentPNG(t))
	if code != http.StatusCreated {
		t.Fatalf("valid upload: code=%d (want 201) body=%s", code, raw)
	}
	var created map[string]any
	_ = json.Unmarshal(raw, &created)
	attID, _ := created["id"].(string)
	if attID == "" {
		t.Fatalf("upload returned no id: %s", raw)
	}
	if created["content_type"] != "image/png" {
		t.Fatalf("content_type=%v (want image/png)", created["content_type"])
	}

	// List shows it.
	code, m := h.getJSON(t, "/api/v1/entities/expense/"+expID+"/attachments", admin)
	if code != http.StatusOK || countOf(m) != 1 {
		t.Fatalf("list: code=%d count=%d (want 200/1)", code, countOf(m))
	}

	// Stream returns the PNG bytes + content type.
	code, body := h.do(t, http.MethodGet, "/api/v1/attachments/"+attID, admin, nil, "")
	if code != http.StatusOK {
		t.Fatalf("stream: code=%d", code)
	}
	if !bytes.HasPrefix(body, []byte("\x89PNG")) {
		t.Fatalf("stream did not return PNG bytes")
	}

	// Soft-delete hides it from the list and 404s a subsequent stream.
	if code, _ := h.do(t, http.MethodDelete, "/api/v1/attachments/"+attID, admin, nil, ""); code != http.StatusNoContent {
		t.Fatalf("delete: code=%d (want 204)", code)
	}
	_, m = h.getJSON(t, "/api/v1/entities/expense/"+expID+"/attachments", admin)
	if countOf(m) != 0 {
		t.Fatalf("list after delete: count=%d (want 0)", countOf(m))
	}
	if code, _ := h.do(t, http.MethodGet, "/api/v1/attachments/"+attID, admin, nil, ""); code != http.StatusNotFound {
		t.Fatalf("stream after delete: code=%d (want 404)", code)
	}
}

func TestAttachments_UnknownParentIs404(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	admin := h.login(t, slug(h), h.ids.adminEmail)

	// A well-formed but non-existent expense id -> 404 (no orphan attachments).
	if code, raw := h.uploadAttachment(t, admin, "expense", uuid.NewString(), "r.png", attachmentPNG(t)); code != http.StatusNotFound {
		t.Fatalf("unknown parent upload: code=%d (want 404) body=%s", code, raw)
	}
}

func TestAttachments_PostedParentDeleteRefused(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	admin := h.login(t, slug(h), h.ids.adminEmail)
	expID := h.seedExpense(t, "draft").String()

	code, raw := h.uploadAttachment(t, admin, "expense", expID, "receipt.png", attachmentPNG(t))
	if code != http.StatusCreated {
		t.Fatalf("upload: code=%d body=%s", code, raw)
	}
	var created map[string]any
	_ = json.Unmarshal(raw, &created)
	attID := created["id"].(string)

	// Flip the parent expense to posted; deleting its attachment is now refused.
	if _, err := h.pool.Exec(context.Background(),
		`UPDATE expenses SET status = 'posted' WHERE tenant_id = $1 AND id = $2`,
		h.ids.tenantID, expID); err != nil {
		t.Fatalf("post expense: %v", err)
	}
	if code, _ := h.do(t, http.MethodDelete, "/api/v1/attachments/"+attID, admin, nil, ""); code != http.StatusConflict {
		t.Fatalf("delete posted-parent attachment: code=%d (want 409)", code)
	}
}

func TestAttachments_TenantIsolation(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	admin := h.login(t, slug(h), h.ids.adminEmail)
	expID := h.seedExpense(t, "draft").String()

	code, raw := h.uploadAttachment(t, admin, "expense", expID, "receipt.png", attachmentPNG(t))
	if code != http.StatusCreated {
		t.Fatalf("upload: code=%d body=%s", code, raw)
	}
	var created map[string]any
	_ = json.Unmarshal(raw, &created)
	attID := created["id"].(string)

	// Stand up a second tenant + its own system_admin in the same DB.
	hash, err := password.New(password.DefaultParams, "").Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	suffix := time.Now().UnixNano()
	otherSlug := fmt.Sprintf("other-%d", suffix)
	otherEmail := fmt.Sprintf("other-admin-%d@it.local", suffix)
	var otherTenant, otherUser uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO tenants (name, slug) VALUES ('Other Co', $1) RETURNING id`, otherSlug).Scan(&otherTenant); err != nil {
		t.Fatalf("seed other tenant: %v", err)
	}
	defer cleanupTenant(ctx, h.pool, otherTenant)
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'Other Admin', 'active', $3, now()) RETURNING id`,
		otherTenant, otherEmail, hash).Scan(&otherUser); err != nil {
		t.Fatalf("seed other user: %v", err)
	}
	grantRole(t, ctx, h.pool, otherTenant, otherUser, "system_admin")
	other := h.login(t, otherSlug, otherEmail)

	// The other tenant cannot stream the attachment (404, not the bytes).
	if code, _ := h.do(t, http.MethodGet, "/api/v1/attachments/"+attID, other, nil, ""); code != http.StatusNotFound {
		t.Fatalf("cross-tenant stream: code=%d (want 404)", code)
	}
	// Nor see it in a list for the same entity id.
	_, m := h.getJSON(t, "/api/v1/entities/expense/"+expID+"/attachments", other)
	if countOf(m) != 0 {
		t.Fatalf("cross-tenant list: count=%d (want 0)", countOf(m))
	}
	// Nor delete it.
	if code, _ := h.do(t, http.MethodDelete, "/api/v1/attachments/"+attID, other, nil, ""); code != http.StatusNotFound {
		t.Fatalf("cross-tenant delete: code=%d (want 404)", code)
	}
}

func TestAttachments_ForbiddenForAttendant(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	admin := h.login(t, slug(h), h.ids.adminEmail)
	expID := h.seedExpense(t, "draft").String()

	// A freshly-created attendant holds neither attachment.read nor
	// attachment.manage, so every attachment route is 403.
	hash, err := password.New(password.DefaultParams, "").Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	email := fmt.Sprintf("att-noperm-%d@it.local", time.Now().UnixNano())
	var uid uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'No-Perm Attendant', 'active', $3, now()) RETURNING id`,
		h.ids.tenantID, email, hash).Scan(&uid); err != nil {
		t.Fatalf("seed attendant: %v", err)
	}
	grantRole(t, ctx, h.pool, h.ids.tenantID, uid, "attendant")
	att := h.login(t, slug(h), email)

	if code, _ := h.uploadAttachment(t, att, "expense", expID, "r.png", attachmentPNG(t)); code != http.StatusForbidden {
		t.Fatalf("attendant upload: code=%d (want 403)", code)
	}
	if code, _ := h.do(t, http.MethodGet, "/api/v1/entities/expense/"+expID+"/attachments", att, nil, ""); code != http.StatusForbidden {
		t.Fatalf("attendant list: code=%d (want 403)", code)
	}

	// Sanity: the admin (system_admin) can still upload.
	if code, _ := h.uploadAttachment(t, admin, "expense", expID, "ok.png", attachmentPNG(t)); code != http.StatusCreated {
		t.Fatalf("admin upload: code=%d (want 201)", code)
	}
}
