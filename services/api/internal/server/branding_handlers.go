package server

import (
	"encoding/json"
	"net/http"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/branding"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// Tenant branding / company-letterhead API (LETTERHEAD).
//
// One letterhead per tenant, stored in tenant_branding. The text fields drive
// the header of every downloadable PDF (the shared newLetterheadDoc helper);
// the logo is uploaded/streamed separately so a branding read never carries the
// image bytes. Writes are gated by companies.manage and audited.

// maxBrandingLogoBytes caps an uploaded logo at 1 MiB. The whole request body
// is bounded by MaxBytesReader so an oversized upload can't exhaust memory.
const maxBrandingLogoBytes = 1 << 20 // 1 MiB

type brandingDTO struct {
	DisplayName     string `json:"display_name"`
	LegalName       string `json:"legal_name"`
	TaxID           string `json:"tax_id"`
	RegistrationNo  string `json:"registration_no"`
	AddressLine1    string `json:"address_line1"`
	AddressLine2    string `json:"address_line2"`
	City            string `json:"city"`
	Country         string `json:"country"`
	Phone           string `json:"phone"`
	Email           string `json:"email"`
	Website         string `json:"website"`
	FooterNote      string `json:"footer_note"`
	HasLogo         bool   `json:"has_logo"`
	LogoContentType string `json:"logo_content_type,omitempty"`
	LogoURL         string `json:"logo_url,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

func toBrandingDTO(b *branding.Branding) brandingDTO {
	dto := brandingDTO{
		DisplayName:     b.DisplayName,
		LegalName:       b.LegalName,
		TaxID:           b.TaxID,
		RegistrationNo:  b.RegistrationNo,
		AddressLine1:    b.AddressLine1,
		AddressLine2:    b.AddressLine2,
		City:            b.City,
		Country:         b.Country,
		Phone:           b.Phone,
		Email:           b.Email,
		Website:         b.Website,
		FooterNote:      b.FooterNote,
		HasLogo:         b.HasLogo,
		LogoContentType: b.LogoContentType,
	}
	if b.HasLogo {
		// Same-origin path the SDK/BFF resolve; carries the bytes via GET.
		dto.LogoURL = "/api/v1/branding/logo"
	}
	if !b.UpdatedAt.IsZero() {
		dto.UpdatedAt = b.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return dto
}

// loadLetterhead resolves a tenant's branding (text + logo bytes) into the
// flattened LetterheadBranding the shared PDF helper consumes. It never fails
// the caller hard: on any error it logs and returns a zero-value branding so a
// document still renders (with the product name as a fallback brand). This is
// the bridge every document-PDF handler uses before newLetterheadDoc.
func (s *Server) loadLetterhead(r *http.Request, tenantID uuid.UUID) LetterheadBranding {
	ctx := r.Context()
	if s.branding == nil {
		return LetterheadBranding{}
	}
	b, err := s.branding.Get(ctx, tenantID)
	if err != nil {
		s.logger.Error("load letterhead branding", "error", err)
		return LetterheadBranding{}
	}
	lb := LetterheadBranding{
		DisplayName:    b.DisplayName,
		LegalName:      b.LegalName,
		TaxID:          b.TaxID,
		RegistrationNo: b.RegistrationNo,
		AddressLine1:   b.AddressLine1,
		AddressLine2:   b.AddressLine2,
		City:           b.City,
		Country:        b.Country,
		Phone:          b.Phone,
		Email:          b.Email,
		Website:        b.Website,
		FooterNote:     b.FooterNote,
	}
	if b.HasLogo {
		data, ct, found, lerr := s.branding.GetLogo(ctx, tenantID)
		if lerr != nil {
			s.logger.Error("load letterhead logo", "error", lerr)
		} else if found {
			lb.Logo = data
			lb.LogoContentType = ct
		}
	}
	return lb
}

// handleGetBranding returns the tenant's branding (text fields + logo presence
// and URL, never the bytes). Any authenticated tenant user may read it (the
// letterhead is on every document they can already download).
func (s *Server) handleGetBranding(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	b, err := s.branding.Get(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("get branding", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toBrandingDTO(b))
}

type updateBrandingRequest struct {
	DisplayName    string `json:"display_name"`
	LegalName      string `json:"legal_name"`
	TaxID          string `json:"tax_id"`
	RegistrationNo string `json:"registration_no"`
	AddressLine1   string `json:"address_line1"`
	AddressLine2   string `json:"address_line2"`
	City           string `json:"city"`
	Country        string `json:"country"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	Website        string `json:"website"`
	FooterNote     string `json:"footer_note"`
}

// handleUpdateBranding upserts the tenant branding text fields. Permission:
// companies.manage. Audited as branding.updated.
func (s *Server) handleUpdateBranding(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req updateBrandingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ctx := r.Context()
	before, err := s.branding.Get(ctx, actor.TenantID)
	if err != nil {
		s.logger.Error("update branding: load before", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	after, err := s.branding.Upsert(ctx, actor.TenantID, branding.UpsertInput{
		DisplayName:    req.DisplayName,
		LegalName:      req.LegalName,
		TaxID:          req.TaxID,
		RegistrationNo: req.RegistrationNo,
		AddressLine1:   req.AddressLine1,
		AddressLine2:   req.AddressLine2,
		City:           req.City,
		Country:        req.Country,
		Phone:          req.Phone,
		Email:          req.Email,
		Website:        req.Website,
		FooterNote:     req.FooterNote,
	})
	if err != nil {
		s.logger.Error("update branding", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.auditBranding(r, actor, "branding.updated", "BrandingUpdated",
		toBrandingDTO(before), toBrandingDTO(after))
	writeJSON(w, http.StatusOK, toBrandingDTO(after))
}

// handleUploadBrandingLogo accepts a multipart image (image/png or image/jpeg),
// caps it at 1 MiB, and stores the bytes + content type. Permission:
// companies.manage. Audited as branding.logo_updated.
func (s *Server) handleUploadBrandingLogo(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	// Bound the entire request body so an oversized upload can't exhaust memory;
	// a body over the cap surfaces as a parse error we map to 413.
	r.Body = http.MaxBytesReader(w, r.Body, maxBrandingLogoBytes+(1<<16))
	if err := r.ParseMultipartForm(maxBrandingLogoBytes); err != nil { //nolint:gosec // G120: body bounded by MaxBytesReader above
		writeError(w, http.StatusRequestEntityTooLarge, "image must be 1 MiB or smaller")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "an image file field is required")
		return
	}
	defer func() { _ = file.Close() }()

	if header.Size > maxBrandingLogoBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "image must be 1 MiB or smaller")
		return
	}

	// Read into memory (already bounded) and sniff the real content type so a
	// renamed/relabelled file can't smuggle a non-image in.
	data := make([]byte, 0, header.Size)
	buf := make([]byte, 32*1024)
	for {
		n, rerr := file.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
			if len(data) > maxBrandingLogoBytes {
				writeError(w, http.StatusRequestEntityTooLarge, "image must be 1 MiB or smaller")
				return
			}
		}
		if rerr != nil {
			break
		}
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "the uploaded image is empty")
		return
	}

	contentType := http.DetectContentType(data)
	if contentType != "image/png" && contentType != "image/jpeg" {
		writeError(w, http.StatusBadRequest, "logo must be a PNG or JPEG image")
		return
	}

	ctx := r.Context()
	if err := s.branding.SetLogo(ctx, actor.TenantID, data, contentType); err != nil {
		s.logger.Error("upload branding logo", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.auditBranding(r, actor, "branding.logo_updated", "BrandingLogoUpdated", nil, map[string]any{
		"content_type": contentType, "byte_count": len(data),
	})

	b, err := s.branding.Get(ctx, actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toBrandingDTO(b))
}

// handleGetBrandingLogo streams the stored logo with its content type, or 404
// when none is set. Readable by any authenticated tenant user.
func (s *Server) handleGetBrandingLogo(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	data, contentType, found, err := s.branding.GetLogo(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("get branding logo", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "no logo set")
		return
	}
	// The content type is validated at upload (PNG/JPEG only, via
	// http.DetectContentType) and set explicitly here. nosniff stops the browser
	// from re-interpreting the body, and an inline disposition serves the logo
	// for rendering without allowing it to be re-typed — parity with the
	// attachments handler's defense-in-depth on serving stored user bytes.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleDeleteBrandingLogo clears the stored logo. Permission: companies.manage.
// Audited as branding.logo_cleared. Idempotent (204 even when none was set).
func (s *Server) handleDeleteBrandingLogo(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if err := s.branding.ClearLogo(r.Context(), actor.TenantID); err != nil {
		s.logger.Error("delete branding logo", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.auditBranding(r, actor, "branding.logo_cleared", "BrandingLogoCleared", nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

// auditBranding writes a tenant-wide branding audit + outbox event in its own
// transaction. Best-effort: a failure is logged but does not fail the request
// (the branding write already committed via the repo).
func (s *Server) auditBranding(
	r *http.Request, actor identity.Actor,
	action, eventType string, prev, next any,
) {
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		s.logger.Error("audit branding: begin", "error", err, "action", action)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID:      actor.TenantID,
		ActorID:       actor.UserID,
		Action:        action,
		EventType:     eventType,
		EntityType:    "tenant_branding",
		EntityID:      actor.TenantID.String(),
		PreviousValue: prev,
		NewValue:      next,
		IP:            clientIP(r),
		UserAgent:     r.UserAgent(),
		RequestID:     chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("audit branding: write", "error", err, "action", action)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		s.logger.Error("audit branding: commit", "error", err, "action", action)
	}
}
